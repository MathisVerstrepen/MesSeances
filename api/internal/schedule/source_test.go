package schedule

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeSnapshotReader struct {
	mu                sync.Mutex
	version           int64
	enrichmentVersion int64
	data              Dataset
	versionErr        error
	loadErr           error
	loadVersion       int64
	useLoadVersion    bool
	loads             int
	checks            int
	loadStarted       chan struct{}
	loadRelease       chan struct{}
	loadDeadline      chan time.Duration
}

func (f *fakeSnapshotReader) CurrentRevision(context.Context) (SnapshotRevision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	return SnapshotRevision{ScheduleVersion: f.version, EnrichmentVersion: f.enrichmentVersion}, f.versionErr
}
func (f *fakeSnapshotReader) Load(ctx context.Context) (Dataset, SnapshotRevision, error) {
	f.mu.Lock()
	f.loads++
	started, release := f.loadStarted, f.loadRelease
	data, version, enrichmentVersion, err := cloneDataset(f.data), f.version, f.enrichmentVersion, f.loadErr
	if f.useLoadVersion {
		version = f.loadVersion
	}
	deadlineObserved := f.loadDeadline
	f.mu.Unlock()
	if deadlineObserved != nil {
		remaining := time.Duration(-1)
		if deadline, ok := ctx.Deadline(); ok {
			remaining = time.Until(deadline)
		}
		deadlineObserved <- remaining
	}
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return Dataset{}, SnapshotRevision{}, ctx.Err()
		}
	}
	return data, SnapshotRevision{ScheduleVersion: version, EnrichmentVersion: enrichmentVersion}, err
}

func TestPostgresSourceInitialLoadAndImmutableClone(t *testing.T) {
	reader := &fakeSnapshotReader{version: 1, data: testDataset()}
	source, err := NewPostgresSource(context.Background(), reader)
	if err != nil {
		t.Fatal(err)
	}
	copy := source.Snapshot()
	copy.Theaters[0].Name = "mutated"
	if source.Snapshot().Theaters[0].Name == "mutated" {
		t.Fatal("snapshot mutable")
	}
	reader.mu.Lock()
	loads, checks := reader.loads, reader.checks
	reader.mu.Unlock()
	if loads != 1 || checks != 2 {
		t.Fatalf("loads=%d checks=%d", loads, checks)
	}
}

func TestPostgresSourceRefreshAndLastGoodFailures(t *testing.T) {
	reader := &fakeSnapshotReader{version: 1, data: testDataset()}
	source, err := NewPostgresSource(context.Background(), reader)
	if err != nil {
		t.Fatal(err)
	}
	replacement := testDataset()
	replacement.Theaters[0].Name = "Replacement"
	replacement.GeneratedAt = replacement.GeneratedAt.Add(time.Minute)
	reader.mu.Lock()
	reader.version = 2
	reader.data = replacement
	reader.mu.Unlock()
	if got := source.Snapshot().Theaters[0].Name; got != "Replacement" {
		t.Fatalf("name=%q", got)
	}
	reader.mu.Lock()
	reader.version = 3
	reader.versionErr = errors.New("down")
	reader.mu.Unlock()
	if got := source.Snapshot().Theaters[0].Name; got != "Replacement" {
		t.Fatal("version failure lost last good")
	}
	reader.mu.Lock()
	reader.versionErr = nil
	reader.loadErr = errors.New("bad load")
	reader.mu.Unlock()
	if got := source.Snapshot().Theaters[0].Name; got != "Replacement" {
		t.Fatal("load failure lost last good")
	}
	reader.mu.Lock()
	reader.loadErr = nil
	reader.version = 0
	reader.mu.Unlock()
	if got := source.Snapshot().Theaters[0].Name; got != "Replacement" {
		t.Fatal("nonpositive version lost last good")
	}
}

func TestPostgresSourceRefreshesOnEnrichmentRevision(t *testing.T) {
	reader := &fakeSnapshotReader{version: 1, data: testDataset()}
	source, err := NewPostgresSource(context.Background(), reader)
	if err != nil {
		t.Fatal(err)
	}
	enriched := testDataset()
	enriched.Showtimes[0].Movie.Enrichment = &MovieEnrichment{TMDBID: 42, Genres: []string{"Drame"}, PosterURL: "https://image.tmdb.org/t/p/w500/a.jpg", BackdropURL: "https://image.tmdb.org/t/p/w780/a.jpg"}
	reader.mu.Lock()
	reader.enrichmentVersion = 1
	reader.data = enriched
	reader.mu.Unlock()
	snapshot := source.Snapshot()
	if snapshot.Showtimes[0].Movie.Enrichment == nil || snapshot.Showtimes[0].Movie.Enrichment.TMDBID != 42 || snapshot.Showtimes[0].Movie.Enrichment.BackdropURL != "https://image.tmdb.org/t/p/w780/a.jpg" {
		t.Fatalf("movie=%+v", snapshot.Showtimes[0].Movie)
	}
	snapshot.Showtimes[0].Movie.Enrichment.Genres[0] = "mutated"
	if source.Snapshot().Showtimes[0].Movie.Enrichment.Genres[0] != "Drame" {
		t.Fatal("enrichment genres mutable")
	}
}

func TestPostgresSourceInvalidBackdropRetainsLastGood(t *testing.T) {
	reader := &fakeSnapshotReader{version: 1, data: testDataset()}
	source, err := NewPostgresSource(context.Background(), reader)
	if err != nil {
		t.Fatal(err)
	}
	invalid := testDataset()
	invalid.Showtimes[0].Movie.Enrichment = &MovieEnrichment{TMDBID: 42, BackdropURL: "https://evil.example/t/p/w780/a.jpg"}
	reader.mu.Lock()
	reader.enrichmentVersion = 1
	reader.data = invalid
	reader.mu.Unlock()
	if snapshot := source.Snapshot(); snapshot.Showtimes[0].Movie.Enrichment != nil {
		t.Fatalf("invalid enrichment replaced last good: %+v", snapshot.Showtimes[0].Movie.Enrichment)
	}
}

