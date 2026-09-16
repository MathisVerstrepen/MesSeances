package synccontrol

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"messeances/api/internal/cgr"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/kinepolis"
	"messeances/api/internal/pathe"
	"messeances/api/internal/schedule"
	"messeances/api/internal/ugc"
)

func TestManagerAllPublishesSuccessfulProvidersIndependently(t *testing.T) {
	for _, mode := range []string{"success", "client", "fetch", "validation", "publication"} {
		t.Run(mode, func(t *testing.T) {
			window := Window{From: "2026-08-17"}
			versions := make(map[Target]int64)
			enriched := make(map[Target]bool)
			for _, provider := range allTestProviders {
				versions[provider] = 1 // Previously published data.
			}
			version := int64(1)
			failed := Target("")
			code := FailureNone
			switch mode {
			case "client":
				failed, code = TargetUGC, FailureClientCreation
			case "fetch":
				failed, code = TargetUGC, FailureProviderSync
			case "validation":
				failed, code = TargetPathe, FailureDatasetRejected
			case "publication":
				failed, code = TargetPathe, FailureReplacement
			}
			executor := &ProductionExecutor{
				now:    func() time.Time { return time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC) },
				logger: slog.New(slog.DiscardHandler), operationTimeout: time.Second,
				writer: writerFunc(func(_ context.Context, datasets []schedule.Dataset) (int64, error) {
					if len(datasets) != 1 {
						t.Errorf("publication must isolate providers: count=%d", len(datasets))
						return 0, errors.New("unexpected publication batch")
					}
					provider := Target(datasets[0].Provider)
					if provider == failed && mode == "publication" {
						return 0, errors.New("synthetic transaction failure")
					}
					version++
					versions[provider] = version
					return version, nil
				}),
				newUGC: func() (ugc.Getter, error) {
					if mode == "client" {
						return nil, errors.New("synthetic client failure")
					}
					return unusedGetter{}, nil
				},
				newKinepolis: func() (kinepolis.Fetcher, error) { return unusedFetcher{}, nil },
				newPathe:     func() (pathe.Getter, error) { return unusedPatheGetter{}, nil },
				newCGR:       func() (cgr.Getter, error) { return unusedCGRGetter{}, nil },
				syncUGC: func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
					if mode == "fetch" {
						return schedule.Dataset{}, ugc.SyncSummary{}, errors.New("synthetic fetch failure")
					}
					return validDataset(t, schedule.ProviderUGC, window), ugc.SyncSummary{}, nil
				},
				syncKinepolis: func(context.Context, kinepolis.Fetcher, kinepolis.SyncOptions) (schedule.Dataset, kinepolis.SyncSummary, error) {
					return validDataset(t, schedule.ProviderKinepolis, window), kinepolis.SyncSummary{}, nil
				},
				syncPathe: func(context.Context, pathe.Getter, pathe.SyncOptions) (schedule.Dataset, pathe.SyncSummary, error) {
					if mode == "validation" {
						return schedule.Dataset{}, pathe.SyncSummary{}, nil
					}
					return validDataset(t, schedule.ProviderPathe, window), pathe.SyncSummary{}, nil
				},
				syncCGR: func(context.Context, cgr.Getter, cgr.SyncOptions) (schedule.Dataset, cgr.SyncSummary, error) {
					return validDataset(t, schedule.ProviderCGR, window), cgr.SyncSummary{}, nil
				},
				enrich: func(_ context.Context, movies []enrichment.Movie) (*enrichment.Summary, error) {
					provider := Target(movies[0].SourceProvider)
					if versions[provider] == 1 {
						t.Errorf("enriched provider before publication: %s", provider)
					}
					enriched[provider] = true
					return &enrichment.Summary{Matched: 1}, errors.New("nonfatal enrichment failure")
				},
			}
			configureMegaramaTestExecutor(t, executor, window)
			configureCinevilleTestExecutor(t, executor, window)
			configureMK2TestExecutor(t, executor, window)
			configureCinewestTestExecutor(t, executor, window)
			configureGrandEcranTestExecutor(t, executor, window)
			configureNoeCinemasTestExecutor(t, executor, window)
			manager, err := newTestManager(t.Context(), executor.now, executor)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(manager.Close)
			if _, err := manager.Start(TargetAll); err != nil {
				t.Fatal(err)
			}
			status := waitForTerminal(t, manager)
			manager.Close()
			wantState := StateSucceeded
			if failed != "" {
				wantState = StateFailed
			}
			if status.State != wantState {
				t.Fatalf("state=%s want=%s", status.State, wantState)
			}
			for _, provider := range allTestProviders {
				got := status.Providers[string(provider)]
				if provider == failed {
					if got.State != ProviderFailed || got.ErrorCode != code || got.Outcome != nil || versions[provider] != 1 || enriched[provider] {
						t.Errorf("failed provider=%s status=%+v version=%d enriched=%t", provider, got, versions[provider], enriched[provider])
					}
				} else if got.State != ProviderSucceeded || got.Outcome == nil || got.Outcome.Sync.Version != versions[provider] || versions[provider] == 1 || !enriched[provider] || got.Outcome.Enrichment.Status != EnrichmentDegraded || got.Outcome.Enrichment.Counts.Matched != 1 {
					t.Errorf("successful provider=%s status=%+v version=%d enriched=%t", provider, got, versions[provider], enriched[provider])
				}
			}
		})
	}
}
