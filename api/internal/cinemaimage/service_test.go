package cinemaimage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type memoryRepository struct {
	mu        sync.Mutex
	records   map[Identity]Record
	present   map[Identity]bool
	ambiguous bool
}

func (r *memoryRepository) List(context.Context, ListQuery) ([]Theater, int, error) {
	return []Theater{}, 0, nil
}
func (r *memoryRepository) Current(_ context.Context, id Identity) (Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.present[id] {
		return Record{}, ErrNotFound
	}
	return r.records[id], nil
}
func (r *memoryRepository) Save(_ context.Context, id Identity, expected int64, next Record) (Record, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.save(id, expected, &next)
}
func (r *memoryRepository) Remove(_ context.Context, id Identity, expected int64) (Record, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.save(id, expected, nil)
}
func (r *memoryRepository) save(id Identity, expected int64, next *Record) (Record, string, error) {
	if !r.present[id] {
		return Record{}, "", ErrNotFound
	}
	old := r.records[id]
	if old.Revision != expected {
		return Record{}, "", ErrConflict
	}
	if next == nil && old.Key == "" {
		return old, "", nil
	}
	if expected == MaxRevision {
		return Record{}, "", ErrStorage
	}
	n := Record{Revision: expected + 1}
	if next != nil {
		n = *next
		n.Revision = expected + 1
	}
	r.records[id] = n
	if r.ambiguous {
		return Record{}, "", ErrStorage
	}
	return n, old.Key, nil
}
func (r *memoryRepository) References(_ context.Context, names []string) (map[string]bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]bool{}
	for _, n := range names {
		for _, record := range r.records {
			if record.Key == n {
				out[n] = true
			}
		}
	}
	return out, nil
}

type syntheticImporter struct {
	bytes []byte
	calls int
	err   error
}

