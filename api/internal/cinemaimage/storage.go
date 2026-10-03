package cinemaimage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	webpdecoder "golang.org/x/image/webp"
	"golang.org/x/sys/unix"
)

var finalName = regexp.MustCompile(`^[a-f0-9]{32}\.webp$`)
var tempName = regexp.MustCompile(`^[a-f0-9]{32}\.tmp$`)

// Store has one runtime owner. Close only after HTTP and cleanup have drained.
type Store struct {
	root               *os.Root
	dir, lock, cursor  *os.File
	slots, publication chan struct{}
	mu                 sync.Mutex // active stage names
	active             map[string]bool
	sweepMu            sync.Mutex
}

func Open(path string) (*Store, error) {
	if !utf8.ValidString(path) {
		return nil, ErrStorage
	}
	for _, c := range path {
		if unicode.IsControl(c) {
			return nil, ErrStorage
		}
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || path == "/" {
		return nil, ErrStorage
	}
	info, err := os.Lstat(path)
	if err != nil || !privateDir(info) {
		return nil, ErrStorage
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, ErrStorage
	}
	s := &Store{root: root, slots: make(chan struct{}, 1), publication: make(chan struct{}, 1), active: make(map[string]bool)}
	ok := false
	defer func() {
		if !ok {
			_ = s.Close()
		}
	}()
	s.dir, err = root.OpenFile(".", os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrStorage
	}
	actual, err := s.dir.Stat()
	if err != nil || !privateDir(actual) || !os.SameFile(info, actual) {
		return nil, ErrStorage
	}
	s.lock, err = root.OpenFile(".lock", os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, ErrStorage
	}
	li, err := s.lock.Stat()
	if err != nil || !privateFile(li) {
		return nil, ErrStorage
	}
	if unix.Flock(int(s.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		return nil, ErrStorage
	}
	stage, err := s.Stage(context.Background(), []byte("probe"))
	if err != nil {
		return nil, ErrStorage
	}
	unlock, err := s.Guard(context.Background())
	if err != nil {
		stage.Done()
		return nil, ErrStorage
	}
	name, err := stage.Publish(context.Background())
	unlock()
	stage.Done()
	if err != nil || s.Remove(name) != nil {
		return nil, ErrStorage
	}
	ok = true
	return s, nil
}
func owned(i os.FileInfo) bool {
	st, ok := i.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Geteuid())
}
func privateDir(i os.FileInfo) bool {
	return i != nil && i.IsDir() && i.Mode().Perm() == 0700 && owned(i)
}
func privateFile(i os.FileInfo) bool {
	return i != nil && i.Mode().IsRegular() && i.Mode().Perm() == 0600 && owned(i)
}
func (s *Store) Close() error {
	var errs []error
	for _, f := range []*os.File{s.cursor, s.dir, s.lock} {
		if f != nil {
			errs = append(errs, f.Close())
		}
	}
	if s.root != nil {
		errs = append(errs, s.root.Close())
	}
	return errors.Join(errs...)
}
func (s *Store) Admit(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case s.slots <- struct{}{}:
		return func() { <-s.slots }, nil
	default:
		return nil, ErrBusy
	}
}

