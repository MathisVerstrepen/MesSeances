package synccontrol

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/cgr"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/kinepolis"
	"messeances/api/internal/pathe"
	"messeances/api/internal/schedule"
	"messeances/api/internal/ugc"
)

type observedSync struct {
	provider, result, stage, code, enrichment string
}

type captureSyncObserver struct{ observations []observedSync }

func (o *captureSyncObserver) ObserveSync(provider, result, stage, code, enrichment string, _ time.Duration, _ map[string]int) {
	o.observations = append(o.observations, observedSync{provider, result, stage, code, enrichment})
}

type writerFunc func(context.Context, []schedule.Dataset) (int64, error)

func (f writerFunc) Replace(ctx context.Context, data []schedule.Dataset) (schedule.PublicationResult, error) {
	version, err := f(ctx, data)
	result := schedule.PublicationResult{Version: version, Providers: make(map[schedule.Provider]schedule.PublicationMetrics, len(data))}
	for _, dataset := range data {
		result.Providers[dataset.Provider] = schedule.PublicationMetrics{Movies: 1, NewMovies: 1, Showtimes: len(dataset.Showtimes), NewShowtimes: len(dataset.Showtimes)}
	}
	return result, err
}

type unusedGetter struct{}

func (unusedGetter) Get(context.Context, ugc.Operation, string) (ugc.FetchResult, error) {
	return ugc.FetchResult{}, nil
}
func (unusedGetter) RequestCount() int { return 0 }

type countedUGCGetter struct{ requests int }

func (countedUGCGetter) Get(context.Context, ugc.Operation, string) (ugc.FetchResult, error) {
	return ugc.FetchResult{}, nil
}
func (getter countedUGCGetter) RequestCount() int { return getter.requests }

type unusedFetcher struct{}

func (unusedFetcher) Fetch(context.Context) ([]byte, error)               { return nil, nil }
func (unusedFetcher) FetchCinema(context.Context, string) ([]byte, error) { return nil, nil }

type countedFetcher struct{ requests int }

func (countedFetcher) Fetch(context.Context) ([]byte, error)               { return nil, nil }
func (countedFetcher) FetchCinema(context.Context, string) ([]byte, error) { return nil, nil }
func (f countedFetcher) RequestCount() int                                 { return f.requests }

type unusedPatheGetter struct{}

func (unusedPatheGetter) Get(context.Context, pathe.Operation, string) ([]byte, error) {
	return nil, nil
}
func (unusedPatheGetter) RequestCount() int { return 0 }

type unusedCGRGetter struct{}

func (unusedCGRGetter) Get(context.Context, cgr.Operation, string) ([]byte, error) { return nil, nil }
func (unusedCGRGetter) RequestCount() int                                          { return 0 }

type countingCGRGetter struct{ requests int }

func (countingCGRGetter) Get(context.Context, cgr.Operation, string) ([]byte, error) { return nil, nil }
func (g countingCGRGetter) RequestCount() int                                        { return g.requests }

func TestProductionExecutorCommitsBeforeNonFatalEnrichment(t *testing.T) {
	window := Window{From: "2026-08-17"}
	events := []string{}
	executor := &ProductionExecutor{
		now: time.Now, logger: slog.New(slog.DiscardHandler),
		writer: writerFunc(func(_ context.Context, data []schedule.Dataset) (int64, error) {
			events = append(events, "commit:"+string(data[0].Provider))
			return 1, nil
		}),
		newUGC:       func() (ugc.Getter, error) { return unusedGetter{}, nil },
		newKinepolis: func() (kinepolis.Fetcher, error) { return unusedFetcher{}, nil },
		newPathe:     func() (pathe.Getter, error) { return unusedPatheGetter{}, nil },
		newCGR:       func() (cgr.Getter, error) { return unusedCGRGetter{}, nil },
		syncUGC: func(_ context.Context, _ ugc.Getter, options ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
			if options.From != window.From {
				t.Fatalf("UGC options=%+v", options)
			}
			return validDataset(t, schedule.ProviderUGC, window), ugc.SyncSummary{}, nil
		},
		syncKinepolis: func(_ context.Context, _ kinepolis.Fetcher, options kinepolis.SyncOptions) (schedule.Dataset, kinepolis.SyncSummary, error) {
			return validDataset(t, schedule.ProviderKinepolis, window), kinepolis.SyncSummary{}, nil
		},
		syncPathe: func(_ context.Context, _ pathe.Getter, options pathe.SyncOptions) (schedule.Dataset, pathe.SyncSummary, error) {
			return validDataset(t, schedule.ProviderPathe, window), pathe.SyncSummary{}, nil
		},
		syncCGR: func(_ context.Context, _ cgr.Getter, options cgr.SyncOptions) (schedule.Dataset, cgr.SyncSummary, error) {
			return validDataset(t, schedule.ProviderCGR, window), cgr.SyncSummary{}, nil
		},
		enrich: func(_ context.Context, movies []enrichment.Movie) (*enrichment.Summary, error) {
			events = append(events, "enrich:"+movies[0].SourceProvider)
			return nil, errors.New("degraded")
		},
	}
	for _, provider := range []Target{TargetUGC, TargetKinepolis, TargetPathe, TargetCGR} {
		if _, err := executor.Run(context.Background(), provider, window); err != nil {
			t.Fatalf("provider=%s err=%v", provider, err)
		}
	}
	want := []string{"commit:ugc", "enrich:ugc", "commit:kinepolis", "enrich:kinepolis", "commit:pathe", "enrich:pathe", "commit:cgr", "enrich:cgr"}
	if len(events) != len(want) {
		t.Fatalf("events=%v", events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events=%v", events)
		}
	}
}

