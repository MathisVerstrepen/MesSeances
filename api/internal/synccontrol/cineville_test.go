package synccontrol

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/cineville"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type cinevillePayloadFetcher struct {
	bootstrap, page string
	requests        int
}

func (f *cinevillePayloadFetcher) Fetch(context.Context) ([]byte, error) {
	f.requests++
	return []byte(f.bootstrap), nil
}

func (f *cinevillePayloadFetcher) FetchCinema(context.Context, string, string) ([]byte, error) {
	f.requests++
	return []byte(f.page), nil
}

func (f *cinevillePayloadFetcher) RequestCount() int { return f.requests }

func TestCinevilleExecutorPayloadAndFinalValidationFailures(t *testing.T) {
	const catalog = `[{"id":639,"cine":"katorzaquimper","nom_cine_public":"Katorza","adresse_ville":"Quimper","code_postal_1":"29000"}]`
	const bootstrap = `<html><script id="__NEXT_DATA__" type="application/json">{"buildId":"build-1","props":{"pageProps":{"cinemas":` + catalog + `}}}</script></html>`
	const emptyPage = `{"pageProps":{"cines":` + catalog + `,"cinemaId":639,"prog":[],"progWithEvents":[],"attributs":[]}}`
	const sensitive = "synthetic-private-body"
	for _, test := range []struct {
		name, bootstrap, page, operation, category string
		stage                                      FailureStage
		code                                       FailureCode
		requests                                   int
	}{
		{"malformed bootstrap", "<html>" + sensitive + "</html>", "", "cinemas", "invalid_payload", StageProviderFetch, FailureProviderSync, 1},
		{"malformed page", bootstrap, `{"pageProps":{"cinemaId":"` + sensitive + `"}}`, "program", "invalid_payload", StageProviderFetch, FailureProviderSync, 2},
		{"final empty dataset", bootstrap, emptyPage, "dataset_validation", "validation", StageDatasetValidation, FailureDatasetRejected, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			fetcher := &cinevillePayloadFetcher{bootstrap: test.bootstrap, page: test.page}
			executor, err := NewProductionExecutor(ProductionExecutorOptions{
				Writer: writerFunc(func(context.Context, []schedule.Dataset) (int64, error) {
					t.Fatal("invalid provider data reached publication")
					return 0, nil
				}),
				NewCineville: func() (cineville.Fetcher, error) { return fetcher, nil },
				Enrich: func(context.Context, []enrichment.Movie) (*enrichment.Summary, error) {
					t.Fatal("invalid provider data reached enrichment")
					return nil, nil
				},
				Now:    func() time.Time { return time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC) },
				Logger: slog.New(slog.NewJSONHandler(&logs, nil)), OperationTimeout: time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = executor.Run(t.Context(), TargetCineville, Window{From: "2026-09-14"})
			var re *RunError
			if !errors.As(err, &re) || re.Stage != test.stage || re.Code != test.code || re.Provider != TargetCineville {
				t.Fatalf("failure=%v", err)
			}
			operational := strings.Join(re.logs[TargetCineville], "\n")
			for _, want := range []string{"event=provider_failed stage=" + string(test.stage), "operation=" + test.operation + " category=" + test.category, "requests=" + strconv.Itoa(test.requests)} {
				if !strings.Contains(operational, want) {
					t.Fatalf("missing %q: %s", want, operational)
				}
			}
			finalValidation := test.stage == StageDatasetValidation
			if strings.Contains(operational, "event=fetch_succeeded") != finalValidation || fetcher.requests != test.requests {
				t.Fatal("misleading fetch success or unexpected retry")
			}
			if !finalValidation {
				if strings.Contains(operational+logs.String(), "dataset_validation") || !strings.Contains(logs.String(), `"fetch_category":"invalid_payload"`) || !strings.Contains(logs.String(), `"request_operation":"`+test.operation+`"`) {
					t.Fatal("payload classification lost in logs")
				}
			}
			combined := operational + logs.String() + err.Error()
			for _, forbidden := range []string{sensitive, "__NEXT_DATA__", "pageProps", "event=validation_succeeded", "event=publication_started"} {
				if strings.Contains(combined, forbidden) {
					t.Fatalf("output contains %q", forbidden)
				}
			}
		})
	}
}

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
	if status.State != StateSucceeded || status.Providers["cineville"].State != ProviderSucceeded || status.Providers["ugc"].State != ProviderNotRequested || len(status.Providers) != 10 {
		t.Fatal("Cineville status")
	}
	status.Providers["cineville"] = ProviderStatus{State: "mutated"}
	if manager.Status().Providers["cineville"].State != "succeeded" {
		t.Fatal("status mutation")
	}
}
