package synccontrol

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/grandecran"
	"messeances/api/internal/mk2"
	"messeances/api/internal/noecinemas"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
	"messeances/api/internal/veo"
)

func TestProviderExecutorMetricsFailuresAndNonfatalEnrichment(t *testing.T) {
	for _, test := range []struct {
		provider  Target
		movieID   string
		requests  int
		configure func(*testing.T, *ProductionExecutor, Window)
		failFetch func(*ProductionExecutor)
	}{
		{TargetMK2, "HO00006568", 27, configureMK2TestExecutor, func(e *ProductionExecutor) {
			e.syncMK2 = func(context.Context, mk2.Fetcher, mk2.SyncOptions) (schedule.Dataset, mk2.SyncSummary, error) {
				return schedule.Dataset{}, mk2.SyncSummary{}, &mk2.RequestError{Operation: mk2.OperationComplex, Kind: syncproxy.FailureChallenge, StatusCode: 403}
			}
		}},
		{TargetGrandEcran, "cEvent_1", 27, configureGrandEcranTestExecutor, func(e *ProductionExecutor) {
			e.syncGrandEcran = func(context.Context, grandecran.Getter, grandecran.SyncOptions) (schedule.Dataset, grandecran.SyncSummary, error) {
				return schedule.Dataset{}, grandecran.SyncSummary{}, &grandecran.RequestError{Operation: grandecran.OperationRoom, Kind: syncproxy.FailureChallenge, StatusCode: 403}
			}
		}},
		{TargetNoeCinemas, "cEvent_1", 27, configureNoeCinemasTestExecutor, func(e *ProductionExecutor) {
			e.syncNoeCinemas = func(context.Context, noecinemas.Getter, noecinemas.SyncOptions) (schedule.Dataset, noecinemas.SyncSummary, error) {
				return schedule.Dataset{}, noecinemas.SyncSummary{}, &noecinemas.RequestError{Operation: noecinemas.OperationRoom, Kind: syncproxy.FailureChallenge, StatusCode: 403}
			}
		}},
		{TargetVeo, "cEvent_1", 29, configureVeoTestExecutor, func(e *ProductionExecutor) {
			e.syncVeo = func(context.Context, veo.Getter, veo.SyncOptions) (schedule.Dataset, veo.SyncSummary, error) {
				return schedule.Dataset{}, veo.SyncSummary{}, &veo.RequestError{Operation: veo.OperationCheckout, Kind: syncproxy.FailureChallenge, StatusCode: 403}
			}
		}},
	} {
		t.Run(string(test.provider), func(t *testing.T) {
			window := Window{From: "2026-08-17"}
			writes, enrichments := 0, 0
			e := &ProductionExecutor{now: time.Now, logger: slog.New(slog.DiscardHandler), operationTimeout: time.Second,
				writer: writerFunc(func(_ context.Context, d []schedule.Dataset) (int64, error) {
					writes++
					if len(d) != 1 || d[0].Provider != schedule.Provider(test.provider) {
						t.Fatal("wrong publication")
					}
					return 1, nil
				}),
				enrich: func(_ context.Context, m []enrichment.Movie) (*enrichment.Summary, error) {
					enrichments++
					if writes != 1 || len(m) != 1 || m[0].SourceProvider != string(test.provider) || m[0].ProviderID != test.movieID || m[0].RuntimeMinutes != 0 {
						t.Fatal("source enrichment")
					}
					return nil, errors.New("synthetic-enrichment-secret")
				},
			}
			test.configure(t, e, window)
			out, err := e.Run(t.Context(), test.provider, window)
			if err != nil || out[test.provider].Sync.Requests != test.requests || writes != 1 || enrichments != 1 || out[test.provider].Enrichment.Status != EnrichmentDegraded {
				t.Fatalf("out=%+v err=%v", out, err)
			}
			test.failFetch(e)
			_, err = e.Run(t.Context(), test.provider, window)
			var re *RunError
			if !errors.As(err, &re) || re.Provider != test.provider || re.Stage != StageProviderFetch || writes != 1 || enrichments != 1 || !strings.Contains(strings.Join(re.logs[test.provider], " "), "category=challenge") {
				t.Fatalf("fetch failure=%v", err)
			}
			test.configure(t, e, window)
			e.writer = writerFunc(func(context.Context, []schedule.Dataset) (int64, error) { return 0, errors.New("publication-secret") })
			if _, err = e.Run(t.Context(), test.provider, window); err == nil || strings.Contains(err.Error(), "secret") || enrichments != 1 {
				t.Fatal("publication failure", err)
			}
		})
	}
}

func TestProviderManagerSelectionAndStatusCloning(t *testing.T) {
	for _, provider := range []Target{TargetMK2, TargetGrandEcran, TargetNoeCinemas, TargetVeo, TargetMajestic, TargetCineode, TargetCinemovida, TargetCinewest, TargetCineville} {
		t.Run(string(provider), func(t *testing.T) {
			for _, mode := range []string{"manual", "scheduled"} {
				t.Run(mode, func(t *testing.T) {
					calls := make(chan Target, 14)
					m, err := newTestManager(t.Context(), time.Now, executorFunc(func(_ context.Context, target Target, _ Window) (ProviderOutcome, error) {
						calls <- target
						return ProviderOutcome{}, nil
					}))
					if err != nil {
						t.Fatal(err)
					}
					defer m.Close()
					if mode == "scheduled" {
						_, _, err = m.StartScheduled(Occurrence{ScheduleID: 1, Provider: provider, Revision: 1, ScheduledFor: time.Now().UTC()})
					} else {
						_, err = m.Start(provider)
					}
					if err != nil {
						t.Fatal(err)
					}
					status := waitForTerminal(t, m)
					if status.State != StateSucceeded || len(status.Providers) != 14 || len(calls) != 1 || <-calls != provider {
						t.Fatalf("status=%+v", status)
					}
					if mode == "scheduled" && (status.Occurrence == nil || status.Occurrence.Provider != provider) {
						t.Fatal("scheduled occurrence lost")
					}
					for name, state := range status.Providers {
						want := ProviderNotRequested
						if name == string(provider) {
							want = ProviderSucceeded
						}
						if state.State != want {
							t.Fatalf("provider %s: got %s want %s", name, state.State, want)
						}
					}
					status.Providers[string(provider)] = ProviderStatus{State: "mutated"}
					if m.Status().Providers[string(provider)].State != ProviderSucceeded {
						t.Fatal("status mutation")
					}
				})
			}
		})
	}
}