func TestProductionExecutorPublishesTargetAllOnce(t *testing.T) {
	window := Window{From: "2026-08-17"}
	writes, enrichments := 0, 0
	executor := &ProductionExecutor{
		now: time.Now, logger: slog.New(slog.DiscardHandler),
		writer: writerFunc(func(_ context.Context, datasets []schedule.Dataset) (int64, error) {
			writes++
			if len(datasets) != 4 || datasets[0].Provider != schedule.ProviderUGC || datasets[1].Provider != schedule.ProviderKinepolis || datasets[2].Provider != schedule.ProviderPathe || datasets[3].Provider != schedule.ProviderCGR || datasets[0].Window.Through != "2027-01-10" || datasets[1].Window.Through != "2026-11-20" || datasets[2].Window.Through != "2026-12-15" || datasets[3].Window.Through != "2026-10-30" {
				t.Fatalf("datasets=%+v", datasets)
			}
			return 11, nil
		}),
		newUGC:       func() (ugc.Getter, error) { return unusedGetter{}, nil },
		newKinepolis: func() (kinepolis.Fetcher, error) { return unusedFetcher{}, nil },
		newPathe:     func() (pathe.Getter, error) { return unusedPatheGetter{}, nil },
		newCGR:       func() (cgr.Getter, error) { return unusedCGRGetter{}, nil },
		syncUGC: func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
			data := validDataset(t, schedule.ProviderUGC, window)
			data.Window.Through = "2027-01-10"
			return data, ugc.SyncSummary{}, nil
		},
		syncKinepolis: func(context.Context, kinepolis.Fetcher, kinepolis.SyncOptions) (schedule.Dataset, kinepolis.SyncSummary, error) {
			data := validDataset(t, schedule.ProviderKinepolis, window)
			data.Window.Through = "2026-11-20"
			return data, kinepolis.SyncSummary{}, nil
		},
		syncPathe: func(context.Context, pathe.Getter, pathe.SyncOptions) (schedule.Dataset, pathe.SyncSummary, error) {
			data := validDataset(t, schedule.ProviderPathe, window)
			data.Window.Through = "2026-12-15"
			return data, pathe.SyncSummary{Requests: 17}, nil
		},
		syncCGR: func(context.Context, cgr.Getter, cgr.SyncOptions) (schedule.Dataset, cgr.SyncSummary, error) {
			data := validDataset(t, schedule.ProviderCGR, window)
			data.Window.Through = "2026-10-30"
			return data, cgr.SyncSummary{Requests: 8}, nil
		},
		enrich: func(context.Context, []enrichment.Movie) (*enrichment.Summary, error) {
			enrichments++
			if writes != 1 {
				t.Fatal("enrichment ran before publication")
			}
			return nil, nil
		},
	}
	outcomes, err := executor.Run(context.Background(), TargetAll, window)
	if err != nil || writes != 1 || enrichments != 4 || outcomes[TargetUGC].Sync.Version != 11 || outcomes[TargetKinepolis].Sync.Version != 11 || outcomes[TargetPathe].Sync.Version != 11 || outcomes[TargetCGR].Sync.Version != 11 || outcomes[TargetUGC].Sync.Through != "2027-01-10" || outcomes[TargetKinepolis].Sync.Through != "2026-11-20" || outcomes[TargetPathe].Sync.Through != "2026-12-15" || outcomes[TargetCGR].Sync.Through != "2026-10-30" || outcomes[TargetPathe].Sync.Requests != 17 || outcomes[TargetCGR].Sync.Requests != 8 {
		t.Fatalf("outcomes=%+v writes=%d enrichments=%d err=%v", outcomes, writes, enrichments, err)
	}
}

func TestProductionExecutorPopulatesKinepolisRequestCount(t *testing.T) {
	window := Window{From: "2026-08-17"}
	executor := &ProductionExecutor{
		now: time.Now, logger: slog.New(slog.DiscardHandler),
		writer:       writerFunc(func(context.Context, []schedule.Dataset) (int64, error) { return 4, nil }),
		newKinepolis: func() (kinepolis.Fetcher, error) { return countedFetcher{requests: 6}, nil },
		syncKinepolis: func(context.Context, kinepolis.Fetcher, kinepolis.SyncOptions) (schedule.Dataset, kinepolis.SyncSummary, error) {
			return validDataset(t, schedule.ProviderKinepolis, window), kinepolis.SyncSummary{Cinemas: 1, Showtimes: 1}, nil
		},
	}
	outcomes, err := executor.Run(context.Background(), TargetKinepolis, window)
	if err != nil || outcomes[TargetKinepolis].Sync.Requests != 6 {
		t.Fatalf("outcomes=%+v err=%v", outcomes, err)
	}
}

