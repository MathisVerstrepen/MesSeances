package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/schedule"
	"messeances/api/internal/synccontrol"
	"messeances/api/internal/tmdb"
	"messeances/api/internal/ugc"
)

type fakeWriter struct {
	calls     int
	version   int64
	err       error
	onReplace func()
}

func (w *fakeWriter) Replace(context.Context, []schedule.Dataset) (int64, error) {
	w.calls++
	if w.onReplace != nil {
		w.onReplace()
	}
	return w.version, w.err
}

type commandStore struct{}

func (commandStore) IsLocallyMerged(context.Context, string, string) (bool, error) {
	return false, nil
}
func (commandStore) Match(context.Context, string, string, string) (enrichment.Match, bool, error) {
	return enrichment.Match{}, false, nil
}
func (commandStore) Metadata(context.Context, string, int64, string) (enrichment.Metadata, bool, error) {
	return enrichment.Metadata{}, false, nil
}
func (commandStore) SaveDecision(context.Context, enrichment.Match) error                 { return nil }
func (commandStore) Publish(context.Context, enrichment.Match, enrichment.Metadata) error { return nil }

type commandProvider struct{}

func (commandProvider) Search(context.Context, string) ([]tmdb.Candidate, error) { return nil, nil }
func (commandProvider) Details(context.Context, int64) (tmdb.Details, error) {
	return tmdb.Details{}, nil
}

func proxyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "proxies.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func commandDataset(scope string) schedule.Dataset {
	location, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 8, 15, 12, 0, 0, 0, location)
	return schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderUGC, Scope: scope, GeneratedAt: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC), Timezone: schedule.Timezone, Window: schedule.Window{From: "2026-08-15", Through: "2026-08-15"}, Theaters: []schedule.TheaterRecord{{ID: "ugc-25", ProviderID: "25", Slug: "ugc-25", Name: "UGC Lille", Address: "Lille", City: "Lille", PostalCode: "59000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}}}, Showtimes: []schedule.ShowtimeRecord{{ID: "ugc-showing-100", ProviderShowingID: "100", ServiceDate: "2026-08-15", TheaterID: "ugc-25", Movie: schedule.MovieRecord{ProviderID: "10", Slug: "ugc-film-10", Title: "Film", RuntimeMinutes: 90}, StartTime: start, EndTime: start.Add(90 * time.Minute), Language: schedule.LanguageVF, ProviderVersion: "VF", Format: "2D", Room: "Salle 1", BookingURL: "https://www.ugc.fr/reservationSeances.html?id=100"}}}
}

func fakeSync(ctx context.Context, getter ugc.Getter, options ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
	scope := schedule.ScopeAll
	if options.CinemaID != "" {
		scope = schedule.ScopeSingle
	}
	data := commandDataset(scope)
	return data, ugc.SyncSummary{Scope: scope, Cinemas: 1, Dates: 1, Showtimes: 1, GeneratedAt: data.GeneratedAt}, nil
}

func fixedNow() time.Time { return time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC) }

func assertJSONLog(t *testing.T, raw, event, code string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	var entry map[string]any
	if len(lines) == 0 || json.Unmarshal([]byte(lines[len(lines)-1]), &entry) != nil {
		t.Fatalf("invalid JSON log=%q", raw)
	}
	if entry["msg"] != event || entry["error_code"] != code || entry["component"] != "sync_ugc" {
		t.Fatalf("log=%+v", entry)
	}
}

type commandExecutorFunc func(context.Context, synccontrol.Target, synccontrol.Window) (synccontrol.ProviderOutcome, error)

func (f commandExecutorFunc) Run(ctx context.Context, target synccontrol.Target, window synccontrol.Window) (map[synccontrol.Target]synccontrol.ProviderOutcome, error) {
	outcome, err := f(ctx, target, window)
	return map[synccontrol.Target]synccontrol.ProviderOutcome{target: outcome}, err
}

func testExecutorFactory(t *testing.T) func(synccontrol.ProductionExecutorOptions) (fullExecutor, error) {
	t.Helper()
	return func(options synccontrol.ProductionExecutorOptions) (fullExecutor, error) {
		return commandExecutorFunc(func(ctx context.Context, _ synccontrol.Target, _ synccontrol.Window) (synccontrol.ProviderOutcome, error) {
			data := commandDataset(schedule.ScopeAll)
			version, err := options.Writer.Replace(ctx, []schedule.Dataset{data})
			if err != nil {
				return synccontrol.ProviderOutcome{}, synccontrol.NewRunError(synccontrol.FailureReplacement, err)
			}
			outcome := synccontrol.ProviderOutcome{Sync: synccontrol.SyncOutcome{Version: version, Cinemas: 1, Dates: 1, Showtimes: 1, GeneratedAt: data.GeneratedAt}, Enrichment: synccontrol.EnrichmentOutcome{Status: "skipped"}}
			if options.Enrich != nil {
				summary, enrichErr := options.Enrich(ctx, []enrichment.Movie{{SourceProvider: enrichment.SourceUGC, ProviderID: "10"}})
				if summary != nil || enrichErr != nil {
					outcome.Enrichment.Status = "complete"
					if summary != nil {
						outcome.Enrichment.Counts = &synccontrol.EnrichmentCounts{Reused: summary.Reused, Matched: summary.Matched, ReviewRequired: summary.ReviewRequired, Unmatched: summary.Unmatched, Failed: summary.Failed}
					}
					if enrichErr != nil {
						outcome.Enrichment.Status = "degraded"
					}
				}
			}
			return outcome, nil
		}), nil
	}
}

