package synccontrol

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/kinepolis"
	"messeances/api/internal/pathe"
	"messeances/api/internal/schedule"
	"messeances/api/internal/tmdb"
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

func (unusedGetter) Get(context.Context, string, string) (ugc.FetchResult, error) {
	return ugc.FetchResult{}, nil
}
func (unusedGetter) RequestCount() int { return 0 }

type unusedFetcher struct{}

func (unusedFetcher) Fetch(context.Context) ([]byte, error) { return nil, nil }

type countedFetcher struct{ requests int }

func (countedFetcher) Fetch(context.Context) ([]byte, error) { return nil, nil }
func (f countedFetcher) RequestCount() int                   { return f.requests }

type unusedPatheGetter struct{}

func (unusedPatheGetter) Get(context.Context, pathe.Operation, string) ([]byte, error) {
	return nil, nil
}
func (unusedPatheGetter) RequestCount() int { return 0 }

type fakeEnrichmentStore struct{}

func (fakeEnrichmentStore) IsLocallyMerged(context.Context, string, string) (bool, error) {
	return false, nil
}
func (fakeEnrichmentStore) Match(context.Context, string, string, string) (enrichment.Match, bool, error) {
	return enrichment.Match{}, false, nil
}
func (fakeEnrichmentStore) ConfirmedMatches(context.Context, string, string, int, int) ([]enrichment.ReusableMetadataMatch, error) {
	return nil, nil
}
func (fakeEnrichmentStore) Metadata(context.Context, string, int64, string) (enrichment.Metadata, bool, error) {
	return enrichment.Metadata{}, false, nil
}
func (fakeEnrichmentStore) SaveDecision(context.Context, enrichment.Match) error { return nil }
func (fakeEnrichmentStore) Publish(context.Context, enrichment.Match, enrichment.Metadata) error {
	return nil
}

type fakeEnrichmentProvider struct{}

func (fakeEnrichmentProvider) Search(context.Context, string) ([]tmdb.Candidate, error) {
	return nil, nil
}
func (fakeEnrichmentProvider) Details(context.Context, int64) (tmdb.Details, error) {
	return tmdb.Details{}, nil
}

func TestProductionExecutorCommitsBeforeNonFatalEnrichment(t *testing.T) {
	window := Window{From: "2026-08-17"}
	events := []string{}
	executor := &ProductionExecutor{
		now: time.Now, logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		writer: writerFunc(func(_ context.Context, data []schedule.Dataset) (int64, error) {
			events = append(events, "commit:"+string(data[0].Provider))
			return 1, nil
		}),
		newUGC:       func() (ugc.Getter, error) { return unusedGetter{}, nil },
		newKinepolis: func() (kinepolis.Fetcher, error) { return unusedFetcher{}, nil },
		newPathe:     func() (pathe.Getter, error) { return unusedPatheGetter{}, nil },
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
		enrich: func(_ context.Context, movies []enrichment.Movie) (*enrichment.Summary, error) {
			events = append(events, "enrich:"+movies[0].SourceProvider)
			return nil, errors.New("degraded")
		},
	}
	for _, provider := range []Target{TargetUGC, TargetKinepolis, TargetPathe} {
		if _, err := executor.Run(context.Background(), provider, window); err != nil {
			t.Fatalf("provider=%s err=%v", provider, err)
		}
	}
	want := []string{"commit:ugc", "enrich:ugc", "commit:kinepolis", "enrich:kinepolis", "commit:pathe", "enrich:pathe"}
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
		now: time.Now, logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		writer: writerFunc(func(_ context.Context, datasets []schedule.Dataset) (int64, error) {
			writes++
			if len(datasets) != 3 || datasets[0].Provider != schedule.ProviderUGC || datasets[1].Provider != schedule.ProviderKinepolis || datasets[2].Provider != schedule.ProviderPathe || datasets[0].Window.Through != "2027-01-10" || datasets[1].Window.Through != "2026-11-20" || datasets[2].Window.Through != "2026-12-15" {
				t.Fatalf("datasets=%+v", datasets)
			}
			return 11, nil
		}),
		newUGC:       func() (ugc.Getter, error) { return unusedGetter{}, nil },
		newKinepolis: func() (kinepolis.Fetcher, error) { return unusedFetcher{}, nil },
		newPathe:     func() (pathe.Getter, error) { return unusedPatheGetter{}, nil },
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
		enrich: func(context.Context, []enrichment.Movie) (*enrichment.Summary, error) {
			enrichments++
			if writes != 1 {
				t.Fatal("enrichment ran before publication")
			}
			return nil, nil
		},
	}
	outcomes, err := executor.Run(context.Background(), TargetAll, window)
	if err != nil || writes != 1 || enrichments != 3 || outcomes[TargetUGC].Sync.Version != 11 || outcomes[TargetKinepolis].Sync.Version != 11 || outcomes[TargetPathe].Sync.Version != 11 || outcomes[TargetUGC].Sync.Through != "2027-01-10" || outcomes[TargetKinepolis].Sync.Through != "2026-11-20" || outcomes[TargetPathe].Sync.Through != "2026-12-15" || outcomes[TargetPathe].Sync.Requests != 17 {
		t.Fatalf("outcomes=%+v writes=%d enrichments=%d err=%v", outcomes, writes, enrichments, err)
	}
}