func TestProductionExecutorLogsSanitizedKinepolisFetchDiagnostics(t *testing.T) {
	const sensitive = "https://user:proxy-password@proxy.example/path?token=token-secret cookie=session-secret body=provider-body-secret cause=transport-secret"
	tests := []struct {
		name          string
		category      kinepolis.ErrorCategory
		status        int
		operation     kinepolis.Operation
		wantCategory  string
		wantOperation string
		wantStatus    bool
	}{
		{name: "schedule invalid payload", category: kinepolis.CategoryInvalidPayload, operation: kinepolis.OperationSchedule, wantCategory: "invalid_payload", wantOperation: "cinemas"},
		{name: "cinema invalid payload", category: kinepolis.CategoryInvalidPayload, operation: kinepolis.OperationCinema, wantCategory: "invalid_payload", wantOperation: "cinema"},
		{name: "transport", category: kinepolis.CategoryTransport, operation: kinepolis.OperationCinema, wantCategory: "transport", wantOperation: "cinema"},
		{name: "redirect", category: kinepolis.CategoryRedirect, operation: kinepolis.OperationCinema, wantCategory: "redirect", wantOperation: "cinema"},
		{name: "response read", category: kinepolis.CategoryResponseRead, operation: kinepolis.OperationSchedule, wantCategory: "response_read", wantOperation: "cinemas"},
		{name: "response large", category: kinepolis.CategoryResponseLarge, operation: kinepolis.OperationSchedule, wantCategory: "response_too_large", wantOperation: "cinemas"},
		{name: "challenge", category: kinepolis.CategoryChallenge, operation: kinepolis.OperationCinema, wantCategory: "challenge", wantOperation: "cinema"},
		{name: "forbidden", category: kinepolis.CategoryStatus, status: 403, operation: kinepolis.OperationCinema, wantCategory: "http_status", wantOperation: "cinema", wantStatus: true},
		{name: "server", category: kinepolis.CategoryServer, status: 503, operation: kinepolis.OperationSchedule, wantCategory: "http_status", wantOperation: "cinemas", wantStatus: true},
		{name: "content type", category: kinepolis.CategoryContentType, operation: kinepolis.OperationSchedule, wantCategory: "content_type", wantOperation: "cinemas"},
		{name: "empty", category: kinepolis.CategoryEmptyResponse, operation: kinepolis.OperationCinema, wantCategory: "empty_response", wantOperation: "cinema"},
		{name: "canceled", category: kinepolis.CategoryCanceled, operation: kinepolis.OperationCinema, wantCategory: "canceled", wantOperation: "cinema"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			executor := failedKinepolisExecutor(&logs, fmt.Errorf("%s: %w", sensitive, &kinepolis.RequestError{Operation: test.operation, Category: test.category, StatusCode: test.status}))
			_, err := executor.Run(context.Background(), TargetKinepolis, Window{From: "2026-08-17"})
			var runErr *RunError
			if !errors.As(err, &runErr) || runErr.Stage != StageProviderFetch || runErr.Code != FailureProviderSync {
				t.Fatalf("err=%v", err)
			}
			combined := strings.Join(runErr.logs[TargetKinepolis], "\n") + logs.String()
			for _, want := range []string{"operation=" + test.wantOperation, "category=" + test.wantCategory, `"request_operation":"` + test.wantOperation + `"`, `"fetch_category":"` + test.wantCategory + `"`, "requests=1"} {
				if !strings.Contains(combined, want) {
					t.Fatalf("output missing %q: %s", want, combined)
				}
			}
			if test.wantStatus != strings.Contains(combined, "http_status=") || test.wantStatus != strings.Contains(combined, `"http_status":`) {
				t.Fatalf("status mismatch: %s", combined)
			}
			for _, forbidden := range []string{sensitive, "proxy-password", "proxy.example", "token-secret", "session-secret", "provider-body-secret", "transport-secret"} {
				if strings.Contains(combined, forbidden) {
					t.Fatalf("output leaked %q: %s", forbidden, combined)
				}
			}
		})
	}
}

func TestProductionExecutorKinepolisTypedErrorPrecedesDatasetSentinel(t *testing.T) {
	requestErr := &kinepolis.RequestError{Operation: kinepolis.OperationSchedule, Category: kinepolis.CategoryInvalidPayload}
	typed := fmt.Errorf("typed: %w; validation: %w", requestErr, schedule.ErrDatasetValidation)
	executor := failedKinepolisExecutor(&bytes.Buffer{}, typed)
	_, err := executor.Run(context.Background(), TargetKinepolis, Window{From: "2026-08-17"})
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.Stage != StageProviderFetch || runErr.Code != FailureProviderSync {
		t.Fatalf("typed precedence err=%v", err)
	}

	executor = failedKinepolisExecutor(&bytes.Buffer{}, fmt.Errorf("%w: final", schedule.ErrDatasetValidation))
	_, err = executor.Run(context.Background(), TargetKinepolis, Window{From: "2026-08-17"})
	if !errors.As(err, &runErr) || runErr.Stage != StageDatasetValidation || runErr.Code != FailureDatasetRejected {
		t.Fatalf("bare validation err=%v", err)
	}
}

func TestProductionExecutorRetainsTypedKinepolisOperationOnCancellation(t *testing.T) {
	requestErr := &kinepolis.RequestError{Operation: kinepolis.OperationCinema, Category: kinepolis.CategoryCanceled}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var logs bytes.Buffer
	executor := failedKinepolisExecutor(&logs, fmt.Errorf("request: %w; context: %w", requestErr, context.Canceled))
	_, err := executor.Run(ctx, TargetKinepolis, Window{From: "2026-08-17"})
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.Stage != StageProviderFetch || runErr.Code != FailureCanceled {
		t.Fatalf("err=%v", err)
	}
	combined := strings.Join(runErr.logs[TargetKinepolis], "\n") + logs.String()
	if !strings.Contains(combined, "operation=cinema category=canceled") || !strings.Contains(combined, `"request_operation":"cinema"`) || !strings.Contains(combined, `"fetch_category":"canceled"`) {
		t.Fatalf("output=%s", combined)
	}
}

func TestProductionExecutorBoundsKinepolisFetchDiagnostics(t *testing.T) {
	const malicious = "proxy-password token-secret provider-body-secret raw-url?secret"
	var logs bytes.Buffer
	executor := failedKinepolisExecutor(&logs, &kinepolis.RequestError{Operation: kinepolis.Operation(malicious), Category: kinepolis.ErrorCategory(malicious), StatusCode: 999})
	_, err := executor.Run(context.Background(), TargetKinepolis, Window{From: "2026-08-17"})
	var runErr *RunError
	if !errors.As(err, &runErr) {
		t.Fatal(err)
	}
	combined := strings.Join(runErr.logs[TargetKinepolis], "\n") + logs.String()
	if !strings.Contains(combined, "operation=unknown category=unknown") || !strings.Contains(combined, `"request_operation":"unknown"`) || strings.Contains(combined, "http_status") || strings.Contains(combined, malicious) {
		t.Fatalf("unbounded output: %s", combined)
	}
}