func TestRunRejectsRemovedRequestInterval(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-request-interval", "0"}, &stdout, &stderr, fixedNow, dependencies{})
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "flag provided but not defined: -request-interval") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunRejectsRemovedCacheFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-cache", "old.json"}, &stdout, &stderr, fixedNow, dependencies{})
	if code != 2 || !strings.Contains(stderr.String(), "flag provided but not defined") || stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunCompleteMissingDatabaseURLBeforeSyncOrDatabase(t *testing.T) {
	path := proxyFile(t, "http://127.0.0.1:8080\n")
	syncCalled, databaseCalled := false, false
	deps := dependencies{getenv: func(name string) string {
		if name != "DATABASE_URL" {
			t.Fatalf("env=%q", name)
		}
		return ""
	}, sync: func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
		syncCalled = true
		return schedule.Dataset{}, ugc.SyncSummary{}, nil
	}, openDatabase: func(context.Context, string) (databaseServices, func(), error) {
		databaseCalled = true
		return databaseServices{}, nil, nil
	}}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", path, "-from", "2026-08-15", "-through", "2026-08-15"}, &stdout, &stderr, fixedNow, deps)
	if code != 2 || syncCalled || databaseCalled {
		t.Fatalf("code=%d sync=%v db=%v stderr=%q", code, syncCalled, databaseCalled, stderr.String())
	}
	assertJSONLog(t, stderr.String(), "configuration_failed", "configuration_error")
}