func (f *syntheticImporter) Fetch(context.Context, string) ([]byte, string, error) {
	f.calls++
	return f.bytes, "image/png", f.err
}
func serviceFixture(t *testing.T) (*Service, *memoryRepository, Identity) {
	t.Helper()
	id := Identity{"ugc", "25"}
	r := &memoryRepository{records: map[Identity]Record{}, present: map[Identity]bool{id: true}}
	return NewService(r, testStore(t), nil, nil), r, id
}
func upload(t *testing.T, s *Service, id Identity, expected int64) (Result, error) {
	t.Helper()
	b := pngPhoto(t, 20, 10)
	return s.Upload(context.Background(), id, func() (int64, []byte, string, error) { return expected, b, "image/png", nil })
}
func TestServiceUploadReplaceRemoveAndConflict(t *testing.T) {
	s, r, id := serviceFixture(t)
	ctx := context.Background()
	if out, e := s.Remove(ctx, id, 0); e != nil || out.ImageRevision != 0 || len(r.records) != 0 {
		t.Fatal("empty removal created row", e)
	}
	first, e := upload(t, s, id, 0)
	if e != nil || first.ImageRevision != 1 || first.Image == nil {
		t.Fatal(e)
	}
	old := r.records[id]
	if _, e = upload(t, s, id, 0); !errors.Is(e, ErrConflict) || r.records[id] != old {
		t.Fatal("stale edit", e)
	}
	if _, e = s.Read(ctx, id, 1); e != nil {
		t.Fatal(e)
	}
	second, e := upload(t, s, id, 1)
	if e != nil || second.ImageRevision != 2 {
		t.Fatal(e)
	}
	if _, e = s.media.root.Stat(old.Key); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("predecessor retained")
	}
	if _, e = s.Read(ctx, id, 1); !errors.Is(e, ErrImageNotFound) {
		t.Fatal(e)
	}
	removed, e := s.Remove(ctx, id, 2)
	if e != nil || removed.ImageRevision != 3 || removed.Image != nil {
		t.Fatal(e)
	}
	if _, e = s.Remove(ctx, id, 2); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if out, e := s.Remove(ctx, id, 3); e != nil || out.ImageRevision != 3 {
		t.Fatal("empty no-op", e)
	}
	if _, e = s.Read(ctx, id, 3); !errors.Is(e, ErrImageNotFound) {
		t.Fatal(e)
	}
	r.records[id] = Record{Revision: MaxRevision}
	if _, e = upload(t, s, id, MaxRevision); !errors.Is(e, ErrStorage) {
		t.Fatal(e)
	}
	if out, e := s.Remove(ctx, id, MaxRevision); e != nil || out.ImageRevision != MaxRevision {
		t.Fatal("max empty no-op", e)
	}
}
func TestServiceAdmissionAndFailurePreservePhoto(t *testing.T) {
	s, r, id := serviceFixture(t)
	ctx := context.Background()
	if _, e := upload(t, s, id, 0); e != nil {
		t.Fatal(e)
	}
	old := r.records[id]
	release, e := s.media.Admit(ctx)
	if e != nil {
		t.Fatal(e)
	}
	called := false
	_, e = s.Upload(ctx, id, func() (int64, []byte, string, error) { called = true; return 1, nil, "", nil })
	if !errors.Is(e, ErrBusy) || called {
		t.Fatal("busy body read", e)
	}
	f := &syntheticImporter{bytes: pngPhoto(t, 20, 10)}
	s.importer = f
	if _, e = s.Import(ctx, id, 1, "https://image.example/photo"); !errors.Is(e, ErrBusy) || f.calls != 0 {
		t.Fatal(e)
	}
	release()
	if _, e = s.Upload(ctx, id, func() (int64, []byte, string, error) { return 1, []byte("broken"), "image/png", nil }); !errors.Is(e, ErrUnsupported) || r.records[id] != old {
		t.Fatal(e)
	}
	f.err = ErrDownload
	if _, e = s.Import(ctx, id, 1, "https://image.example/photo"); !errors.Is(e, ErrDownload) || r.records[id] != old {
		t.Fatal(e)
	}
	s.importer = nil
	if _, e = s.Import(ctx, id, 1, "https://image.example/photo"); !errors.Is(e, ErrImportUnavailable) {
		t.Fatal(e)
	}
	r.ambiguous = true
	if _, e = upload(t, s, id, 1); !errors.Is(e, ErrStorage) {
		t.Fatal(e)
	}
	if _, e = s.media.root.Stat(old.Key); e != nil {
		t.Fatal("old removed after ambiguous commit")
	}
	if _, e = s.media.root.Stat(r.records[id].Key); e != nil {
		t.Fatal("candidate removed after ambiguous commit")
	}
}
func TestServiceConcurrentFirstWrites(t *testing.T) {
	s, _, id := serviceFixture(t)
	b := pngPhoto(t, 20, 10)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Upload(context.Background(), id, func() (int64, []byte, string, error) { return 0, b, "image/png", nil })
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		} else if !errors.Is(e, ErrBusy) && !errors.Is(e, ErrConflict) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatalf("successes %d", success)
	}
}
func TestServiceImportUsesSameNormalization(t *testing.T) {
	s, r, id := serviceFixture(t)
	s.importer = &syntheticImporter{bytes: pngPhoto(t, 32, 16)}
	out, e := s.Import(context.Background(), id, 0, "https://image.example/photo")
	if e != nil || out.Image == nil || out.Image.Width != 32 {
		t.Fatal(e)
	}
	if _, e = s.Read(context.Background(), id, 1); e != nil {
		t.Fatal(e)
	}
	r.present[id] = false
	if _, e = s.List(context.Background(), ListQuery{Limit: 20}); e != nil {
		t.Fatal(e)
	}
	called := false
	if _, e = s.Upload(context.Background(), id, func() (int64, []byte, string, error) { called = true; return 0, nil, "", nil }); !errors.Is(e, ErrNotFound) || called {
		t.Fatal(e)
	}
}

func TestServicePostCommitUnlinkFailureIsDeferred(t *testing.T) {
	s, r, id := serviceFixture(t)
	if _, e := upload(t, s, id, 0); e != nil {
		t.Fatal(e)
	}
	old := r.records[id]
	// Local fixture permission fault leaves metadata commit authoritative.
	if e := os.Chmod(filepath.Join(s.media.root.Name(), old.Key), 0644); e != nil {
		t.Fatal(e)
	}
	out, e := upload(t, s, id, 1)
	if e != nil || out.ImageRevision != 2 {
		t.Fatal("unlink error invalidated commit", e)
	}
	if _, e = s.media.root.Stat(old.Key); e != nil {
		t.Fatal("unsafe predecessor removed")
	}
	if _, e = s.Read(context.Background(), id, 2); e != nil {
		t.Fatal(e)
	}
}
