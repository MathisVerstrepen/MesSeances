package accountavatar

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

	webpdecoder "golang.org/x/image/webp"
	"golang.org/x/sys/unix"
)

var finalName = regexp.MustCompile(`^[a-f0-9]{32}\.webp$`)
var tempName = regexp.MustCompile(`^[a-f0-9]{32}\.tmp$`)

// Store has one runtime owner. Close only after requests and Sweep have drained.
type Store struct {
	root               *os.Root
	dir, lock, cursor  *os.File
	slots, publication chan struct{}
	mu                 sync.Mutex // active stage names
	active             map[string]bool
	sweepMu            sync.Mutex
	client             *pictureClient
}

func Open(path string) (*Store, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || filepath.Clean(path) == "/" {
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
	s := &Store{root: root, slots: make(chan struct{}, 2), publication: make(chan struct{}, 1), active: make(map[string]bool), client: newPictureClient()}
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
	s.lock, err = root.OpenFile(".lock", os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
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
	// Probe durable writes without accepting an operator path as a filename.
	stage, err := s.Stage(context.Background(), []byte("probe"))
	if err != nil {
		return nil, ErrStorage
	}
	stage.Done()
	if err = s.remove(stage.temp); err != nil || s.dir.Sync() != nil {
		return nil, ErrStorage
	}
	ok = true
	return s, nil
}
func owned(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Geteuid())
}
func privateDir(info os.FileInfo) bool {
	return info != nil && info.IsDir() && info.Mode().Perm() == 0700 && owned(info)
}
func privateFile(info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && info.Mode().Perm() == 0600 && owned(info)
}
func (s *Store) Close() error {
	var errs []error
	if s.client != nil {
		s.client.close()
	}
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

// Guard is shared with GC, and must be obtained before beginning a DB transaction.
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
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(b) > MaxOutput {
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
	_, writeErr := f.Write(b)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || ctx.Err() != nil {
		stage.Done()
		_ = s.remove(stage.temp)
		return nil, ErrStorage
	}
	return stage, nil
}

// Publish requires Guard. Failure/ambiguous DB commit leaves an orphan for GC.
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
func (s *Store) Read(ctx context.Context, name string) ([]byte, error) {
	if !finalName.MatchString(name) {
		return nil, ErrStorage
	}
	b, err := s.readFile(ctx, name)
	if err != nil {
		return nil, err
	}
	cfg, err := webpdecoder.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width != 256 || cfg.Height != 256 {
		return nil, ErrStorage
	}
	if _, err = webpFraming(b, 256, 256); err != nil {
		return nil, ErrStorage
	}
	im, err := webpdecoder.Decode(bytes.NewReader(b))
	if err != nil || im.Bounds().Dx() != 256 || im.Bounds().Dy() != 256 {
		return nil, ErrStorage
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return b, nil
}

// readFile is private; callers must first validate the generated filename.
func (s *Store) readFile(ctx context.Context, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := s.root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, ErrStorage
	}
	defer func() { _ = f.Close() }() // Read-only descriptor; no pending writes.
	info, err := f.Stat()
	if err != nil || !privateFile(info) || info.Size() > MaxOutput {
		return nil, ErrStorage
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxOutput+1))
	if err != nil || len(b) > MaxOutput {
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
	info, err := s.root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !privateFile(info) {
		return ErrStorage
	}
	if s.root.Remove(name) != nil {
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

// Sweep's reference callback must commit an advisory-lock barrier transaction.
// The guard remains held after that commit through unlink. No account row locks.
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
		info, err := s.root.Lstat(name)
		if err != nil {
			result.Errors++
			continue
		}
		if !privateFile(info) || now.Sub(info.ModTime()) < time.Hour {
			continue
		}
		if len(candidates) == 100 {
			result.Backlog = true
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
	for _, name := range candidates {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if refs[name] {
			continue
		}
		if err = s.remove(name); err != nil {
			result.Errors++
		} else {
			result.Deleted++
		}
	}
	return result, nil
}