func failedKinepolisExecutor(logs *bytes.Buffer, fetchErr error) *ProductionExecutor {
	return &ProductionExecutor{
		now:          time.Now,
		logger:       slog.New(slog.NewJSONHandler(logs, nil)),
		newKinepolis: func() (kinepolis.Fetcher, error) { return countedFetcher{requests: 1}, nil },
		syncKinepolis: func(context.Context, kinepolis.Fetcher, kinepolis.SyncOptions) (schedule.Dataset, kinepolis.SyncSummary, error) {
			return schedule.Dataset{}, kinepolis.SyncSummary{}, fetchErr
		},
	}
}

func TestProductionExecutorTargetAllSecondPreparationAndPublicationFailuresAreAtomic(t *testing.T) {
	window := Window{From: "2026-08-17"}
	writes, enrichments := 0, 0
	executor := &ProductionExecutor{
		now: time.Now, logger: slog.New(slog.DiscardHandler),
		writer: writerFunc(func(context.Context, []schedule.Dataset) (int64, error) { writes++; return 0, nil }),
		newUGC: func() (ugc.Getter, error) { return unusedGetter{}, nil }, newKinepolis: func() (kinepolis.Fetcher, error) { return unusedFetcher{}, nil },
		newPathe: func() (pathe.Getter, error) { return unusedPatheGetter{}, nil },
		newCGR:   func() (cgr.Getter, error) { return unusedCGRGetter{}, nil },
		syncUGC: func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
			return validDataset(t, schedule.ProviderUGC, window), ugc.SyncSummary{}, nil
		},
		syncKinepolis: func(context.Context, kinepolis.Fetcher, kinepolis.SyncOptions) (schedule.Dataset, kinepolis.SyncSummary, error) {
			return schedule.Dataset{}, kinepolis.SyncSummary{}, errors.New("second-provider-secret")
		},
		syncPathe: func(context.Context, pathe.Getter, pathe.SyncOptions) (schedule.Dataset, pathe.SyncSummary, error) {
			return validDataset(t, schedule.ProviderPathe, window), pathe.SyncSummary{}, nil
		},
		syncCGR: func(context.Context, cgr.Getter, cgr.SyncOptions) (schedule.Dataset, cgr.SyncSummary, error) {
			return validDataset(t, schedule.ProviderCGR, window), cgr.SyncSummary{}, nil
		},
		enrich: func(context.Context, []enrichment.Movie) (*enrichment.Summary, error) { enrichments++; return nil, nil },
	}
	_, err := executor.Run(context.Background(), TargetAll, window)
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.Provider != TargetKinepolis || runErr.Stage != StageProviderFetch || writes != 0 || enrichments != 0 {
		t.Fatalf("err=%v writes=%d enrichments=%d", err, writes, enrichments)
	}
	executor.syncKinepolis = func(context.Context, kinepolis.Fetcher, kinepolis.SyncOptions) (schedule.Dataset, kinepolis.SyncSummary, error) {
		return validDataset(t, schedule.ProviderKinepolis, window), kinepolis.SyncSummary{}, nil
	}
	executor.syncPathe = func(context.Context, pathe.Getter, pathe.SyncOptions) (schedule.Dataset, pathe.SyncSummary, error) {
		return schedule.Dataset{}, pathe.SyncSummary{}, errors.New("third-provider-secret")
	}
	_, err = executor.Run(context.Background(), TargetAll, window)
	if !errors.As(err, &runErr) || runErr.Provider != TargetPathe || runErr.Stage != StageProviderFetch || writes != 0 || enrichments != 0 {
		t.Fatalf("err=%v writes=%d enrichments=%d", err, writes, enrichments)
	}
	executor.syncPathe = func(context.Context, pathe.Getter, pathe.SyncOptions) (schedule.Dataset, pathe.SyncSummary, error) {
		return validDataset(t, schedule.ProviderPathe, window), pathe.SyncSummary{}, nil
	}
	executor.writer = writerFunc(func(context.Context, []schedule.Dataset) (int64, error) {
		writes++
		return 0, errors.New("publication-secret")
	})
	_, err = executor.Run(context.Background(), TargetAll, window)
	if !errors.As(err, &runErr) || runErr.Stage != StagePublication || runErr.Code != FailureReplacement || writes != 1 || enrichments != 0 {
		t.Fatalf("err=%v writes=%d enrichments=%d", err, writes, enrichments)
	}
}

func TestProductionExecutorTelemetryIsBoundedAndRedacted(t *testing.T) {
	const secret = "synthetic-telemetry-secret"
	window := Window{From: "2026-08-17"}
	tests := []struct {
		name      string
		configure func(*ProductionExecutor)
		want      observedSync
	}{
		{name: "client", configure: func(e *ProductionExecutor) { e.newUGC = func() (ugc.Getter, error) { return nil, errors.New(secret) } }, want: observedSync{"ugc", "failed", "client_creation", "client_creation_failed", ""}},
		{name: "fetch", configure: func(e *ProductionExecutor) {
			e.syncUGC = func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
				return schedule.Dataset{}, ugc.SyncSummary{}, errors.New(secret)
			}
		}, want: observedSync{"ugc", "failed", "provider_fetch", "provider_sync_failed", ""}},
		{name: "validation", configure: func(e *ProductionExecutor) {
			e.syncUGC = func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
				return schedule.Dataset{}, ugc.SyncSummary{}, fmt.Errorf("%w: %s", schedule.ErrDatasetValidation, secret)
			}
		}, want: observedSync{"ugc", "failed", "dataset_validation", "dataset_rejected", ""}},
		{name: "publication", configure: func(e *ProductionExecutor) {
			e.writer = writerFunc(func(context.Context, []schedule.Dataset) (int64, error) { return 0, errors.New(secret) })
		}, want: observedSync{"ugc", "failed", "publication", "replacement_failed", ""}},
		{name: "enrichment", configure: func(e *ProductionExecutor) {
			e.enrich = func(context.Context, []enrichment.Movie) (*enrichment.Summary, error) {
				return &enrichment.Summary{Failed: 1}, errors.New(secret)
			}
		}, want: observedSync{"ugc", "succeeded", "none", "none", "degraded"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			observer := &captureSyncObserver{}
			executor := &ProductionExecutor{
				now: time.Now, logger: slog.New(slog.NewJSONHandler(&logs, nil)), observer: observer,
				writer: writerFunc(func(context.Context, []schedule.Dataset) (int64, error) { return 7, nil }),
				newUGC: func() (ugc.Getter, error) { return unusedGetter{}, nil },
				syncUGC: func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
					return validDataset(t, schedule.ProviderUGC, window), ugc.SyncSummary{}, nil
				},
			}
			test.configure(executor)
			_, _ = executor.Run(context.Background(), TargetUGC, window)
			if strings.Contains(logs.String(), secret) || len(observer.observations) != 1 || observer.observations[0] != test.want {
				t.Fatalf("logs=%q observations=%+v want=%+v", logs.String(), observer.observations, test.want)
			}
		})
	}
}

