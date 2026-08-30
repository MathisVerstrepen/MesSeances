package schedule

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type refreshObservation struct{ result, stage, reason string }
type captureRefreshObserver struct {
	refreshes []refreshObservation
	freshness [][3]time.Time
	successes []time.Time
	revisions []SnapshotRevision
	completed chan refreshObservation
}

func (o *captureRefreshObserver) ObserveScheduleRefresh(result, stage, reason string, _ time.Duration) {
	observation := refreshObservation{result, stage, reason}
	o.refreshes = append(o.refreshes, observation)
	if o.completed != nil && result != "unchanged" && result != "reloaded" {
		o.completed <- observation
	}
}
func (o *captureRefreshObserver) SetScheduleRevision(schedule, enrichment int64) {
	o.revisions = append(o.revisions, SnapshotRevision{ScheduleVersion: schedule, EnrichmentVersion: enrichment})
}
func (o *captureRefreshObserver) SetScheduleFreshness(generatedAt, windowStart, windowEnd time.Time) {
	o.freshness = append(o.freshness, [3]time.Time{generatedAt, windowStart, windowEnd})
}
func (o *captureRefreshObserver) SetScheduleRefreshLastSuccess(at time.Time) {
	o.successes = append(o.successes, at)
	if o.completed != nil {
		o.completed <- o.refreshes[len(o.refreshes)-1]
	}
}

type fakeSnapshotReader struct {
	mu                sync.Mutex
	version           int64
	enrichmentVersion int64
	locationVersion   int64
	data              Dataset
	versionErr        error
	loadErr           error
	loadVersion       int64
	useLoadVersion    bool
	loads             int
	checks            int
	versionStarted    chan struct{}
	versionRelease    chan struct{}
	loadStarted       chan struct{}
	loadRelease       chan struct{}
}

func (f *fakeSnapshotReader) CurrentRevision(ctx context.Context) (SnapshotRevision, error) {
	f.mu.Lock()
	f.checks++
	started, release := f.versionStarted, f.versionRelease
	revision, err := SnapshotRevision{ScheduleVersion: f.version, EnrichmentVersion: f.enrichmentVersion, TheaterLocationVersion: f.locationVersion}, f.versionErr
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
			return SnapshotRevision{}, ctx.Err()
		}
	}
	return revision, err
}