func TestProductionExecutorPopulatesKinepolisRequestCount(t *testing.T) {
	window := Window{From: "2026-08-17"}
	executor := &ProductionExecutor{
		now: time.Now, logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
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

func TestProductionExecutorTargetAllSecondPreparationAndPublicationFailuresAreAtomic(t *testing.T) {
	window := Window{From: "2026-08-17"}
	writes, enrichments := 0, 0
	executor := &ProductionExecutor{
		now: time.Now, logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		writer: writerFunc(func(context.Context, []schedule.Dataset) (int64, error) { writes++; return 0, nil }),
		newUGC: func() (ugc.Getter, error) { return unusedGetter{}, nil }, newKinepolis: func() (kinepolis.Fetcher, error) { return unusedFetcher{}, nil },
		newPathe: func() (pathe.Getter, error) { return unusedPatheGetter{}, nil },
		syncUGC: func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
			return validDataset(t, schedule.ProviderUGC, window), ugc.SyncSummary{}, nil
		},
		syncKinepolis: func(context.Context, kinepolis.Fetcher, kinepolis.SyncOptions) (schedule.Dataset, kinepolis.SyncSummary, error) {
			return schedule.Dataset{}, kinepolis.SyncSummary{}, errors.New("second-provider-secret")
		},
		syncPathe: func(context.Context, pathe.Getter, pathe.SyncOptions) (schedule.Dataset, pathe.SyncSummary, error) {
			return validDataset(t, schedule.ProviderPathe, window), pathe.SyncSummary{}, nil
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
		{name: "forbidden", category: pathe.CategoryStatus, status: 403, operation: pathe.OperationShows, wantCategory: "status", wantOperation: "shows", wantStatus: "403"},
		{name: "rate limited", category: pathe.CategoryStatus, status: 429, operation: pathe.OperationCinemaProgram, wantCategory: "status", wantOperation: "cinema_program", wantStatus: "429"},
		{name: "server status", category: pathe.CategoryServer, status: 503, operation: pathe.OperationMovieTimes, wantCategory: "status", wantOperation: "movie_showtimes", wantStatus: "503"},
		{name: "challenge", category: pathe.CategoryChallenge, operation: pathe.OperationEventTimes, wantCategory: "challenge", wantOperation: "event_showtimes"},
		{name: "content type", category: pathe.CategoryContentType, operation: pathe.OperationCinemas, wantCategory: "content_type", wantOperation: "cinemas"},
		{name: "redirect", category: pathe.CategoryRedirect, operation: pathe.OperationShows, wantCategory: "redirect", wantOperation: "shows"},
		{name: "response too large", category: pathe.CategoryResponseLarge, operation: pathe.OperationCinemaProgram, wantCategory: "response_too_large", wantOperation: "cinema_program"},
		{name: "invalid JSON", category: pathe.CategoryInvalidJSON, operation: pathe.OperationMovieTimes, wantCategory: "invalid_json", wantOperation: "movie_showtimes"},
		{name: "canceled", category: pathe.CategoryCanceled, operation: pathe.OperationEventTimes, wantCategory: "canceled", wantOperation: "event_showtimes"},
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
		now: time.Now, logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
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
			now: time.Now, logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
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
				now: time.Now, logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
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
				now: time.Now, logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), enrich: test.enrich,
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
	if provider == schedule.ProviderUGC {
		theaterID, theaterProviderID, movieID, showingID = "ugc-25", "25", "200", "100"
		address, postal, passes = "1 rue", "59000", []string{"UGC_ILLIMITE"}
		booking = "https://www.ugc.fr/reservationSeances.html?id=100"
	} else if provider == schedule.ProviderPathe {
		theaterID, theaterProviderID, movieID, showingID = "pathe-cinema", "cinema", "movie", "V3308S1"
		address, postal = "1 rue", "59000"
		booking = "https://s.pathe.fr/fr/V3308S1/booking"
	}
	return schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: provider, Scope: schedule.ScopeAll, GeneratedAt: time.Now().UTC(), Timezone: schedule.Timezone, Window: schedule.Window{From: window.From, Through: window.From},
		Theaters:  []schedule.TheaterRecord{{ID: theaterID, ProviderID: theaterProviderID, Slug: theaterID, Name: "Cinéma", Address: address, City: "Lille", PostalCode: postal, AvailableDates: []string{window.From}, AcceptedPasses: passes}},
		Showtimes: []schedule.ShowtimeRecord{{ID: string(provider) + "-showing-" + showingID, ProviderShowingID: showingID, ServiceDate: window.From, TheaterID: theaterID, Movie: schedule.MovieRecord{ProviderID: movieID, Slug: string(provider) + "-film-" + movieID, Title: "Film", RuntimeMinutes: 100}, StartTime: start, EndTime: start.Add(100 * time.Minute), Language: schedule.LanguageVF, ProviderVersion: "VF", Format: "2D", BookingURL: booking}},
	}
}
