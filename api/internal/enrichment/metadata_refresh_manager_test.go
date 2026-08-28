package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/tmdb"
)

func TestMetadataRefreshManagerRunningTerminalCountersAndSnapshotCopies(t *testing.T) {
	startedAt := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	clock := &metadataRefreshClock{times: []time.Time{startedAt, startedAt.Add(30 * time.Second), startedAt.Add(time.Minute)}}
	store := &metadataRefreshStore{ids: []int64{42}, metadata: map[int64]Metadata{}}
	provider := &managedMetadataProvider{started: make(chan struct{}), release: make(chan struct{})}
	service := NewMetadataRefreshService(store, provider, clock.Now, nil)
	manager, err := NewMetadataRefreshManager(context.Background(), service, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { manager.Close() }()

	accepted, err := manager.Start()
	if err != nil || accepted.State != MetadataRefreshRunning || !accepted.StartedAt.Equal(startedAt) || accepted.FinishedAt != nil || accepted.Summary != nil || accepted.ErrorCode != nil {
		t.Fatalf("accepted=%+v err=%v", accepted, err)
	}
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not reach provider")
	}
	if _, err := manager.Start(); !errors.Is(err, ErrMetadataRefreshInProgress) {
		t.Fatalf("second start error=%v", err)
	}
	running := manager.Snapshot()
	if running == nil || running.State != MetadataRefreshRunning || running.FinishedAt != nil {
		t.Fatalf("running snapshot=%+v", running)
	}
	close(provider.release)
	terminal := waitForMetadataRefreshState(t, manager, MetadataRefreshSucceeded)
	wantSummary := MetadataRefreshSummary{Processed: 1, Updated: 1}
	if terminal.Summary == nil || *terminal.Summary != wantSummary || terminal.FinishedAt == nil || !terminal.FinishedAt.Equal(startedAt.Add(time.Minute)) || terminal.ErrorCode != nil {
		t.Fatalf("terminal=%+v", terminal)
	}

	terminal.Summary.Processed = 999
	*terminal.FinishedAt = time.Time{}
	copy := manager.Snapshot()
	if copy.Summary == nil || *copy.Summary != wantSummary || copy.FinishedAt == nil || copy.FinishedAt.IsZero() {
		t.Fatalf("snapshot was aliased: %+v", copy)
	}
}

func TestMetadataRefreshManagerSharesGateWithRerun(t *testing.T) {
	gate := NewTMDBRunGate()
	provider := &sharedGateProvider{started: make(chan struct{}), release: make(chan struct{})}
	runStore := &rerunStore{movies: []Movie{{ProviderID: "10", Title: "Film", RuntimeMinutes: 90}}}
	rerun := NewRerunService(runStore, NewMatcher(newMemoryStore(), provider, func() time.Time { return matcherNow }), gate)
	refreshStore := &metadataRefreshStore{ids: []int64{20}, metadata: map[int64]Metadata{}}
	manager, err := NewMetadataRefreshManager(context.Background(), NewMetadataRefreshService(refreshStore, provider, func() time.Time { return matcherNow }, gate), func() time.Time { return matcherNow })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { manager.Close() }()

	rerunDone := make(chan error, 1)
	go func() {
		_, err := rerun.Rerun(context.Background())
		rerunDone <- err
	}()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("rerun did not reach provider")
	}
	if _, err := manager.Start(); !errors.Is(err, ErrMetadataRefreshInProgress) {
		t.Fatalf("manager start during rerun error=%v", err)
	}
	close(provider.release)
	if err := <-rerunDone; err != nil {
		t.Fatalf("rerun error=%v", err)
	}

	provider = &sharedGateProvider{started: make(chan struct{}), release: make(chan struct{})}
	manager.Close()
	manager, err = NewMetadataRefreshManager(context.Background(), NewMetadataRefreshService(refreshStore, provider, func() time.Time { return matcherNow }, gate), func() time.Time { return matcherNow })
	if err != nil {
		t.Fatal(err)
	}
	running, err := manager.Start()
	if err != nil || running.State != MetadataRefreshRunning {
		t.Fatalf("manager start=%+v err=%v", running, err)
	}
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not reach provider")
	}
	if _, err := rerun.Rerun(context.Background()); !errors.Is(err, ErrRerunInProgress) {
		t.Fatalf("rerun during managed refresh error=%v", err)
	}
	close(provider.release)
	waitForMetadataRefreshState(t, manager, MetadataRefreshSucceeded)
}

