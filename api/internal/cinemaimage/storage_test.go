package cinemaimage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := s.Close(); e != nil {
			t.Error(e)
		}
	})
	return s
}
func publishPhoto(t *testing.T, s *Store) (string, Photo) {
	t.Helper()
	ctx := context.Background()
	p, e := Normalize(ctx, pngPhoto(t, 32, 16), "image/png")
	if e != nil {
		t.Fatal(e)
	}
	stage, e := s.Stage(ctx, p.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	defer stage.Done()
	unlock, e := s.Guard(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	key, e := stage.Publish(ctx)
	if e != nil {
		t.Fatal(e)
	}
	return key, p
}
func TestStoreOwnershipAndAdmission(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	release, e := s.Admit(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Admit(ctx); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	release()
	if second, e := Open(s.root.Name()); e == nil {
		_ = second.Close()
		t.Fatal("second owner accepted")
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = s.Admit(ctx); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	for _, mode := range []os.FileMode{0755, 0777, 0701} {
		path := t.TempDir()
		if os.Chmod(path, mode) != nil {
			t.Fatal("chmod")
		}
		if bad, e := Open(path); e == nil {
			_ = bad.Close()
			t.Fatal("unsafe directory accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "root")
	if os.Symlink(s.root.Name(), path) != nil {
		t.Fatal("symlink")
	}
	if bad, e := Open(path); e == nil {
		_ = bad.Close()
		t.Fatal("symlink root accepted")
	}
}
func TestStoreDurabilityAndNoReplace(t *testing.T) {
	path := t.TempDir()
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	key, p := publishPhoto(t, s)
	stage, e := s.Stage(context.Background(), p.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	stage.name = key
	if _, e = stage.Publish(context.Background()); !errors.Is(e, ErrStorage) {
		t.Fatal("overwrite accepted")
	}
	stage.Done()
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = s.Close() }()
	b, e := s.Read(context.Background(), key, p.Width, p.Height, len(p.Bytes))
	if e != nil || string(b) != string(p.Bytes) {
		t.Fatal("reopen read", e)
	}
	for _, name := range []string{"../" + key, "/etc/passwd", "", strings.Repeat("a", 32) + ".jpg"} {
		if _, e = s.Read(context.Background(), name, 32, 16, len(p.Bytes)); !errors.Is(e, ErrStorage) {
			t.Fatal("invalid key accepted")
		}
	}
	if _, e = s.Read(context.Background(), key, 33, 16, len(p.Bytes)); !errors.Is(e, ErrStorage) {
		t.Fatal("wrong metadata accepted")
	}
}
func TestStoreRejectsNonregularAndUnsafeFiles(t *testing.T) {
	s := testStore(t)
	key, p := publishPhoto(t, s)
	if os.Chmod(filepath.Join(s.root.Name(), key), 0644) != nil {
		t.Fatal("chmod")
	}
	if _, e := s.Read(context.Background(), key, 32, 16, len(p.Bytes)); !errors.Is(e, ErrStorage) {
		t.Fatal(e)
	}
	link := strings.Repeat("b", 32) + ".webp"
	if os.Symlink(key, filepath.Join(s.root.Name(), link)) != nil {
		t.Fatal("symlink")
	}
	if _, e := s.Read(context.Background(), link, 32, 16, len(p.Bytes)); !errors.Is(e, ErrStorage) {
		t.Fatal(e)
	}
	if e := s.Remove(link); !errors.Is(e, ErrStorage) {
		t.Fatal("symlink removal accepted")
	}
	if _, e := s.Read(context.Background(), strings.Repeat("c", 32)+".webp", 32, 16, len(p.Bytes)); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
}
func TestStoreReferenceSafeCleanup(t *testing.T) {
	s := testStore(t)
	referenced, p := publishPhoto(t, s)
	orphan, _ := publishPhoto(t, s)
	active, e := s.Stage(context.Background(), p.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	defer active.Done()
	for _, name := range []string{referenced, orphan, active.temp} {
		path := filepath.Join(s.root.Name(), name)
		if os.Chtimes(path, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour)) != nil {
			t.Fatal("chtimes")
		}
	}
	references := func(_ context.Context, names []string) (map[string]bool, error) {
		return map[string]bool{referenced: true}, nil
	}
	r, e := s.Sweep(context.Background(), time.Now(), references)
	if e != nil || r.Deleted != 1 {
		t.Fatalf("result %+v %v", r, e)
	}
	if _, e = s.Read(context.Background(), referenced, 32, 16, len(p.Bytes)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.root.Stat(active.temp); e != nil {
		t.Fatal("active stage deleted")
	}
	if _, e = s.root.Stat(orphan); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("orphan survived")
	}
	if _, e = s.Sweep(context.Background(), time.Now(), func(context.Context, []string) (map[string]bool, error) { return nil, ErrStorage }); !errors.Is(e, ErrStorage) {
		t.Fatal("barrier failure ignored")
	}
}

type otherOwnerInfo struct{ os.FileInfo }

func (i otherOwnerInfo) Sys() any { return &syscall.Stat_t{Uid: uint32(os.Geteuid() + 1)} }
func TestStoreWrongOwnerAndCleanupDeletionBound(t *testing.T) {
	s := testStore(t)
	info, e := s.root.Stat(".")
	if e != nil {
		t.Fatal(e)
	}
	if privateDir(otherOwnerInfo{info}) {
		t.Fatal("wrong-owner directory accepted")
	}
	now := time.Now()
	keys := []string{}
	for n := range 105 {
		key := fmt.Sprintf("%032x.webp", n+1)
		keys = append(keys, key)
		p := filepath.Join(s.root.Name(), key)
		if os.WriteFile(p, []byte{1}, 0600) != nil || os.Chtimes(p, now.Add(-2*time.Hour), now.Add(-2*time.Hour)) != nil {
			t.Fatal("fixture write")
		}
	}
	info, e = s.root.Stat(keys[0])
	if e != nil {
		t.Fatal(e)
	}
	if privateFile(otherOwnerInfo{info}) {
		t.Fatal("wrong-owner file accepted")
	}
	refs := func(context.Context, []string) (map[string]bool, error) { return map[string]bool{}, nil }
	r, e := s.Sweep(context.Background(), now, refs)
	if e != nil || r.Deleted != 100 || !r.Backlog {
		t.Fatalf("first pass %+v %v", r, e)
	}
	r, e = s.Sweep(context.Background(), now, refs)
	if e != nil || r.Deleted != 5 {
		t.Fatalf("second pass %+v %v", r, e)
	}
	for n := range 101 {
		key := fmt.Sprintf("%032x.webp", n+1)
		p := filepath.Join(s.root.Name(), key)
		if os.WriteFile(p, []byte{1}, 0600) != nil || os.Chtimes(p, now.Add(-2*time.Hour), now.Add(-2*time.Hour)) != nil {
			t.Fatal("fixture write")
		}
	}
	// Whichever 100 entries directory order puts first, references must not starve
	// the one orphan found later in the same bounded scan.
	r, e = s.Sweep(context.Background(), now, func(_ context.Context, names []string) (map[string]bool, error) {
		out := map[string]bool{}
		for _, name := range names[:len(names)-1] {
			out[name] = true
		}
		return out, nil
	})
	if e != nil || r.Deleted != 1 {
		t.Fatalf("referenced prefix starved orphan %+v %v", r, e)
	}
}
