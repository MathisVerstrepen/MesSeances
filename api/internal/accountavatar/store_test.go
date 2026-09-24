package accountavatar

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s, path
}

func TestPublicationNeverOverwrites(t *testing.T) {
	s, root := testStore(t)
	stage, err := s.Stage(t.Context(), []byte("candidate"))
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Done()
	if err = os.WriteFile(filepath.Join(root, stage.name), []byte("predecessor"), 0600); err != nil {
		t.Fatal(err)
	}
	release, err := s.Guard(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err = stage.Publish(t.Context()); !errors.Is(err, ErrStorage) {
		t.Fatal("rename replaced existing name")
	}
	data, err := os.ReadFile(filepath.Join(root, stage.name))
	if err != nil || string(data) != "predecessor" {
		t.Fatal("existing file damaged")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = stage.Publish(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled publication accepted")
	}
}
func publish(t *testing.T, s *Store) string {
	t.Helper()
	b, err := Normalize(t.Context(), fixture(t, "png"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	stage, err := s.Stage(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Done()
	unlock, err := s.Guard(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	name, err := stage.Publish(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return name
}
func TestStoreOwnershipPublicationAndRestart(t *testing.T) {
	s, path := testStore(t)
	name := publish(t, s)
	if _, err := Open(path); !errors.Is(err, ErrStorage) {
		t.Fatal("second writer admitted")
	}
	if _, err := s.Read(t.Context(), name); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../outside", "/absolute", ".lock", "bad.png"} {
		if _, err := s.Read(t.Context(), name); !errors.Is(err, ErrStorage) {
			t.Fatal("unsafe name")
		}
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	link := "11111111111111111111111111111111.png"
	if err := os.Symlink(outside, filepath.Join(path, link)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(t.Context(), link); !errors.Is(err, ErrStorage) {
		t.Fatal("symlink read")
	}
	if err := s.Remove(link); !errors.Is(err, ErrStorage) {
		t.Fatal("symlink delete")
	}
	rootLink := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, rootLink); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(rootLink + "/"); !errors.Is(err, ErrStorage) {
		t.Fatal("symlink root")
	}
	bad := t.TempDir()
	if err := os.Chmod(bad, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(bad); !errors.Is(err, ErrStorage) {
		t.Fatal("nonprivate root")
	}
	// A distinct root can reopen with the same persisted processed bytes and lock inode.
	restart := t.TempDir()
	if err := os.Chmod(restart, 0700); err != nil {
		t.Fatal(err)
	}
	one, err := Open(restart)
	if err != nil {
		t.Fatal(err)
	}
	saved := publish(t, one)
	if err = one.Close(); err != nil {
		t.Fatal(err)
	}
	two, err := Open(restart)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := two.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err = two.Read(t.Context(), saved); err != nil {
		t.Fatal("restart lost media")
	}
}
func TestAdmissionAndGuardCancellation(t *testing.T) {
	s, _ := testStore(t)
	one, err := s.Admit(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.Admit(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Admit(t.Context()); !errors.Is(err, ErrBusy) {
		t.Fatal("third admitted")
	}
	one()
	two()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = s.Admit(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled admitted")
	}
	unlock, err := s.Guard(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Guard(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("guard ignores cancellation")
	}
	unlock()
}
func TestSweepReferenceFailureActiveAgeCursor(t *testing.T) {
	s, path := testStore(t)
	old := time.Now().Add(-2 * time.Hour)
	referenced := publish(t, s)
	orphan := publish(t, s)
	young := publish(t, s)
	active, err := s.Stage(t.Context(), []byte("staged"))
	if err != nil {
		t.Fatal(err)
	}
	defer active.Done()
	for _, name := range []string{referenced, orphan, active.temp} {
		if err := os.Chtimes(filepath.Join(path, name), old, old); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Sweep(t.Context(), time.Now(), func(context.Context, []string) (map[string]bool, error) {
		return nil, errors.New("database unavailable")
	}); err == nil {
		t.Fatal("database failure hidden")
	}
	if _, err := os.Stat(filepath.Join(path, orphan)); err != nil {
		t.Fatal("outage deleted orphan")
	}
	refs := func(context.Context, []string) (map[string]bool, error) {
		return map[string]bool{referenced: true}, nil
	}
	r, err := s.Sweep(t.Context(), time.Now(), refs)
	if err != nil || r.Deleted != 1 {
		t.Fatalf("sweep %v %v", r, err)
	}
	for _, name := range []string{referenced, young, active.temp} {
		if _, err := os.Stat(filepath.Join(path, name)); err != nil {
			t.Fatal("retained file deleted")
		}
	}
	for i := 0; i < 1100; i++ {
		name := fmt.Sprintf("%032x.tmp", i)
		file := filepath.Join(path, name)
		if err := os.WriteFile(file, []byte("orphan"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, old, old); err != nil {
			t.Fatal(err)
		}
	}
	total := 0
	for range 30 {
		r, err = s.Sweep(t.Context(), time.Now(), refs)
		if err != nil || r.Examined > 1000 || r.Deleted > 100 {
			t.Fatalf("bounded sweep %v %v", r, err)
		}
		total += r.Deleted
	}
	if total != 1100 {
		t.Fatalf("cursor starved later entries, deleted=%d", total)
	}
}