func TestMetadataRefreshManagerFailureIsSanitizedAndShutdownCancels(t *testing.T) {
	appCtx, cancelApp := context.WithCancel(context.Background())
	store := &metadataRefreshStore{ids: []int64{1}, metadata: map[int64]Metadata{}}
	provider := &managedMetadataProvider{started: make(chan struct{}), waitForCancel: true, secret: "provider-secret"}
	manager, err := NewMetadataRefreshManager(appCtx, NewMetadataRefreshService(store, provider, time.Now, nil), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not reach provider")
	}
	cancelApp()
	manager.Close()
	status := manager.Snapshot()
	if status == nil || status.State != MetadataRefreshFailed || status.FinishedAt == nil || status.Summary != nil || status.ErrorCode == nil || *status.ErrorCode != MetadataRefreshFailure {
		t.Fatalf("failed status=%+v", status)
	}
	if _, err := manager.Start(); !errors.Is(err, ErrMetadataRefreshUnavailable) {
		t.Fatalf("start after close error=%v", err)
	}
}

func TestMetadataRefreshManagerFailureStatusDoesNotExposeCause(t *testing.T) {
	secret := "synthetic-database-secret"
	store := &metadataRefreshStore{idsErr: errors.New(secret)}
	provider := &managedMetadataProvider{started: make(chan struct{}), release: make(chan struct{})}
	manager, err := NewMetadataRefreshManager(context.Background(), NewMetadataRefreshService(store, provider, time.Now, nil), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if _, err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	status := waitForMetadataRefreshState(t, manager, MetadataRefreshFailed)
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) || status.ErrorCode == nil || *status.ErrorCode != MetadataRefreshFailure || status.Summary != nil {
		t.Fatalf("unsafe failed status=%s", encoded)
	}
}

func TestMetadataRefreshManagerSnapshotConcurrentAccess(t *testing.T) {
	store := &metadataRefreshStore{ids: []int64{1}, metadata: map[int64]Metadata{}}
	provider := &managedMetadataProvider{started: make(chan struct{}), release: make(chan struct{})}
	manager, err := NewMetadataRefreshManager(context.Background(), NewMetadataRefreshService(store, provider, time.Now, nil), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if _, err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	<-provider.started

	var snapshots sync.WaitGroup
	for range 32 {
		snapshots.Add(1)
		go func() {
			defer snapshots.Done()
			for range 100 {
				status := manager.Snapshot()
				if status == nil {
					t.Error("missing running snapshot")
					return
				}
				status.StartedAt = time.Time{}
			}
		}()
	}
	close(provider.release)
	snapshots.Wait()
	terminal := waitForMetadataRefreshState(t, manager, MetadataRefreshSucceeded)
	if terminal.StartedAt.IsZero() {
		t.Fatal("snapshot mutation changed manager state")
	}
}

func TestNewMetadataRefreshManagerRejectsMissingDependencies(t *testing.T) {
	if _, err := NewMetadataRefreshManager(nil, nil, nil); err == nil {
		t.Fatal("nil dependencies accepted")
	}
	if (*MetadataRefreshManager)(nil).Snapshot() != nil {
		t.Fatal("nil manager returned status")
	}
	if _, err := (*MetadataRefreshManager)(nil).Start(); !errors.Is(err, ErrMetadataRefreshUnavailable) {
		t.Fatalf("nil manager start error=%v", err)
	}
}

type managedMetadataProvider struct {
	started       chan struct{}
	release       chan struct{}
	waitForCancel bool
	secret        string
	once          sync.Once
}

func (p *managedMetadataProvider) Details(ctx context.Context, id int64) (tmdb.Details, error) {
	p.once.Do(func() { close(p.started) })
	if p.waitForCancel {
		<-ctx.Done()
		return tmdb.Details{}, errors.New(p.secret)
	}
	select {
	case <-p.release:
		return tmdb.Details{ID: id, OriginalTitle: "Original", Title: "Film", Runtime: 90}, nil
	case <-ctx.Done():
		return tmdb.Details{}, ctx.Err()
	}
}

type metadataRefreshClock struct {
	mu    sync.Mutex
	times []time.Time
}

func (c *metadataRefreshClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.times) == 0 {
		panic("metadata refresh test clock exhausted")
	}
	now := c.times[0]
	c.times = c.times[1:]
	return now
}

func waitForMetadataRefreshState(t *testing.T, manager *MetadataRefreshManager, state MetadataRefreshState) *MetadataRefreshStatus {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		status := manager.Snapshot()
		if status != nil && status.State == state {
			return status
		}
		if time.Now().After(deadline) {
			t.Fatalf("metadata refresh did not reach %q: %+v", state, status)
		}
		time.Sleep(time.Millisecond)
	}
}