func TestPostgresSourceRefreshLoadDeadlineRetainsLastGood(t *testing.T) {
	reader := &fakeSnapshotReader{version: 1, data: testDataset()}
	source, err := NewPostgresSource(context.Background(), reader)
	if err != nil {
		t.Fatal(err)
	}

	deadlineObserved := make(chan time.Duration, 1)
	release := make(chan struct{})
	reader.mu.Lock()
	reader.version = 2
	reader.loadErr = context.DeadlineExceeded
	reader.loadDeadline = deadlineObserved
	reader.loadRelease = release
	reader.mu.Unlock()

	done := make(chan Dataset, 1)
	go func() { done <- source.Snapshot() }()

	var remaining time.Duration
	select {
	case remaining = <-deadlineObserved:
	case <-time.After(time.Second):
		t.Fatal("changed-version load did not start")
	}
	if remaining < 4*time.Second || remaining > 5*time.Second {
		t.Fatalf("load context deadline remaining=%s, want production 5s timeout", remaining)
	}
	select {
	case <-done:
		t.Fatal("refresh returned before controlled deadline failure")
	default:
	}
	close(release)
	select {
	case snapshot := <-done:
		if snapshot.Theaters[0].Name != testDataset().Theaters[0].Name {
			t.Fatal("load deadline failure lost last good")
		}
	case <-time.After(time.Second):
		t.Fatal("refresh did not return promptly after controlled deadline failure")
	}
}

func TestPostgresSourceRefreshNonpositiveLoadedVersionRetainsLastGood(t *testing.T) {
	reader := &fakeSnapshotReader{version: 1, data: testDataset()}
	source, err := NewPostgresSource(context.Background(), reader)
	if err != nil {
		t.Fatal(err)
	}

	replacement := testDataset()
	replacement.Theaters[0].Name = "Rejected replacement"
	replacement.GeneratedAt = replacement.GeneratedAt.Add(time.Minute)
	reader.mu.Lock()
	reader.version = 2
	reader.data = replacement
	reader.useLoadVersion = true
	reader.loadVersion = 0
	reader.mu.Unlock()

	if got := source.Snapshot().Theaters[0].Name; got != testDataset().Theaters[0].Name {
		t.Fatalf("nonpositive loaded version replaced last good with %q", got)
	}
	reader.mu.Lock()
	loads := reader.loads
	reader.mu.Unlock()
	if loads != 2 {
		t.Fatalf("changed version did not trigger load: loads=%d", loads)
	}
}

func TestPostgresSourceRefreshInvalidDatasetRetainsLastGood(t *testing.T) {
	reader := &fakeSnapshotReader{version: 1, data: testDataset()}
	source, err := NewPostgresSource(context.Background(), reader)
	if err != nil {
		t.Fatal(err)
	}

	invalid := testDataset()
	invalid.GeneratedAt = time.Time{}
	invalid.Theaters[0].Name = "Rejected replacement"
	reader.mu.Lock()
	reader.version = 2
	reader.data = invalid
	reader.mu.Unlock()

	if got := source.Snapshot().Theaters[0].Name; got != testDataset().Theaters[0].Name {
		t.Fatalf("invalid loaded dataset replaced last good with %q", got)
	}
	reader.mu.Lock()
	loads := reader.loads
	reader.mu.Unlock()
	if loads != 2 {
		t.Fatalf("changed version did not trigger load: loads=%d", loads)
	}
}

func TestPostgresSourceInitialLoadFailure(t *testing.T) {
	reader := &fakeSnapshotReader{loadErr: errors.New("missing")}
	if _, err := NewPostgresSource(context.Background(), reader); err == nil {
		t.Fatal("initial failure accepted")
	}
}

func TestPostgresSourceConcurrentRefreshNonblocking(t *testing.T) {
	reader := &fakeSnapshotReader{version: 1, data: testDataset()}
	source, err := NewPostgresSource(context.Background(), reader)
	if err != nil {
		t.Fatal(err)
	}
	replacement := testDataset()
	replacement.Theaters[0].Name = "Replacement"
	replacement.GeneratedAt = replacement.GeneratedAt.Add(time.Minute)
	started, release := make(chan struct{}, 1), make(chan struct{})
	reader.mu.Lock()
	reader.version = 2
	reader.data = replacement
	reader.loadStarted = started
	reader.loadRelease = release
	reader.mu.Unlock()
	done := make(chan Dataset, 1)
	go func() { done <- source.Snapshot() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	quick := make(chan Dataset, 1)
	go func() { quick <- source.Snapshot() }()
	select {
	case snapshot := <-quick:
		if snapshot.Theaters[0].Name == "Replacement" {
			t.Fatal("blocked refresh leaked uncommitted replacement")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("concurrent snapshot blocked")
	}
	close(release)
	select {
	case snapshot := <-done:
		if snapshot.Theaters[0].Name != "Replacement" {
			t.Fatal("replacement not returned")
		}
	case <-time.After(time.Second):
		t.Fatal("refresh blocked")
	}
	if source.Snapshot().Theaters[0].Name != "Replacement" {
		t.Fatal("replacement not retained")
	}
	reader.mu.Lock()
	loads := reader.loads
	reader.mu.Unlock()
	if loads != 2 {
		t.Fatalf("loads=%d", loads)
	}
}
