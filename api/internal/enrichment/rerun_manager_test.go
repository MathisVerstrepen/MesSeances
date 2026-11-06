package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type rerunMatcherFunc func(context.Context, []Movie) (Summary, error)

func (f rerunMatcherFunc) ForceRun(ctx context.Context, movies []Movie) (Summary, error) {
	return f(ctx, movies)
}

func waitForRerunState(t *testing.T, manager *RerunManager, state RerunState) *RerunStatus {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		status := manager.Snapshot()
		if status != nil && status.State == state {
			return status
		}
		select {
		case <-deadline:
			t.Fatalf("rerun never reached %s", state)
		case <-time.After(time.Millisecond):
		}
	}
}

func TestRerunManagerReturnsBeforeSlowWorkAndSharesGate(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	gate := NewTMDBRunGate()
	service := NewRerunService(&rerunStore{movies: []Movie{{ProviderID: "1"}}}, rerunMatcherFunc(func(ctx context.Context, _ []Movie) (Summary, error) {
		once.Do(func() { close(started) })
		select {
		case <-release:
			return Summary{Matched: 1}, nil
		case <-ctx.Done():
			return Summary{}, ctx.Err()
		}
	}), gate)
	manager, err := NewRerunManager(t.Context(), service, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if manager.Snapshot() != nil {
		t.Fatal("unexpected initial job")
	}
	accepted, err := manager.Start()
	if err != nil || accepted.State != RerunRunning || accepted.Summary != nil || accepted.FinishedAt != nil {
		t.Fatalf("accepted=%+v err=%v", accepted, err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	if _, err := manager.Start(); !errors.Is(err, ErrRerunInProgress) {
		t.Fatalf("duplicate=%v", err)
	}
	if _, err := service.Rerun(t.Context()); !errors.Is(err, ErrRerunInProgress) {
		t.Fatalf("shared gate=%v", err)
	}
	refresh := NewMetadataRefreshService(&metadataRefreshStore{}, &metadataRefreshProvider{}, nil, gate)
	if _, err := refresh.Refresh(t.Context()); !errors.Is(err, ErrMetadataRefreshInProgress) {
		t.Fatalf("metadata gate=%v", err)
	}
	close(release)
	status := waitForRerunState(t, manager, RerunSucceeded)
	if status.Summary == nil || status.Summary.Processed != 1 || status.Summary.Matched != 1 || status.FinishedAt == nil || accepted.State != RerunRunning {
		t.Fatalf("status=%+v accepted=%+v", status, accepted)
	}
	status.Summary.Matched = 99
	*status.FinishedAt = time.Time{}
	if fresh := manager.Snapshot(); fresh.Summary.Matched != 1 || fresh.FinishedAt.IsZero() {
		t.Fatal("snapshot aliases manager state")
	}
	if _, err := manager.Start(); err != nil {
		t.Fatalf("restart=%v", err)
	}
	waitForRerunState(t, manager, RerunSucceeded)
}

func TestRerunManagerFailuresAreSanitizedAndReleaseGate(t *testing.T) {
	for _, panicRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "panic"}[panicRun], func(t *testing.T) {
			gate := NewTMDBRunGate()
			manager, err := NewRerunManager(t.Context(), NewRerunService(&rerunStore{}, rerunMatcherFunc(func(context.Context, []Movie) (Summary, error) {
				if panicRun {
					panic("synthetic-secret")
				}
				return Summary{}, errors.New("synthetic-secret")
			}), gate), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Close()
			if _, err := manager.Start(); err != nil {
				t.Fatal(err)
			}
			status := waitForRerunState(t, manager, RerunFailed)
			encoded, err := json.Marshal(status)
			if err != nil || strings.Contains(string(encoded), "synthetic-secret") || status.Summary != nil || status.ErrorCode == nil || *status.ErrorCode != RerunFailure || status.FinishedAt == nil {
				t.Fatalf("status=%s err=%v", encoded, err)
			}
			if !gate.tryAcquire() {
				t.Fatal("gate not released")
			}
			gate.release()
		})
	}
}

func TestRerunManagerCloseCancelsAndWaitsForWorker(t *testing.T) {
	started, finished := make(chan struct{}), make(chan struct{})
	manager, err := NewRerunManager(t.Context(), NewRerunService(&rerunStore{}, rerunMatcherFunc(func(ctx context.Context, _ []Movie) (Summary, error) {
		close(started)
		<-ctx.Done()
		close(finished)
		return Summary{}, ctx.Err()
	}), nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if _, err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	manager.Close()
	select {
	case <-finished:
	default:
		t.Fatal("close returned before worker stopped")
	}
	if manager.Snapshot().State != RerunFailed {
		t.Fatal("missing terminal state")
	}
	if _, err := manager.Start(); !errors.Is(err, ErrRerunUnavailable) {
		t.Fatalf("closed start=%v", err)
	}
}