func TestProductionExecutorLogsSanitizedPatheFetchDiagnostics(t *testing.T) {
	const sensitive = "https://user:proxy-password@proxy.example/path?token=token-secret cookie=session-secret body=provider-body-secret transport=underlying-sensitive-error"
	tests := []struct {
		name          string
		category      pathe.ErrorCategory
		status        int
		operation     pathe.Operation
		wantCategory  string
		wantOperation string
		wantStatus    string
	}{
		{name: "transport", category: pathe.CategoryTransport, operation: pathe.OperationCinemas, wantCategory: "transport", wantOperation: "cinemas"},
		{name: "forbidden", category: pathe.CategoryStatus, status: 403, operation: pathe.OperationShows, wantCategory: "http_status", wantOperation: "program", wantStatus: "403"},
		{name: "rate limited", category: pathe.CategoryStatus, status: 429, operation: pathe.OperationCinemaProgram, wantCategory: "http_status", wantOperation: "program", wantStatus: "429"},
		{name: "server status", category: pathe.CategoryServer, status: 503, operation: pathe.OperationMovieTimes, wantCategory: "http_status", wantOperation: "showings", wantStatus: "503"},
		{name: "challenge", category: pathe.CategoryChallenge, operation: pathe.OperationEventTimes, wantCategory: "challenge", wantOperation: "showings"},
		{name: "content type", category: pathe.CategoryContentType, operation: pathe.OperationCinemas, wantCategory: "content_type", wantOperation: "cinemas"},
		{name: "redirect", category: pathe.CategoryRedirect, operation: pathe.OperationShows, wantCategory: "redirect", wantOperation: "program"},
		{name: "response too large", category: pathe.CategoryResponseLarge, operation: pathe.OperationCinemaProgram, wantCategory: "response_too_large", wantOperation: "program"},
		{name: "invalid JSON", category: pathe.CategoryInvalidJSON, operation: pathe.OperationMovieTimes, wantCategory: "invalid_payload", wantOperation: "showings"},
		{name: "canceled", category: pathe.CategoryCanceled, operation: pathe.OperationEventTimes, wantCategory: "canceled", wantOperation: "showings"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			executor := failedPatheExecutor(&logs, fmt.Errorf("%s: %w", sensitive, &pathe.RequestError{Operation: test.operation, Category: test.category, StatusCode: test.status}))
			_, err := executor.Run(context.Background(), TargetPathe, Window{From: "2026-08-17"})
			var runErr *RunError
			if !errors.As(err, &runErr) || runErr.Code != FailureProviderSync {
				t.Fatalf("err=%v", err)
			}
			logLine := logs.String()
			for _, want := range []string{`"provider":"pathe"`, `"result":"failed"`, `"stage":"provider_fetch"`, `"error_code":"provider_sync_failed"`, `"fetch_category":"` + test.wantCategory + `"`, `"request_operation":"` + test.wantOperation + `"`} {
				if !strings.Contains(logLine, want) {
					t.Fatalf("log missing %q: %s", want, logLine)
				}
			}
			if test.wantStatus != "" && !strings.Contains(logLine, `"http_status":`+test.wantStatus) {
				t.Fatalf("log missing status %s: %s", test.wantStatus, logLine)
			}
			if test.wantStatus == "" && strings.Contains(logLine, `"http_status"`) {
				t.Fatalf("log contains unexpected status: %s", logLine)
			}
			for _, forbidden := range []string{sensitive, "user:proxy-password", "proxy.example", "token-secret", "session-secret", "provider-body-secret", "underlying-sensitive-error"} {
				if strings.Contains(logLine, forbidden) {
					t.Fatalf("log leaked %q: %s", forbidden, logLine)
				}
			}
		})
	}
}

func TestProductionExecutorBoundsPatheFetchDiagnostics(t *testing.T) {
	const malicious = "https://user:password@proxy.example/?token=secret"
	var logs bytes.Buffer
	executor := failedPatheExecutor(&logs, &pathe.RequestError{Operation: pathe.Operation(malicious), Category: pathe.ErrorCategory(malicious), StatusCode: 999})
	_, _ = executor.Run(context.Background(), TargetPathe, Window{From: "2026-08-17"})
	logLine := logs.String()
	if !strings.Contains(logLine, `"fetch_category":"unknown"`) || !strings.Contains(logLine, `"request_operation":"unknown"`) || strings.Contains(logLine, `"http_status"`) || strings.Contains(logLine, malicious) {
		t.Fatalf("unbounded diagnostic log: %s", logLine)
	}
}