// Guard must precede any media transaction and remain held through post-commit unlink.
func (s *Store) Guard(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case s.publication <- struct{}{}:
		return func() { <-s.publication }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type Stage struct {
	store      *Store
	temp, name string
}

func (s *Store) Stage(ctx context.Context, b []byte) (*Stage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(b) == 0 || len(b) > MaxOutput {
		return nil, ErrStorage
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, ErrStorage
	}
	base := hex.EncodeToString(random[:])
	stage := &Stage{s, base + ".tmp", base + ".webp"}
	s.mu.Lock()
	s.active[stage.temp] = true
	s.active[stage.name] = true
	s.mu.Unlock()
	f, err := s.root.OpenFile(stage.temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
	if err != nil {
		stage.Done()
		return nil, ErrStorage
	}
	n, we := f.Write(b)
	se := f.Sync()
	ce := f.Close()
	if we != nil || n != len(b) || se != nil || ce != nil || ctx.Err() != nil {
		stage.Done()
		_ = s.remove(stage.temp)
		return nil, ErrStorage
	}
	return stage, nil
}

// Publish requires Guard. Any failure leaves only a reference-checkable orphan.
func (s *Stage) Publish(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	fd := int(s.store.dir.Fd())
	if unix.Renameat2(fd, s.temp, fd, s.name, unix.RENAME_NOREPLACE) != nil || s.store.dir.Sync() != nil {
		return "", ErrStorage
	}
	return s.name, nil
}
func (s *Stage) Done() {
	s.store.mu.Lock()
	delete(s.store.active, s.temp)
	delete(s.store.active, s.name)
	s.store.mu.Unlock()
}
func (s *Store) Read(ctx context.Context, name string, width, height, size int) ([]byte, error) {
	if !finalName.MatchString(name) || width < 1 || height < 1 || width > MaxEdge || height > MaxEdge || size < 1 || size > MaxOutput {
		return nil, ErrStorage
	}
	f, err := s.root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, ErrStorage
	}
	defer func() { _ = f.Close() }() // read-only descriptor
	i, err := f.Stat()
	if err != nil || !privateFile(i) || i.Size() != int64(size) {
		return nil, ErrStorage
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxOutput+1))
	if err != nil || len(b) != size {
		return nil, ErrStorage
	}
	cfg, err := webpdecoder.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width != width || cfg.Height != height {
		return nil, ErrStorage
	}
	if _, err = webpFraming(b, width, height); err != nil {
		return nil, ErrStorage
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return b, nil
}
func (s *Store) remove(name string) error {
	if !finalName.MatchString(name) && !tempName.MatchString(name) {
		return ErrStorage
	}
	i, err := s.root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !privateFile(i) {
		return ErrStorage
	}
	if s.root.Remove(name) != nil || s.dir.Sync() != nil {
		return ErrStorage
	}
	return nil
}
func (s *Store) Remove(name string) error {
	if !finalName.MatchString(name) {
		return ErrStorage
	}
	return s.remove(name)
}

type SweepResult struct {
	Examined, Deleted, Errors int
	Backlog                   bool
}

// References must commit the media advisory barrier transaction before returning.
func (s *Store) Sweep(ctx context.Context, now time.Time, references func(context.Context, []string) (map[string]bool, error)) (SweepResult, error) {
	s.sweepMu.Lock()
	defer s.sweepMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result SweepResult
	if s.cursor == nil {
		var err error
		s.cursor, err = s.root.Open(".")
		if err != nil {
			return result, ErrStorage
		}
	}
	entries, err := s.cursor.ReadDir(1000)
	if errors.Is(err, io.EOF) || len(entries) < 1000 {
		_ = s.cursor.Close()
		s.cursor = nil
	} else if err != nil {
		return result, ErrStorage
	}
	result.Backlog = len(entries) == 1000
	unlock, err := s.Guard(ctx)
	if err != nil {
		return result, err
	}
	defer unlock()
	var candidates, finals []string
	for _, entry := range entries {
		result.Examined++
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		name := entry.Name()
		if !finalName.MatchString(name) && !tempName.MatchString(name) {
			continue
		}
		s.mu.Lock()
		active := s.active[name]
		s.mu.Unlock()
		if active {
			continue
		}
		i, err := s.root.Lstat(name)
		if err != nil {
			result.Errors++
			continue
		}
		if !privateFile(i) || now.Sub(i.ModTime()) < time.Hour {
			continue
		}
		candidates = append(candidates, name)
		if finalName.MatchString(name) {
			finals = append(finals, name)
		}
	}
	refs, err := references(ctx, finals)
	if err != nil {
		return result, err
	}
	attempts := 0
	for _, name := range candidates {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if refs[name] {
			continue
		}
		if attempts == 100 {
			result.Backlog = true
			break
		}
		attempts++
		if err = s.remove(name); err != nil {
			result.Errors++
		} else {
			result.Deleted++
		}
	}
	return result, nil
}