func TestRunDiagnosticNeverTouchesDatabase(t *testing.T) {
	path := proxyFile(t, "http://127.0.0.1:8080\n")
	deps := dependencies{getenv: func(string) string { t.Fatal("environment lookup called"); return "" }, sync: fakeSync, openDatabase: func(context.Context, string) (databaseServices, func(), error) {
		t.Fatal("database opener called")
		return databaseServices{}, nil, nil
	}}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", path, "-cinema-id", "25", "-from", "2026-08-15", "-through", "2026-08-15"}, &stdout, &stderr, fixedNow, deps)
	want := "sync complete mode=single_cinema persisted=false cinemas=1 skipped=0 dates=1 requests=0 showtimes=1 proxies=1 generated_at=2026-08-14T12:00:00Z\n"
	if code != 0 || stderr.Len() != 0 || stdout.String() != want {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunCompletePersistsExactlyOnceAndCloses(t *testing.T) {
	path := proxyFile(t, "http://127.0.0.1:8080\n")
	writer := &fakeWriter{version: 7}
	closed := false
	deps := dependencies{getenv: func(name string) string {
		if name == "DATABASE_URL" {
			return "postgres://configured"
		}
		return ""
	}, sync: fakeSync, openDatabase: func(context.Context, string) (databaseServices, func(), error) {
		return databaseServices{writer: writer}, func() { closed = true }, nil
	}, newExecutor: testExecutorFactory(t)}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", path, "-from", "2026-08-15", "-through", "2026-08-15"}, &stdout, &stderr, fixedNow, deps)
	want := "sync complete mode=all_cinemas persisted=true version=7 cinemas=1 skipped=0 dates=1 requests=0 showtimes=1 proxies=1 generated_at=2026-08-14T12:00:00Z\nenrichment=skipped\n"
	if code != 0 || writer.calls != 1 || !closed || stderr.Len() != 0 || stdout.String() != want {
		t.Fatalf("code=%d calls=%d closed=%v stdout=%q stderr=%q", code, writer.calls, closed, stdout.String(), stderr.String())
	}
}

func TestRunDatabaseErrorIsRedacted(t *testing.T) {
	path := proxyFile(t, "http://127.0.0.1:8080\n")
	secret := "synthetic-password"
	deps := dependencies{getenv: func(string) string { return "postgres://user:" + secret + "@bad" }, sync: fakeSync, openDatabase: func(context.Context, string) (databaseServices, func(), error) {
		return databaseServices{}, nil, errors.New("parse " + secret)
	}}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", path, "-from", "2026-08-15", "-through", "2026-08-15"}, &stdout, &stderr, fixedNow, deps)
	if code != 1 || strings.Contains(stdout.String()+stderr.String(), secret) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	assertJSONLog(t, stderr.String(), "sync_command_failed", "database_startup_failed")
}

func TestRunRejectsSyntheticProxyWithoutCredentialLeak(t *testing.T) {
	secret := "synthetic-user:synthetic-password"
	path := proxyFile(t, "http://"+secret+"@missing-port\n")
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", path, "-from", "2026-08-15", "-through", "2026-08-15"}, &stdout, &stderr, fixedNow, dependencies{})
	if code != 2 || strings.Contains(stdout.String()+stderr.String(), secret) || strings.Contains(stderr.String(), "synthetic-password") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunDateWindowValidationBeforeSideEffects(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.txt")
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", missing, "-from", "2026-10-18", "-through", "2026-10-31"}, &stdout, &stderr, fixedNow, dependencies{})
	if code != 2 {
		t.Fatalf("14-day code=%d stderr=%q", code, stderr.String())
	}
	assertJSONLog(t, stderr.String(), "configuration_failed", "configuration_error")
	stdout.Reset()
	stderr.Reset()
	code = runWithDependencies(context.Background(), []string{"-proxy-file", missing, "-from", "2026-10-18", "-through", "2026-11-01"}, &stdout, &stderr, fixedNow, dependencies{})
	if code != 2 {
		t.Fatalf("15-day code=%d stderr=%q", code, stderr.String())
	}
	assertJSONLog(t, stderr.String(), "configuration_failed", "configuration_error")
}

func TestRunEnrichmentStartsAfterCommitAndFailureKeepsSuccess(t *testing.T) {
	path := proxyFile(t, "http://127.0.0.1:8080\n")
	committed := false
	writer := &fakeWriter{version: 9, onReplace: func() { committed = true }}
	secret := "synthetic-tmdb-token"
	deps := dependencies{
		getenv: func(name string) string {
			if name == "DATABASE_URL" {
				return "postgres://configured"
			}
			if name == "TMDB_API_READ_ACCESS_TOKEN" {
				return secret
			}
			return ""
		},
		sync: fakeSync,
		openDatabase: func(context.Context, string) (databaseServices, func(), error) {
			return databaseServices{writer: writer, enrichment: commandStore{}}, func() {}, nil
		},
		newExecutor: testExecutorFactory(t),
		newTMDB: func(token string) (enrichment.Provider, error) {
			if token != secret {
				t.Fatal("wrong token")
			}
			return commandProvider{}, nil
		},
		enrich: func(_ context.Context, _ enrichment.Store, _ enrichment.Provider, movies []enrichment.Movie) (enrichment.Summary, error) {
			if !committed {
				t.Fatal("enrichment ran before replacement commit")
			}
			if len(movies) != 1 || movies[0].ProviderID != "10" {
				t.Fatalf("movies=%+v", movies)
			}
			return enrichment.Summary{Matched: 1, Failed: 1}, errors.New("provider body " + secret)
		},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", path, "-from", "2026-08-15", "-through", "2026-08-15"}, &stdout, &stderr, fixedNow, deps)
	combined := stdout.String() + stderr.String()
	if code != 0 || !strings.Contains(stdout.String(), "persisted=true version=9") || !strings.Contains(stdout.String(), "enrichment=degraded") || strings.Contains(combined, secret) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	assertJSONLog(t, stderr.String(), "sync_enrichment_degraded", "enrichment_failed")
}

func TestRunReplacementFailureNeverStartsEnrichment(t *testing.T) {
	path := proxyFile(t, "http://127.0.0.1:8080\n")
	writer := &fakeWriter{err: errors.New("commit uncertain")}
	deps := dependencies{getenv: func(name string) string {
		if name == "DATABASE_URL" {
			return "postgres://configured"
		}
		t.Fatalf("post-commit env lookup %q", name)
		return ""
	}, sync: fakeSync, openDatabase: func(context.Context, string) (databaseServices, func(), error) {
		return databaseServices{writer: writer}, func() {}, nil
	}, newTMDB: func(string) (enrichment.Provider, error) { t.Fatal("TMDB client created"); return nil, nil }, newExecutor: testExecutorFactory(t)}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", path, "-from", "2026-08-15", "-through", "2026-08-15"}, &stdout, &stderr, fixedNow, deps)
	if code != 1 || strings.Contains(stdout.String(), "enrichment=") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	assertJSONLog(t, stderr.String(), "sync_command_failed", "replacement_failed")
}

func TestRunDiagnosticProviderFailureIsStructuredAndRedacted(t *testing.T) {
	path := proxyFile(t, "http://127.0.0.1:8080\n")
	secret := "synthetic-provider-secret"
	deps := dependencies{sync: func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
		return schedule.Dataset{}, ugc.SyncSummary{}, errors.New("provider response " + secret)
	}}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", path, "-cinema-id", "25", "-from", "2026-08-15", "-through", "2026-08-15"}, &stdout, &stderr, fixedNow, deps)
	if code != 1 || stdout.Len() != 0 || strings.Contains(stderr.String(), secret) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	assertJSONLog(t, stderr.String(), "sync_command_failed", "provider_sync_failed")
}
