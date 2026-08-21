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
	f.mu.Unlock()
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

func TestPostgresSourceSnapshotIsDetachedZeroAllocationAndNoIO(t *testing.T) {
	reader := &fakeSnapshotReader{version: 1, data: testDataset()}
	source, err := NewPostgresSource(context.Background(), reader)
	if err != nil {
		t.Fatal(err)
	}
	view := source.Snapshot()
	reader.data.Theaters[0].Name = "mutated"
	reader.data.Theaters[0].AvailableDates[0] = "mutated"
	reader.data.Showtimes[0].Movie.Genres = []string{"mutated"}
	if view.data.Theaters[0].Name == "mutated" || view.data.Theaters[0].AvailableDates[0] == "mutated" || len(view.data.Showtimes[0].Movie.Genres) != 0 {
		t.Fatal("snapshot retained caller-owned memory")
	}
	if allocations := testing.AllocsPerRun(1000, func() { _ = source.Snapshot() }); allocations != 0 {
		t.Fatalf("Snapshot allocations=%v", allocations)
	}
	for range 10 {
		if source.Snapshot() != view {
			t.Fatal("snapshot pointer changed without publication")
		}
	}
	reader.mu.Lock()
	loads, checks := reader.loads, reader.checks
	reader.mu.Unlock()
	if loads != 1 || checks != 0 {
		t.Fatalf("loads=%d checks=%d", loads, checks)
	}
}

func TestPostgresSourcePollPublishesScheduleAndEnrichmentRevisions(t *testing.T) {
	reader := &fakeSnapshotReader{version: 1, data: testDataset()}
	source, err := NewPostgresSource(context.Background(), reader)
	if err != nil {
		t.Fatal(err)
	}
	old := source.Snapshot()
	replacement := testDataset()
	replacement.Theaters[0].Name = "Replacement"
	reader.mu.Lock()
	reader.version = 2
	reader.data = replacement
	reader.mu.Unlock()
	source.refresh(context.Background())
	current := source.Snapshot()
	if current == old || current.data.Theaters[0].Name != "Replacement" || old.data.Theaters[0].Name == "Replacement" {
		t.Fatal("schedule revision publication did not preserve immutable old view")
	}
	enriched := testDataset()
	enriched.Showtimes[0].Movie.Enrichment = &MovieEnrichment{TMDBID: 42, Genres: []string{"Drame"}, BackdropURL: "https://image.tmdb.org/t/p/w780/a.jpg"}
	reader.mu.Lock()
	reader.enrichmentVersion = 1
	reader.data = enriched
	reader.mu.Unlock()
	source.refresh(context.Background())
	if got := source.Snapshot(); got == current || got.data.Showtimes[0].Movie.Enrichment == nil || got.data.Showtimes[0].Movie.Enrichment.TMDBID != 42 {
		t.Fatal("enrichment revision not published")
	}
}

func TestPostgresSourcePollFailuresRetainLastGoodAndRetry(t *testing.T) {
	reader := &fakeSnapshotReader{version: 1, data: testDataset()}
	source, err := NewPostgresSource(context.Background(), reader)
	if err != nil {
		t.Fatal(err)
	}
	want := source.Snapshot()
	reader.mu.Lock()
	reader.version = 2
	reader.versionErr = errors.New("down")
	reader.mu.Unlock()
	source.refresh(context.Background())
	if source.Snapshot() != want {
		t.Fatal("revision failure replaced last good")
	}
	reader.mu.Lock()
	reader.versionErr = nil
	reader.loadErr = errors.New("bad load")
	reader.mu.Unlock()
	source.refresh(context.Background())
	if source.Snapshot() != want {
		t.Fatal("load failure replaced last good")
	}
	reader.mu.Lock()
	reader.loadErr = nil
	reader.data = testDataset()
	reader.data.GeneratedAt = time.Time{}
	reader.mu.Unlock()
	source.refresh(context.Background())
	if source.Snapshot() != want {
		t.Fatal("invalid data replaced last good")
	}
	reader.mu.Lock()
	reader.data = testDataset()
	reader.useLoadVersion = true
	reader.loadVersion = 0
	reader.mu.Unlock()
	source.refresh(context.Background())
	if source.Snapshot() != want {
		t.Fatal("invalid loaded revision replaced last good")
	}
	reader.mu.Lock()
	reader.useLoadVersion = false
	reader.mu.Unlock()
	source.refresh(context.Background())
	if source.Snapshot() == want {
		t.Fatal("failed revision was not retried")
	}
}

func TestPostgresSourceBlockedPollDoesNotBlockSnapshotAndCancellationStopsWorker(t *testing.T) {
	reader := &fakeSnapshotReader{version: 1, data: testDataset()}
	source, err := NewPostgresSource(context.Background(), reader)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	reader.mu.Lock()
	reader.version = 2
	reader.loadStarted = started
	reader.loadRelease = release
	reader.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time, 1)
	done := make(chan struct{})
	go func() {
		source.runTicks(ctx, ticks)
		close(done)
	}()
	ticks <- time.Time{}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("poll did not start")
	}
	want := source.Snapshot()
	quick := make(chan *SnapshotView, 1)
	go func() { quick <- source.Snapshot() }()
	select {
	case got := <-quick:
		if got != want {
			t.Fatal("blocked poll leaked unpublished view")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Snapshot blocked on refresh")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled poll did not exit")
	}
}

func TestPostgresSourceInitialLoadFailure(t *testing.T) {
	reader := &fakeSnapshotReader{loadErr: errors.New("missing")}
	if _, err := NewPostgresSource(context.Background(), reader); err == nil {
		t.Fatal("initial failure accepted")
	}
}