func (f *fakeSnapshotReader) Load(ctx context.Context) (Dataset, SnapshotRevision, error) {
	f.mu.Lock()
	f.loads++
	started, release := f.loadStarted, f.loadRelease
	data, version, enrichmentVersion, locationVersion, err := cloneDataset(f.data), f.version, f.enrichmentVersion, f.locationVersion, f.loadErr
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
	return data, SnapshotRevision{ScheduleVersion: version, EnrichmentVersion: enrichmentVersion, TheaterLocationVersion: locationVersion}, err
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
	withLocation := testDataset()
	latitude, longitude := 50.63, 3.06
	withLocation.Theaters[0].Latitude, withLocation.Theaters[0].Longitude = &latitude, &longitude
	previous := source.Snapshot()
	reader.mu.Lock()
	reader.locationVersion = 1
	reader.data = withLocation
	reader.mu.Unlock()
	source.refresh(context.Background())
	if got := source.Snapshot(); got == previous || got.data.Theaters[0].Latitude == nil || *got.data.Theaters[0].Latitude != latitude {
		t.Fatal("theater location revision not published")
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

func TestPostgresSourceFreshnessStagesReasonsAndFailureRetention(t *testing.T) {
	const secret = "synthetic-refresh-secret"
	data := testDataset()
	reader := &fakeSnapshotReader{version: 1, data: data}
	observer := &captureRefreshObserver{}
	var logs bytes.Buffer
	source, err := NewPostgresSource(context.Background(), reader, SourceOptions{Observer: observer, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	location, _ := time.LoadLocation(Timezone)
	wantStart, _ := time.ParseInLocation(dateLayout, data.Window.From, location)
	wantThrough, _ := time.ParseInLocation(dateLayout, data.Window.Through, location)
	if len(observer.freshness) != 1 || !observer.freshness[0][0].Equal(data.GeneratedAt) || !observer.freshness[0][1].Equal(wantStart) || !observer.freshness[0][2].Equal(wantThrough.AddDate(0, 0, 1)) || len(observer.successes) != 1 {
		t.Fatalf("initial freshness=%+v successes=%d", observer.freshness, len(observer.successes))
	}
	source.refresh(context.Background())
	if got := observer.refreshes[len(observer.refreshes)-1]; got != (refreshObservation{"unchanged", "none", "none"}) || len(observer.successes) != 2 || len(observer.freshness) != 1 {
		t.Fatalf("unchanged=%+v successes=%d freshness=%d", observer.refreshes, len(observer.successes), len(observer.freshness))
	}

	reader.mu.Lock()
	reader.versionErr = errors.New(secret)
	reader.mu.Unlock()
	source.refresh(context.Background())
	reader.mu.Lock()
	reader.versionErr = nil
	reader.version = 0
	reader.mu.Unlock()
	source.refresh(context.Background())
	reader.mu.Lock()
	reader.version = 2
	reader.loadErr = errors.New(secret)
	reader.mu.Unlock()
	source.refresh(context.Background())
	reader.mu.Lock()
	reader.loadErr = nil
	reader.useLoadVersion = true
	reader.loadVersion = 0
	reader.mu.Unlock()
	source.refresh(context.Background())
	reader.mu.Lock()
	reader.useLoadVersion = false
	reader.data = testDataset()
	reader.data.GeneratedAt = time.Time{}
	reader.mu.Unlock()
	source.refresh(context.Background())

	versionStarted := make(chan struct{}, 1)
	reader.mu.Lock()
	reader.data = testDataset()
	reader.versionStarted = versionStarted
	reader.versionRelease = make(chan struct{})
	reader.mu.Unlock()
	checkCtx, cancelCheck := context.WithCancel(context.Background())
	checkDone := make(chan struct{})
	go func() {
		source.refresh(checkCtx)
		close(checkDone)
	}()
	<-versionStarted
	cancelCheck()
	<-checkDone
	reader.mu.Lock()
	reader.versionStarted = nil
	reader.versionRelease = nil
	reader.mu.Unlock()

	loadStarted := make(chan struct{}, 1)
	reader.mu.Lock()
	reader.loadStarted = loadStarted
	reader.loadRelease = make(chan struct{})
	reader.mu.Unlock()
	loadCtx, cancelLoad := context.WithCancel(context.Background())
	loadDone := make(chan struct{})
	go func() {
		source.refresh(loadCtx)
		close(loadDone)
	}()
	<-loadStarted
	cancelLoad()
	<-loadDone
	reader.mu.Lock()
	reader.loadStarted = nil
	reader.loadRelease = nil
	reader.mu.Unlock()

	wantFailures := []refreshObservation{
		{"check_failed", "revision_check", "read_failed"},
		{"invalid_revision", "revision_check", "invalid_revision"},
		{"load_failed", "snapshot_load", "read_failed"},
		{"invalid_revision", "snapshot_load", "invalid_revision"},
		{"invalid_dataset", "dataset_validation", "invalid_dataset"},
		{"canceled", "revision_check", "canceled"},
		{"canceled", "snapshot_load", "canceled"},
	}
	if len(observer.refreshes) != 1+len(wantFailures) {
		t.Fatalf("refreshes=%+v", observer.refreshes)
	}
	for i, want := range wantFailures {
		if observer.refreshes[i+1] != want {
			t.Fatalf("refresh[%d]=%+v want=%+v", i+1, observer.refreshes[i+1], want)
		}
	}
	if len(observer.freshness) != 1 || len(observer.successes) != 2 || strings.Contains(logs.String(), secret) {
		t.Fatalf("freshness=%d successes=%d logs=%q", len(observer.freshness), len(observer.successes), logs.String())
	}

	replacement := testDataset()
	replacement.GeneratedAt = replacement.GeneratedAt.Add(time.Hour)
	reader.mu.Lock()
	reader.data = replacement
	reader.mu.Unlock()
	source.refresh(context.Background())
	if got := observer.refreshes[len(observer.refreshes)-1]; got != (refreshObservation{"reloaded", "none", "none"}) || len(observer.freshness) != 2 || len(observer.successes) != 3 || !observer.freshness[1][0].Equal(replacement.GeneratedAt) {
		t.Fatalf("reload=%+v freshness=%+v successes=%d", observer.refreshes, observer.freshness, len(observer.successes))
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

func TestPostgresSourcePendingSnapshotTransitionsOnTick(t *testing.T) {
	observer := &captureRefreshObserver{completed: make(chan refreshObservation, 2)}
	var logs bytes.Buffer
	reader := &fakeSnapshotReader{loadErr: fmt.Errorf("load pending: %w", ErrNoCompleteSnapshot), versionErr: ErrNoCompleteSnapshot}
	source, err := NewPostgresSource(context.Background(), reader, SourceOptions{Observer: observer, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	if err != nil || source == nil {
		t.Fatalf("source=%v error=%v", source != nil, err)
	}
	if source.Snapshot() != nil {
		t.Fatalf("initial snapshot=%v", source.Snapshot())
	}
	if len(observer.revisions) != 0 || len(observer.freshness) != 0 || len(observer.successes) != 0 {
		t.Fatalf("pending metrics revisions=%v freshness=%v successes=%v", observer.revisions, observer.freshness, observer.successes)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		source.runTicks(ctx, ticks)
		close(done)
	}()
	ticks <- time.Time{}
	if got := <-observer.completed; got != (refreshObservation{"pending", "revision_check", "no_complete_snapshot"}) {
		t.Fatalf("pending observation=%+v", got)
	}
	if source.Snapshot() != nil || len(observer.revisions) != 0 || len(observer.freshness) != 0 || len(observer.successes) != 0 || logs.Len() != 0 {
		t.Fatalf("pending snapshot=%v revisions=%v freshness=%v successes=%v logs=%q", source.Snapshot(), observer.revisions, observer.freshness, observer.successes, logs.String())
	}

	reader.mu.Lock()
	reader.version = 1
	reader.data = testDataset()
	reader.versionErr = nil
	reader.loadErr = nil
	reader.mu.Unlock()
	ticks <- time.Time{}
	if got := <-observer.completed; got != (refreshObservation{"reloaded", "none", "none"}) {
		t.Fatalf("reload observation=%+v", got)
	}
	if source.Snapshot() == nil || len(observer.revisions) != 1 || len(observer.freshness) != 1 || len(observer.successes) != 1 {
		t.Fatalf("loaded snapshot=%v revisions=%v freshness=%v successes=%v", source.Snapshot() != nil, observer.revisions, observer.freshness, observer.successes)
	}
	cancel()
	<-done
}

func TestPostgresSourceInitialLoadRejectsNonPendingFailures(t *testing.T) {
	invalid := testDataset()
	invalid.GeneratedAt = time.Time{}
	for _, test := range []struct {
		name   string
		reader *fakeSnapshotReader
	}{
		{name: "read failure", reader: &fakeSnapshotReader{loadErr: errors.New("missing")}},
		{name: "invalid revision", reader: &fakeSnapshotReader{data: testDataset()}},
		{name: "invalid dataset", reader: &fakeSnapshotReader{version: 1, data: invalid}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewPostgresSource(context.Background(), test.reader); err == nil {
				t.Fatal("initial failure accepted")
			}
		})
	}
}