func TestProductionExecutorRetainsCGRFailureProgressCounters(t *testing.T) {
	const secret = "synthetic-parser-secret"
	var logs bytes.Buffer
	executor := &ProductionExecutor{
		now:    time.Now,
		logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		newCGR: func() (cgr.Getter, error) { return countingCGRGetter{requests: 82}, nil },
		syncCGR: func(context.Context, cgr.Getter, cgr.SyncOptions) (schedule.Dataset, cgr.SyncSummary, error) {
			return schedule.Dataset{}, cgr.SyncSummary{Cinemas: 73, Movies: 380, Requests: 0}, errors.New(secret)
		},
	}
	_, err := executor.Run(context.Background(), TargetCGR, Window{From: "2026-08-25"})
	if err == nil || strings.Contains(logs.String(), secret) || !strings.Contains(logs.String(), `"cinemas":73`) || !strings.Contains(logs.String(), `"movies":380`) || !strings.Contains(logs.String(), `"requests":82`) {
		t.Fatalf("logs=%q err=%v", logs.String(), err)
	}
}

func TestProductionExecutorBuildsCanonicalUGCFailureLog(t *testing.T) {
	const secret = "https://user:proxy-password@proxy.example/path?token=token-secret cookie=session-secret body=provider-body-secret cause=underlying-secret"
	timestamp := time.Date(2026, 8, 26, 7, 57, 6, 0, time.UTC)
	var slogOutput bytes.Buffer
	executor := &ProductionExecutor{
		now:    func() time.Time { return timestamp },
		logger: slog.New(slog.NewJSONHandler(&slogOutput, nil)),
		newUGC: func() (ugc.Getter, error) { return countedUGCGetter{requests: 26}, nil },
		syncUGC: func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
			return schedule.Dataset{}, ugc.SyncSummary{}, fmt.Errorf("%s: %w", secret, &ugc.RequestError{Operation: ugc.OperationShowings, Category: ugc.CategoryHTTPStatus, StatusCode: 403, Attempt: 1, AttemptLimit: 4})
		},
	}
	_, err := executor.Run(context.Background(), TargetUGC, Window{From: "2026-08-26"})
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.Code != FailureProviderSync || runErr.Stage != StageProviderFetch {
		t.Fatalf("error=%v", err)
	}
	lines := runErr.logs[TargetUGC]
	if len(lines) != 4 {
		t.Fatalf("lines=%q", lines)
	}
	wantTerminal := `ts=2026-08-26T07:57:06Z level=error provider=ugc event=provider_failed stage=provider_fetch operation=showings category=http_status http_status=403 attempt=1/4 requests=26 message="Le fournisseur a renvoyé un statut HTTP inattendu."`
	if lines[3] != wantTerminal || !strings.Contains(lines[0], "event=provider_started") || !strings.Contains(lines[1], "event=client_ready") || !strings.Contains(lines[2], "event=fetch_started") {
		t.Fatalf("lines=%q", lines)
	}
	combined := strings.Join(lines, "\n") + slogOutput.String()
	for _, forbidden := range []string{secret, "proxy-password", "proxy.example", "token-secret", "session-secret", "provider-body-secret", "underlying-secret"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("output leaked %q: %s", forbidden, combined)
		}
	}
	for _, want := range []string{`"fetch_category":"http_status"`, `"request_operation":"showings"`, `"http_status":403`, `"attempt":1`, `"attempt_limit":4`, `"requests":26`} {
		if !strings.Contains(slogOutput.String(), want) {
			t.Fatalf("slog missing %q: %s", want, slogOutput.String())
		}
	}
}

func TestProductionExecutorBoundsUGCParseReason(t *testing.T) {
	const secret = "proxy-password token-secret provider-body-secret raw-url?query=secret"
	timestamp := time.Date(2026, 8, 26, 9, 9, 51, 0, time.UTC)
	for _, test := range []struct {
		name   string
		reason ugc.ParseReason
		want   string
	}{
		{name: "known", reason: ugc.ParseReasonShowingEndMissingOrConflicting, want: "showing_end_missing_or_conflicting"},
		{name: "malicious", reason: ugc.ParseReason(secret), want: "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var slogOutput bytes.Buffer
			executor := &ProductionExecutor{
				now:    func() time.Time { return timestamp },
				logger: slog.New(slog.NewJSONHandler(&slogOutput, nil)),
				newUGC: func() (ugc.Getter, error) { return countedUGCGetter{requests: 64}, nil },
				syncUGC: func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
					return schedule.Dataset{}, ugc.SyncSummary{}, fmt.Errorf("%s: %w", secret, &ugc.RequestError{Operation: ugc.OperationShowings, Category: ugc.CategoryInvalidPayload, ParseReason: test.reason})
				},
			}
			_, err := executor.Run(context.Background(), TargetUGC, Window{From: "2026-08-26"})
			var runErr *RunError
			if !errors.As(err, &runErr) {
				t.Fatalf("error=%v", err)
			}
			lines := runErr.logs[TargetUGC]
			wantTerminal := `ts=2026-08-26T09:09:51Z level=error provider=ugc event=provider_failed stage=provider_fetch operation=showings category=invalid_payload parse_reason=` + test.want + ` requests=64 message="La réponse du fournisseur n’a pas pu être interprétée."`
			combined := strings.Join(lines, "\n") + slogOutput.String()
			if len(lines) != 4 || lines[3] != wantTerminal || strings.Contains(combined, secret) || strings.Contains(combined, "proxy-password") || strings.Contains(combined, "provider-body-secret") {
				t.Fatalf("lines=%q slog=%q", lines, slogOutput.String())
			}
		})
	}
}

