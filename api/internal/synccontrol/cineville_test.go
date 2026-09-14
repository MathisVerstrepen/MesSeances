package synccontrol

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/cineville"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

func TestCinevilleExecutorManualMetricsAndFailures(t *testing.T) {
	window := Window{From: "2026-08-17"}
	writes := 0
	enrichments := 0
	e := &ProductionExecutor{now: time.Now, logger: slog.New(slog.DiscardHandler), operationTimeout: time.Second,
		writer: writerFunc(func(_ context.Context, d []schedule.Dataset) (int64, error) {
			writes++
			if len(d) != 1 || d[0].Provider != schedule.ProviderCineville {
				t.Fatal("wrong publication")
			}
			return 1, nil
		}),
		enrich: func(_ context.Context, m []enrichment.Movie) (*enrichment.Summary, error) {
			enrichments++
			if len(m) != 1 || m[0].ProviderID != "-693091020261" || m[0].RuntimeMinutes != 0 || m[0].SourceProvider != "cineville" {
				t.Fatal("source enrichment identity")
			}
			return nil, nil
		},
	}
	configureCinevilleTestExecutor(t, e, window)
	out, err := e.Run(t.Context(), TargetCineville, window)
	if err != nil || out[TargetCineville].Sync.Requests != 27 || writes != 1 || enrichments != 1 {
		t.Fatalf("out=%v err=%v", out, err)
	}
	for _, v := range []struct {
		kind  syncproxy.FailureKind
		stage FailureStage
	}{{syncproxy.FailureStatus, StageProviderFetch}, {syncproxy.FailureInvalidJSON, StageProviderFetch}} {
		e.syncCineville = func(context.Context, cineville.Fetcher, cineville.SyncOptions) (schedule.Dataset, cineville.SyncSummary, error) {
			return schedule.Dataset{}, cineville.SyncSummary{}, &cineville.RequestError{Operation: cineville.OperationCinema, Kind: v.kind, StatusCode: 503}
		}
		_, err := e.Run(t.Context(), TargetCineville, window)
		var re *RunError
		if !errors.As(err, &re) || re.Stage != v.stage || re.Provider != TargetCineville || writes != 1 || enrichments != 1 {
			t.Fatalf("failure=%v", err)
		}
		if !strings.Contains(strings.Join(re.logs[TargetCineville], "\n"), "provider=cineville") {
			t.Fatal("Cineville failure log dropped")
		}
	}
	configureCinevilleTestExecutor(t, e, window)
	e.writer = writerFunc(func(context.Context, []schedule.Dataset) (int64, error) {
		return 0, errors.New("synthetic-publication-secret")
	})
	if _, err := e.Run(t.Context(), TargetCineville, window); err == nil || strings.Contains(err.Error(), "secret") || enrichments != 1 {
		t.Fatal("publication failure/enrichment")
	}
}
func TestCinevilleManagerStatusCloning(t *testing.T) {
	manager, err := newTestManager(t.Context(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) { return ProviderOutcome{}, nil }))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if _, err := manager.Start(TargetCineville); err != nil {
		t.Fatal(err)
	}
	status := waitForTerminal(t, manager)
	if status.State != StateSucceeded || status.Providers["cineville"].State != ProviderSucceeded || status.Providers["ugc"].State != ProviderNotRequested || len(status.Providers) != 9 {
		t.Fatal("Cineville status")
	}
	status.Providers["cineville"] = ProviderStatus{State: "mutated"}
	if manager.Status().Providers["cineville"].State != "succeeded" {
		t.Fatal("status mutation")
	}
}
