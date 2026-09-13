package enrichment

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"messeances/api/internal/syncschedule"
)

type upcomingTestLease struct {
	released atomic.Int32
	err      error
}

func (l *upcomingTestLease) Release(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	l.released.Add(1)
	return l.err
}

type upcomingTestLocker struct {
	lease *upcomingTestLease
	err   error
}

func (l upcomingTestLocker) Acquire(context.Context) (UpcomingLease, error) {
	if l.err != nil {
		return nil, l.err
	}
	return l.lease, nil
}

func TestUpcomingManagerAdmissionCleanup(t *testing.T) {
	for _, name := range []string{"gate contention", "lease contention", "claim error", "claim conflict", "claim panic", "provider panic", "publish failure", "success"} {
		t.Run(name, func(t *testing.T) {
			store := &upcomingTestStore{}
			p := &upcomingTestProvider{}
			gate := NewTMDBRunGate()
			lease := &upcomingTestLease{}
			locker := upcomingTestLocker{lease: lease}
			if name == "gate contention" {
				gate.tryAcquire()
			}
			if name == "lease contention" {
				locker.err = syncschedule.ErrInProgress
			}
			if name == "provider panic" {
				p.discover = func(context.Context) error { panic("private") }
			}
			if name == "publish failure" {
				store.publishErr = errors.New("private")
			}
			m, err := NewUpcomingManager(context.Background(), NewUpcomingService(store, p, nil, gate), locker)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			claims := 0
			done, err := m.StartScheduled(func(context.Context) (bool, error) {
				claims++
				if name == "claim error" {
					return false, errors.New("private")
				}
				if name == "claim panic" {
					panic("private")
				}
				return name != "claim conflict", nil
			})
			switch name {
			case "gate contention", "lease contention":
				if !errors.Is(err, syncschedule.ErrInProgress) || claims != 0 || lease.released.Load() != 0 {
					t.Fatalf("err=%v claims=%d releases=%d", err, claims, lease.released.Load())
				}
				if name == "gate contention" {
					gate.release()
				}
			case "claim error", "claim conflict", "claim panic":
				if err == nil || lease.released.Load() != 1 {
					t.Fatalf("err=%v releases=%d", err, lease.released.Load())
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
				select {
				case result := <-done:
					if result.Succeeded != (name == "success") {
						t.Fatalf("completion=%+v", result)
					}
				case <-time.After(time.Second):
					t.Fatal("completion blocked")
				}
				if lease.released.Load() != 1 {
					t.Fatal("lease leaked")
				}
			}
			if !gate.tryAcquire() {
				t.Fatal("gate leaked")
			}
			gate.release()
		})
	}
}

func TestUpcomingManagerShutdownCancelsAcceptedJob(t *testing.T) {
	started := make(chan struct{})
	p := &upcomingTestProvider{discover: func(ctx context.Context) error { close(started); <-ctx.Done(); return ctx.Err() }}
	store := &upcomingTestStore{}
	lease := &upcomingTestLease{}
	gate := NewTMDBRunGate()
	m, err := NewUpcomingManager(context.Background(), NewUpcomingService(store, p, nil, gate), upcomingTestLocker{lease: lease})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	done, err := m.StartScheduled(func(context.Context) (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if status := m.Snapshot(); status == nil || status.State != UpcomingRunning {
		t.Fatalf("scheduled status=%+v", status)
	}
	if _, err := m.Start(); !errors.Is(err, syncschedule.ErrInProgress) {
		t.Fatalf("manual overlap with scheduled job=%v", err)
	}
	if _, err := m.StartScheduled(func(context.Context) (bool, error) { t.Error("overlapping job claimed"); return true, nil }); !errors.Is(err, syncschedule.ErrInProgress) {
		t.Fatalf("overlap=%v", err)
	}
	m.Close()
	if (<-done).Succeeded || store.publishCalls != 0 || lease.released.Load() != 1 {
		t.Fatal("shutdown published or leaked lease")
	}
	if _, err := m.StartScheduled(func(context.Context) (bool, error) { return true, nil }); !errors.Is(err, syncschedule.ErrTargetUnavailable) {
		t.Fatalf("closed start=%v", err)
	}
	if !gate.tryAcquire() {
		t.Fatal("shutdown leaked gate")
	}
	gate.release()
}

func TestUpcomingManagerManualStatusAndCleanup(t *testing.T) {
	for _, name := range []string{"success", "provider failure", "provider panic", "publish failure", "release failure", "shutdown"} {
		t.Run(name, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			provider := &upcomingTestProvider{discover: func(ctx context.Context) error {
				close(started)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-release:
				}
				switch name {
				case "provider failure":
					return errors.New("private provider failure")
				case "provider panic":
					panic("private provider failure")
				}
				return nil
			}}
			store := &upcomingTestStore{}
			lease := &upcomingTestLease{}
			if name == "publish failure" {
				store.publishErr = errors.New("private database failure")
			}
			if name == "release failure" {
				lease.err = errors.New("private lease failure")
			}
			gate := NewTMDBRunGate()
			now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
			m, err := NewUpcomingManager(t.Context(), NewUpcomingService(store, provider, func() time.Time { return now }, gate), upcomingTestLocker{lease: lease})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			if m.Snapshot() != nil {
				t.Fatal("new manager has status")
			}
			status, err := m.Start()
			if err != nil || status.State != UpcomingRunning || !status.StartedAt.Equal(now) || status.FinishedAt != nil || status.ErrorCode != nil {
				t.Fatalf("start=%+v err=%v", status, err)
			}
			<-started
			if _, err := m.Start(); !errors.Is(err, syncschedule.ErrInProgress) {
				t.Fatalf("manual overlap=%v", err)
			}
			if _, err := m.StartScheduled(func(context.Context) (bool, error) {
				t.Error("manual overlap consumed scheduled claim")
				return true, nil
			}); !errors.Is(err, syncschedule.ErrInProgress) {
				t.Fatalf("scheduled overlap=%v", err)
			}
			copy := m.Snapshot()
			copy.State = UpcomingFailed
			if m.Snapshot().State != UpcomingRunning {
				t.Fatal("snapshot aliases manager state")
			}
			if name == "shutdown" {
				m.Close()
			} else {
				close(release)
			}
			want := UpcomingFailed
			if name == "success" {
				want = UpcomingSucceeded
			}
			finished := waitForUpcomingState(t, m, want)
			if finished.FinishedAt == nil || !finished.FinishedAt.Equal(now) || (finished.ErrorCode == nil) != (want == UpcomingSucceeded) {
				t.Fatalf("finished=%+v", finished)
			}
			if finished.ErrorCode != nil {
				if *finished.ErrorCode != UpcomingFailure {
					t.Fatalf("unsafe code=%s", *finished.ErrorCode)
				}
				*finished.ErrorCode = "modified"
				if *m.Snapshot().ErrorCode != UpcomingFailure {
					t.Fatal("error code aliases manager state")
				}
			}
			*finished.FinishedAt = time.Time{}
			if !m.Snapshot().FinishedAt.Equal(now) || status.State != UpcomingRunning {
				t.Fatal("returned status aliases manager state")
			}
			if lease.released.Load() != 1 || !gate.tryAcquire() {
				t.Fatal("lease or gate leaked")
			}
			gate.release()
			m.Close()
			if _, err := m.Start(); !errors.Is(err, syncschedule.ErrTargetUnavailable) {
				t.Fatalf("closed manual start=%v", err)
			}
		})
	}
}

func waitForUpcomingState(t *testing.T, m *UpcomingManager, want UpcomingState) *UpcomingStatus {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		status := m.Snapshot()
		if status != nil && status.State == want {
			return status
		}
		if time.Now().After(deadline) {
			t.Fatalf("status=%+v want=%s", status, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestUpcomingManagerManualRejectedAdmissionPreservesStatus(t *testing.T) {
	gate := NewTMDBRunGate()
	lease := &upcomingTestLease{}
	locker := &upcomingTestLocker{lease: lease}
	m, err := NewUpcomingManager(t.Context(), NewUpcomingService(&upcomingTestStore{}, &upcomingTestProvider{}, nil, gate), locker)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	// Scheduled runs populate the same bounded status and still deliver completion.
	done, err := m.StartScheduled(func(context.Context) (bool, error) { return true, nil })
	if err != nil || !(<-done).Succeeded {
		t.Fatalf("scheduled start=%v", err)
	}
	previous := m.Snapshot()
	if _, err := m.StartScheduled(func(context.Context) (bool, error) { return false, nil }); !errors.Is(err, syncschedule.ErrOccurrenceClaimed) {
		t.Fatalf("duplicate occurrence=%v", err)
	}
	if current := m.Snapshot(); current.State != previous.State || !current.StartedAt.Equal(previous.StartedAt) {
		t.Fatal("rejected occurrence replaced status")
	}
	for _, startErr := range []error{syncschedule.ErrInProgress, errors.New("private acquisition failure")} {
		locker.err = startErr
		if _, err := m.Start(); !errors.Is(err, startErr) {
			t.Fatalf("manual start=%v", err)
		}
		current := m.Snapshot()
		if current.State != UpcomingSucceeded || !current.StartedAt.Equal(previous.StartedAt) || !current.FinishedAt.Equal(*previous.FinishedAt) || lease.released.Load() != 2 {
			t.Fatal("rejected start replaced status or released unowned lease")
		}
		if !gate.tryAcquire() {
			t.Fatal("rejected manual start leaked gate")
		}
		gate.release()
	}
	locker.err = nil
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	waitForUpcomingState(t, m, UpcomingSucceeded)
	if lease.released.Load() != 3 {
		t.Fatal("manual restart did not complete")
	}
	var missing *UpcomingManager
	if _, err := missing.Start(); !errors.Is(err, syncschedule.ErrTargetUnavailable) || missing.Snapshot() != nil {
		t.Fatalf("nil manager=%v", err)
	}
}

func TestUpcomingManagerManualSharedGateContention(t *testing.T) {
	gate := NewTMDBRunGate()
	lease := &upcomingTestLease{}
	m, err := NewUpcomingManager(t.Context(), NewUpcomingService(&upcomingTestStore{}, &upcomingTestProvider{}, nil, gate), upcomingTestLocker{lease: lease})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	gate.tryAcquire() // Owned by another TMDB operation, not this manager.
	if _, err := m.Start(); !errors.Is(err, syncschedule.ErrInProgress) {
		t.Fatalf("shared gate contention=%v", err)
	}
	if m.Snapshot() != nil || lease.released.Load() != 0 {
		t.Fatal("rejected manual run published status or acquired lease")
	}
	gate.release()
	if _, err := m.Start(); err != nil {
		t.Fatal(err)
	}
	waitForUpcomingState(t, m, UpcomingSucceeded)
}