func failedPatheExecutor(logs *bytes.Buffer, fetchErr error) *ProductionExecutor {
	return &ProductionExecutor{
		now:      time.Now,
		logger:   slog.New(slog.NewJSONHandler(logs, nil)),
		newPathe: func() (pathe.Getter, error) { return unusedPatheGetter{}, nil },
		syncPathe: func(context.Context, pathe.Getter, pathe.SyncOptions) (schedule.Dataset, pathe.SyncSummary, error) {
			return schedule.Dataset{}, pathe.SyncSummary{}, fetchErr
		},
	}
}

func TestProductionExecutorRejectsInvalidDataAndCommitFailure(t *testing.T) {
	writes := 0
	executor := &ProductionExecutor{
		now: time.Now, logger: slog.New(slog.DiscardHandler),
		writer: writerFunc(func(context.Context, []schedule.Dataset) (int64, error) { writes++; return 0, errors.New("db secret") }),
		newUGC: func() (ugc.Getter, error) { return unusedGetter{}, nil },
		syncUGC: func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
			return schedule.Dataset{}, ugc.SyncSummary{}, nil
		},
	}
	if _, err := executor.Run(context.Background(), TargetUGC, Window{}); err == nil || writes != 0 {
		t.Fatalf("invalid data err=%v writes=%d", err, writes)
	}
	executor.syncUGC = func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
		return validDataset(t, schedule.ProviderUGC, Window{From: "2026-08-17"}), ugc.SyncSummary{}, nil
	}
	if _, err := executor.Run(context.Background(), TargetUGC, Window{From: "2026-08-17"}); err == nil || writes != 1 {
		t.Fatalf("commit err=%v writes=%d", err, writes)
	}
}

func TestProductionExecutorFailureCodes(t *testing.T) {
	window := Window{From: "2026-08-17"}
	base := func() *ProductionExecutor {
		return &ProductionExecutor{
			now: time.Now, logger: slog.New(slog.DiscardHandler),
			writer: writerFunc(func(context.Context, []schedule.Dataset) (int64, error) { return 7, nil }),
			newUGC: func() (ugc.Getter, error) { return unusedGetter{}, nil },
			syncUGC: func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
				return validDataset(t, schedule.ProviderUGC, window), ugc.SyncSummary{}, nil
			},
		}
	}
	tests := []struct {
		name string
		code FailureCode
		run  func(*ProductionExecutor) error
	}{
		{name: "missing factory", code: FailureInternal, run: func(e *ProductionExecutor) error {
			e.newUGC = nil
			_, err := e.Run(context.Background(), TargetUGC, window)
			return err
		}},
		{name: "invalid target", code: FailureInternal, run: func(e *ProductionExecutor) error {
			_, err := e.Run(context.Background(), Target("bad"), window)
			return err
		}},
		{name: "client creation", code: FailureClientCreation, run: func(e *ProductionExecutor) error {
			e.newUGC = func() (ugc.Getter, error) { return nil, errors.New("secret") }
			_, err := e.Run(context.Background(), TargetUGC, window)
			return err
		}},
		{name: "provider sync", code: FailureProviderSync, run: func(e *ProductionExecutor) error {
			e.syncUGC = func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
				return schedule.Dataset{}, ugc.SyncSummary{}, errors.New("secret")
			}
			_, err := e.Run(context.Background(), TargetUGC, window)
			return err
		}},
		{name: "dataset rejected", code: FailureDatasetRejected, run: func(e *ProductionExecutor) error {
			e.syncUGC = func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
				return schedule.Dataset{}, ugc.SyncSummary{}, nil
			}
			_, err := e.Run(context.Background(), TargetUGC, window)
			return err
		}},
		{name: "replacement", code: FailureReplacement, run: func(e *ProductionExecutor) error {
			e.writer = writerFunc(func(context.Context, []schedule.Dataset) (int64, error) { return 0, errors.New("secret") })
			_, err := e.Run(context.Background(), TargetUGC, window)
			return err
		}},
		{name: "parent canceled", code: FailureCanceled, run: func(e *ProductionExecutor) error {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			e.syncUGC = func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
				return schedule.Dataset{}, ugc.SyncSummary{}, context.Canceled
			}
			_, err := e.Run(ctx, TargetUGC, window)
			return err
		}},
		{name: "parent deadline", code: FailureCanceled, run: func(e *ProductionExecutor) error {
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel()
			e.syncUGC = func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
				return schedule.Dataset{}, ugc.SyncSummary{}, context.DeadlineExceeded
			}
			_, err := e.Run(ctx, TargetUGC, window)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertRunErrorCode(t, test.run(base()), test.code)
		})
	}
	if err := stageError(context.Background(), TargetUGC, StagePublication, FailureReplacement, context.DeadlineExceeded); err == nil {
		t.Fatal("child timeout returned nil")
	} else {
		assertRunErrorCode(t, err, FailureReplacement)
	}
}

