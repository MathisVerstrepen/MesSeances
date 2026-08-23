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
	"messeances/api/internal/kinepolis"
	"messeances/api/internal/schedule"
	"messeances/api/internal/synccontrol"
)

func fixedNow() time.Time { return time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC) }
func runTest(t *testing.T, args []string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runWithIO(context.Background(), args, fixedNow, &stdout, &stderr)
	return code, stderr.String()
}

func assertJSONLog(t *testing.T, raw, event, code string) {
	t.Helper()
	var entry map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &entry) != nil {
		t.Fatalf("invalid JSON log=%q", raw)
	}
	if entry["msg"] != event || entry["error_code"] != code || entry["component"] != "sync_kinepolis" {
		t.Fatalf("log=%+v", entry)
	}
}

func TestRunRejectsMissingAndInvalidProxyBeforeDatabaseOrNetwork(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if code, stderr := runTest(t, nil); code != 2 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	} else {
		assertJSONLog(t, stderr, "configuration_failed", "configuration_error")
	}
	missing := filepath.Join(t.TempDir(), "missing.txt")
	if code, stderr := runTest(t, []string{"-proxy-file", missing}); code != 2 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	} else {
		assertJSONLog(t, stderr, "configuration_failed", "configuration_error")
	}
	secret := "synthetic-password"
	invalid := filepath.Join(t.TempDir(), "invalid.txt")
	if err := os.WriteFile(invalid, []byte("http://user:"+secret+"@missing-port\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, stderr := runTest(t, []string{"-proxy-file", invalid}); code != 2 || strings.Contains(stderr, secret) {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	} else {
		assertJSONLog(t, stderr, "configuration_failed", "configuration_error")
	}
}

func TestRunAcceptsValidProxyThenRequiresDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	proxyFile := filepath.Join(t.TempDir(), "proxies.txt")
	if err := os.WriteFile(proxyFile, []byte("127.0.0.1:8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	code, stderr := runTest(t, []string{"-proxy-file", proxyFile})
	if code != 2 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	assertJSONLog(t, stderr, "configuration_failed", "configuration_error")
}

func TestRunHelpReturnsBeforeEnvironmentConfiguration(t *testing.T) {
	deps := dependencies{getenv: func(string) string {
		t.Fatal("environment read during help")
		return ""
	}}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-h"}, fixedNow, &stdout, &stderr, deps)
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "Usage of sync-kinepolis:") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunTimingEnvironmentAndExplicitOverridesReachKinepolisClient(t *testing.T) {
	proxyFile := filepath.Join(t.TempDir(), "proxies.txt")
	if err := os.WriteFile(proxyFile, []byte("127.0.0.1:8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		requestEnv   string
		intervalEnv  string
		args         []string
		wantRequest  time.Duration
		wantInterval time.Duration
	}{
		{name: "environment", requestEnv: "7s", intervalEnv: "3s", wantRequest: 7 * time.Second, wantInterval: 3 * time.Second},
		{name: "explicit overrides", requestEnv: "malformed-secret", intervalEnv: "malformed-secret", args: []string{"-timeout", "8s", "-request-interval", "4s"}, wantRequest: 8 * time.Second, wantInterval: 4 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got kinepolis.ClientConfig
			deps := dependencies{
				getenv: func(name string) string {
					switch name {
					case "SYNC_REQUEST_TIMEOUT":
						return test.requestEnv
					case "SYNC_KINEPOLIS_REQUEST_INTERVAL":
						return test.intervalEnv
					default:
						t.Fatalf("unexpected environment lookup %q", name)
						return ""
					}
				},
				newClient: func(config kinepolis.ClientConfig) (kinepolis.Fetcher, error) {
					got = config
					return nil, errors.New("stop after client construction")
				},
			}
			args := append([]string{"-proxy-file", proxyFile}, test.args...)
			var stdout, stderr bytes.Buffer
			if code := runWithDependencies(context.Background(), args, fixedNow, &stdout, &stderr, deps); code != 2 || got.Timeout != test.wantRequest || got.RequestInterval != test.wantInterval {
				t.Fatalf("code=%d config=%+v stdout=%q stderr=%q", code, got, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunOperationTimeoutReachesKinepolisExecutor(t *testing.T) {
	proxyFile := filepath.Join(t.TempDir(), "proxies.txt")
	if err := os.WriteFile(proxyFile, []byte("127.0.0.1:8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var got time.Duration
	deps := dependencies{
		getenv: func(name string) string {
			switch name {
			case "DATABASE_URL":
				return "postgres://configured"
			case "SYNC_OPERATION_TIMEOUT":
				return "37s"
			default:
				return ""
			}
		},
		newClient: func(kinepolis.ClientConfig) (kinepolis.Fetcher, error) { return fakeFetcher{}, nil },
		openDatabase: func(context.Context, string) (databaseServices, func(), error) {
			return databaseServices{writer: fakeWriter{}}, func() {}, nil
		},
		newExecutor: func(options synccontrol.ProductionExecutorOptions) (fullExecutor, error) {
			got = options.OperationTimeout
			return fakeExecutor(func(context.Context, synccontrol.Target, synccontrol.Window) (synccontrol.ProviderOutcome, error) {
				return synccontrol.ProviderOutcome{Enrichment: synccontrol.EnrichmentOutcome{Status: "skipped"}}, nil
			}), nil
		},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", proxyFile}, fixedNow, &stdout, &stderr, deps)
	if code != 0 || got != 37*time.Second {
		t.Fatalf("code=%d operation timeout=%s stdout=%q stderr=%q", code, got, stdout.String(), stderr.String())
	}
}

type fakeFetcher struct{}

func (fakeFetcher) Fetch(context.Context) ([]byte, error) { return nil, nil }

type fakeExecutor func(context.Context, synccontrol.Target, synccontrol.Window) (synccontrol.ProviderOutcome, error)

func (f fakeExecutor) Run(ctx context.Context, target synccontrol.Target, window synccontrol.Window) (map[synccontrol.Target]synccontrol.ProviderOutcome, error) {
	outcome, err := f(ctx, target, window)
	return map[synccontrol.Target]synccontrol.ProviderOutcome{target: outcome}, err
}

type fakeWriter struct{}

func (fakeWriter) Replace(context.Context, []schedule.Dataset) (schedule.PublicationResult, error) {
	return schedule.PublicationResult{Version: 1, Providers: map[schedule.Provider]schedule.PublicationMetrics{schedule.ProviderKinepolis: {Movies: 1, NewMovies: 1, Showtimes: 1, NewShowtimes: 1}}}, nil
}

func TestRunFullPathUsesInjectedDependenciesAndRedactsFailure(t *testing.T) {
	proxyFile := filepath.Join(t.TempDir(), "proxies.txt")
	if err := os.WriteFile(proxyFile, []byte("127.0.0.1:8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	secret := "synthetic-provider-secret"
	closed := false
	deps := dependencies{
		getenv: func(name string) string {
			if name == "DATABASE_URL" {
				return "postgres://configured"
			}
			return ""
		},
		newClient: func(kinepolis.ClientConfig) (kinepolis.Fetcher, error) { return fakeFetcher{}, nil },
		openDatabase: func(context.Context, string) (databaseServices, func(), error) {
			return databaseServices{writer: fakeWriter{}}, func() { closed = true }, nil
		},
		newTMDB: func(string) (enrichment.Provider, error) { t.Fatal("TMDB called"); return nil, nil },
		enrich: func(context.Context, enrichment.Store, enrichment.Provider, []enrichment.Movie) (enrichment.Summary, error) {
			t.Fatal("enrichment called")
			return enrichment.Summary{}, nil
		},
		newExecutor: func(synccontrol.ProductionExecutorOptions) (fullExecutor, error) {
			return fakeExecutor(func(context.Context, synccontrol.Target, synccontrol.Window) (synccontrol.ProviderOutcome, error) {
				return synccontrol.ProviderOutcome{}, synccontrol.NewRunError(synccontrol.FailureProviderSync, errors.New(secret))
			}), nil
		},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", proxyFile}, fixedNow, &stdout, &stderr, deps)
	if code != 1 || !closed || stdout.Len() != 0 || strings.Contains(stderr.String(), secret) {
		t.Fatalf("code=%d closed=%v stdout=%q stderr=%q", code, closed, stdout.String(), stderr.String())
	}
	assertJSONLog(t, stderr.String(), "sync_command_failed", "provider_sync_failed")
}

func TestRunFullPathPreservesStdoutForTypedEnrichmentOutcomes(t *testing.T) {
	proxyFile := filepath.Join(t.TempDir(), "proxies.txt")
	if err := os.WriteFile(proxyFile, []byte("127.0.0.1:8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	generated := time.Date(2026, 8, 15, 12, 30, 0, 0, time.UTC)
	tests := []struct {
		name       string
		enrichment synccontrol.EnrichmentOutcome
		wantLine   string
	}{
		{name: "skipped", enrichment: synccontrol.EnrichmentOutcome{Status: "skipped"}, wantLine: "enrichment=skipped\n"},
		{name: "complete", enrichment: synccontrol.EnrichmentOutcome{Status: "complete", Counts: &synccontrol.EnrichmentCounts{Reused: 1, Matched: 2, ReviewRequired: 3, Unmatched: 4, Failed: 5}}, wantLine: "enrichment=complete reused=1 matched=2 review_required=3 unmatched=4 failed=5\n"},
		{name: "degraded", enrichment: synccontrol.EnrichmentOutcome{Status: "degraded", Counts: &synccontrol.EnrichmentCounts{Failed: 1}}, wantLine: "enrichment=degraded reused=0 matched=0 review_required=0 unmatched=0 failed=1\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			closed := false
			deps := dependencies{
				getenv: func(name string) string {
					if name == "DATABASE_URL" {
						return "postgres://configured"
					}
					return ""
				},
				newClient: func(kinepolis.ClientConfig) (kinepolis.Fetcher, error) { return fakeFetcher{}, nil },
				openDatabase: func(context.Context, string) (databaseServices, func(), error) {
					return databaseServices{writer: fakeWriter{}}, func() { closed = true }, nil
				},
				newExecutor: func(synccontrol.ProductionExecutorOptions) (fullExecutor, error) {
					return fakeExecutor(func(context.Context, synccontrol.Target, synccontrol.Window) (synccontrol.ProviderOutcome, error) {
						return synccontrol.ProviderOutcome{Sync: synccontrol.SyncOutcome{Version: 7, Cinemas: 2, Showtimes: 11, GeneratedAt: generated}, Enrichment: test.enrichment}, nil
					}), nil
				},
			}
			var stdout, stderr bytes.Buffer
			code := runWithDependencies(context.Background(), []string{"-proxy-file", proxyFile}, fixedNow, &stdout, &stderr, deps)
			want := "sync complete provider=kinepolis version=7 cinemas=2 showtimes=11 generated_at=2026-08-15T12:30:00Z\n" + test.wantLine
			if code != 0 || !closed || stdout.String() != want || stderr.Len() != 0 {
				t.Fatalf("code=%d closed=%v stdout=%q stderr=%q", code, closed, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunFullPathReportsReplacementFailureCode(t *testing.T) {
	proxyFile := filepath.Join(t.TempDir(), "proxies.txt")
	if err := os.WriteFile(proxyFile, []byte("127.0.0.1:8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deps := dependencies{
		getenv: func(name string) string {
			if name == "DATABASE_URL" {
				return "postgres://configured"
			}
			return ""
		},
		newClient: func(kinepolis.ClientConfig) (kinepolis.Fetcher, error) { return fakeFetcher{}, nil },
		openDatabase: func(context.Context, string) (databaseServices, func(), error) {
			return databaseServices{writer: fakeWriter{}}, func() {}, nil
		},
		newExecutor: func(synccontrol.ProductionExecutorOptions) (fullExecutor, error) {
			return fakeExecutor(func(context.Context, synccontrol.Target, synccontrol.Window) (synccontrol.ProviderOutcome, error) {
				return synccontrol.ProviderOutcome{}, synccontrol.NewRunError(synccontrol.FailureReplacement, errors.New("secret"))
			}), nil
		},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", proxyFile}, fixedNow, &stdout, &stderr, deps)
	if code != 1 || stdout.Len() != 0 || strings.Contains(stderr.String(), "secret") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	assertJSONLog(t, stderr.String(), "sync_command_failed", "replacement_failed")
}

func TestRunRejectsOtherInvalidConfiguration(t *testing.T) {
	for _, args := range [][]string{{"-from", "invalid"}, {"-through", "2026-09-15"}, {"unexpected"}} {
		if code, _ := runTest(t, args); code != 2 {
			t.Fatalf("args=%v code=%d", args, code)
		}
	}
}

func TestMakeSyncOrchestratesBothProvidersWithSameProxyFile(t *testing.T) {
	body, err := os.ReadFile("../../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Contains(text, "sync-kinepolis:") {
		t.Fatal("public sync-kinepolis target still present")
	}
	ugc := "go run ./cmd/sync-ugc -proxy-file \"$$PROXY_FILE\""
	kinepolis := "go run ./cmd/sync-kinepolis -proxy-file \"$$PROXY_FILE\""
	ugcIndex, kinepolisIndex := strings.Index(text, ugc), strings.Index(text, kinepolis)
	if ugcIndex < 0 || kinepolisIndex < 0 || ugcIndex >= kinepolisIndex {
		t.Fatalf("sync orchestration incorrect: ugc=%d kinepolis=%d", ugcIndex, kinepolisIndex)
	}
	if strings.Count(text, "Usage: make sync PROXY_FILE=/path/to/proxies.txt") != 1 {
		t.Fatal("PROXY_FILE validation missing or duplicated")
	}
}