func TestProductionExecutorClassifiesEmptyUGCOutputsAsDatasetRejections(t *testing.T) {
	window := Window{From: "2026-08-17"}
	for _, name := range []string{"zero cinemas", "zero showtimes"} {
		t.Run(name, func(t *testing.T) {
			executor := &ProductionExecutor{
				now: time.Now, logger: slog.New(slog.DiscardHandler),
				writer: writerFunc(func(context.Context, []schedule.Dataset) (int64, error) {
					t.Fatal("published rejected dataset")
					return 0, nil
				}),
				newUGC: func() (ugc.Getter, error) { return unusedGetter{}, nil },
				syncUGC: func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
					return schedule.Dataset{}, ugc.SyncSummary{}, fmt.Errorf("%w: %s", schedule.ErrDatasetValidation, name)
				},
			}
			_, err := executor.Run(context.Background(), TargetUGC, window)
			var runErr *RunError
			if !errors.As(err, &runErr) || runErr.Stage != StageDatasetValidation || runErr.Code != FailureDatasetRejected {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestProductionExecutorEnrichmentOutcomes(t *testing.T) {
	window := Window{From: "2026-08-17"}
	tests := []struct {
		name   string
		enrich EnrichFunc
		status EnrichmentState
		counts *EnrichmentCounts
	}{
		{name: "nil function", status: "skipped"},
		{name: "lazy skip", enrich: func(context.Context, []enrichment.Movie) (*enrichment.Summary, error) { return nil, nil }, status: "skipped"},
		{name: "degraded without counts", enrich: func(context.Context, []enrichment.Movie) (*enrichment.Summary, error) {
			return nil, errors.New("secret")
		}, status: "degraded"},
		{name: "complete", enrich: func(context.Context, []enrichment.Movie) (*enrichment.Summary, error) {
			return &enrichment.Summary{Matched: 2}, nil
		}, status: "complete", counts: &EnrichmentCounts{Matched: 2}},
		{name: "degraded", enrich: func(context.Context, []enrichment.Movie) (*enrichment.Summary, error) {
			return &enrichment.Summary{Failed: 1}, errors.New("secret")
		}, status: "degraded", counts: &EnrichmentCounts{Failed: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &ProductionExecutor{
				now: time.Now, logger: slog.New(slog.DiscardHandler), enrich: test.enrich,
				writer: writerFunc(func(context.Context, []schedule.Dataset) (int64, error) { return 7, nil }),
				newUGC: func() (ugc.Getter, error) { return unusedGetter{}, nil },
				syncUGC: func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
					return validDataset(t, schedule.ProviderUGC, window), ugc.SyncSummary{}, nil
				},
			}
			outcomes, err := executor.Run(context.Background(), TargetUGC, window)
			outcome := outcomes[TargetUGC]
			if err != nil || outcome.Sync.Version != 7 || outcome.Enrichment.Status != test.status {
				t.Fatalf("outcome=%+v err=%v", outcome, err)
			}
			if test.counts == nil && outcome.Enrichment.Counts != nil || test.counts != nil && (outcome.Enrichment.Counts == nil || *outcome.Enrichment.Counts != *test.counts) {
				t.Fatalf("counts=%+v want=%+v", outcome.Enrichment.Counts, test.counts)
			}
		})
	}
}

func TestEnrichmentMoviesKeepsEarliestShowing(t *testing.T) {
	data := validDataset(t, schedule.ProviderUGC, Window{From: "2026-08-17"})
	latest := data.Showtimes[0]
	latest.ID = "ugc-showing-latest"
	latest.ProviderShowingID = "latest"
	latest.StartTime = latest.StartTime.Add(3 * time.Hour)
	earliest := data.Showtimes[0]
	earliest.ID = "ugc-showing-earliest"
	earliest.ProviderShowingID = "earliest"
	earliest.StartTime = earliest.StartTime.Add(-2 * time.Hour)
	data.Showtimes = []schedule.ShowtimeRecord{latest, data.Showtimes[0], earliest}
	movies := enrichmentMovies(TargetUGC, data)
	if len(movies) != 1 || !movies[0].FirstShowingAt.Equal(earliest.StartTime) {
		t.Fatalf("movies=%+v", movies)
	}
}

func assertRunErrorCode(t *testing.T, err error, code FailureCode) {
	t.Helper()
	var runError *RunError
	if !errors.As(err, &runError) {
		t.Fatalf("err=%v is not RunError", err)
	}
	if runError.Code != code {
		t.Fatalf("err=%v code=%q want=%q", err, runError.Code, code)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("public error leaked cause: %v", err)
	}
}

func validDataset(t *testing.T, provider schedule.Provider, window Window) schedule.Dataset {
	t.Helper()
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 8, 17, 12, 0, 0, 0, location)
	theaterID, theaterProviderID, movieID, showingID := string(provider)+"-cinema", "cinema", "movie", "showing"
	address, postal, passes := "", "", []string{}
	booking := "https://kinepolis.fr/direct-vista-redirect/showing/0/cinema/0"
	switch provider {
	case schedule.ProviderUGC:
		theaterID, theaterProviderID, movieID, showingID = "ugc-25", "25", "200", "100"
		address, postal, passes = "1 rue", "59000", []string{"UGC_ILLIMITE"}
		booking = "https://www.ugc.fr/reservationSeances.html?id=100"
	case schedule.ProviderPathe:
		theaterID, theaterProviderID, movieID, showingID = "pathe-cinema", "cinema", "movie", "V3308S1"
		address, postal = "1 rue", "59000"
		booking = "https://s.pathe.fr/fr/V3308S1/booking"
	case schedule.ProviderCGR:
		theaterID, theaterProviderID, movieID, showingID = "cgr-W8010", "W8010", "1001", "W8010-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		address, postal = "1 rue", "59000"
		booking = "https://achat.cgrcinemas.fr/lille/r/123"
	}
	return schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: provider, Scope: schedule.ScopeAll, GeneratedAt: time.Now().UTC(), Timezone: schedule.Timezone, Window: schedule.Window{From: window.From, Through: window.From},
		Theaters:  []schedule.TheaterRecord{{ID: theaterID, ProviderID: theaterProviderID, Slug: theaterID, Name: "Cinéma", Address: address, City: "Lille", PostalCode: postal, AvailableDates: []string{window.From}, AcceptedPasses: passes}},
		Showtimes: []schedule.ShowtimeRecord{{ID: string(provider) + "-showing-" + showingID, ProviderShowingID: showingID, ServiceDate: window.From, TheaterID: theaterID, Movie: schedule.MovieRecord{ProviderID: movieID, Slug: string(provider) + "-film-" + movieID, Title: "Film", RuntimeMinutes: 100}, StartTime: start, EndTime: start.Add(100 * time.Minute), Language: schedule.LanguageVF, ProviderVersion: "VF", Format: "2D", BookingURL: booking}},
	}
}
