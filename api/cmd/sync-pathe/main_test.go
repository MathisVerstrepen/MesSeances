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
	"messeances/api/internal/pathe"
	"messeances/api/internal/schedule"
	"messeances/api/internal/synccontrol"
)

func fixedNow() time.Time { return time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC) }

func proxyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "proxies.txt")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertJSONLog(t *testing.T, raw, event, code string) {
	t.Helper()
	var entry map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &entry) != nil {
		t.Fatalf("invalid JSON log=%q", raw)
	}
	if entry["msg"] != event || entry["error_code"] != code || entry["component"] != "sync_pathe" {
		t.Fatalf("log=%+v", entry)
	}
}

type fakeGetter struct{}

func (fakeGetter) Get(context.Context, pathe.Operation, string) ([]byte, error) { return nil, nil }
func (fakeGetter) RequestCount() int                                            { return 0 }

type fakeWriter struct{}

func (fakeWriter) Replace(context.Context, []schedule.Dataset) (schedule.PublicationResult, error) {
	return schedule.PublicationResult{}, nil
}

type fakeExecutor func(context.Context, synccontrol.Target, synccontrol.Window) (map[synccontrol.Target]synccontrol.ProviderOutcome, error)

func (f fakeExecutor) Run(ctx context.Context, target synccontrol.Target, window synccontrol.Window) (map[synccontrol.Target]synccontrol.ProviderOutcome, error) {
	return f(ctx, target, window)
}

func TestRunRejectsInvalidInputAndRedactsProxyConfiguration(t *testing.T) {
	tests := [][]string{nil, {"-from", "invalid"}, {"unexpected"}, {"-through", "2026-08-16"}}
	for _, args := range tests {
		var stdout, stderr bytes.Buffer
		code := runWithDependencies(context.Background(), args, fixedNow, &stdout, &stderr, dependencies{})
		if code != 2 || stdout.Len() != 0 {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
	secret := "synthetic-password"
	invalid := proxyFile(t, "http://user:"+secret+"@missing-port\n")
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", invalid}, fixedNow, &stdout, &stderr, dependencies{})
	if code != 2 || strings.Contains(stderr.String(), secret) {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	assertJSONLog(t, stderr.String(), "configuration_failed", "configuration_error")
}

func TestRunHelpReturnsBeforeEnvironmentConfiguration(t *testing.T) {
	deps := dependencies{getenv: func(string) string {
		t.Fatal("environment read during help")
		return ""
	}}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-h"}, fixedNow, &stdout, &stderr, deps)
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "Usage of sync-pathe:") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunTimingEnvironmentAndExplicitOverrideReachPatheClient(t *testing.T) {
	path := proxyFile(t, "127.0.0.1:8080\n")
	tests := []struct {
		name string
		env  string
		args []string
		want time.Duration
	}{
		{name: "environment", env: "7s", want: 7 * time.Second},
		{name: "override", env: "malformed-secret", args: []string{"-timeout", "8s"}, want: 8 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got pathe.ClientConfig
			deps := dependencies{
				getenv: func(name string) string {
					if name != "SYNC_REQUEST_TIMEOUT" {
						t.Fatalf("unexpected environment lookup %q", name)
					}
					return test.env
				},
				newClient: func(config pathe.ClientConfig) (pathe.Getter, error) {
					got = config
					return nil, errors.New("stop after client construction")
				},
			}
			args := append([]string{"-proxy-file", path}, test.args...)
			var stdout, stderr bytes.Buffer
			code := runWithDependencies(context.Background(), args, fixedNow, &stdout, &stderr, deps)
			if code != 2 || got.Timeout != test.want || len(got.Proxies) != 1 || strings.Contains(stderr.String(), test.env) {
				t.Fatalf("code=%d config=%+v stderr=%q", code, got, stderr.String())
			}
		})
	}
}

func TestRunFullPathUsesPatheTargetAndRendersEnrichmentOutcomes(t *testing.T) {
	path := proxyFile(t, "127.0.0.1:8080\n")
	generated := time.Date(2026, 8, 15, 12, 30, 0, 0, time.UTC)
	tests := []struct {
		name       string
		enrichment synccontrol.EnrichmentOutcome
		wantLine   string
	}{
		{name: "skipped", enrichment: synccontrol.EnrichmentOutcome{Status: synccontrol.EnrichmentSkipped}, wantLine: "enrichment=skipped\n"},
		{name: "complete", enrichment: synccontrol.EnrichmentOutcome{Status: synccontrol.EnrichmentComplete, Counts: &synccontrol.EnrichmentCounts{Reused: 1, Matched: 2, ReviewRequired: 3, Unmatched: 4, Failed: 5}}, wantLine: "enrichment=complete reused=1 matched=2 review_required=3 unmatched=4 failed=5\n"},
		{name: "degraded", enrichment: synccontrol.EnrichmentOutcome{Status: synccontrol.EnrichmentDegraded, Counts: &synccontrol.EnrichmentCounts{Failed: 1}}, wantLine: "enrichment=degraded reused=0 matched=0 review_required=0 unmatched=0 failed=1\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			closed := false
			var options synccontrol.ProductionExecutorOptions
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
				newClient: func(pathe.ClientConfig) (pathe.Getter, error) { return fakeGetter{}, nil },
				openDatabase: func(context.Context, string) (databaseServices, func(), error) {
					return databaseServices{writer: fakeWriter{}}, func() { closed = true }, nil
				},
				newTMDB: func(string) (enrichment.Provider, error) { t.Fatal("TMDB called"); return nil, nil },
				newExecutor: func(got synccontrol.ProductionExecutorOptions) (fullExecutor, error) {
					options = got
					if summary, err := options.Enrich(context.Background(), nil); err != nil || summary != nil {
						t.Fatalf("skipped enrichment summary=%+v err=%v", summary, err)
					}
					return fakeExecutor(func(_ context.Context, target synccontrol.Target, window synccontrol.Window) (map[synccontrol.Target]synccontrol.ProviderOutcome, error) {
						if target != synccontrol.TargetPathe || window.From != "2026-08-20" {
							t.Fatalf("target=%s window=%+v", target, window)
						}
						return map[synccontrol.Target]synccontrol.ProviderOutcome{synccontrol.TargetPathe: {Sync: synccontrol.SyncOutcome{Version: 7, Cinemas: 2, Showtimes: 11, GeneratedAt: generated}, Enrichment: test.enrichment}}, nil
					}), nil
				},
			}
			var stdout, stderr bytes.Buffer
			code := runWithDependencies(context.Background(), []string{"-proxy-file", path, "-from", "2026-08-20"}, fixedNow, &stdout, &stderr, deps)
			want := "sync complete provider=pathe version=7 cinemas=2 showtimes=11 generated_at=2026-08-15T12:30:00Z\n" + test.wantLine
			if code != 0 || !closed || options.Writer == nil || options.NewPathe == nil || options.NewUGC != nil || options.NewKinepolis != nil || options.OperationTimeout != 37*time.Second || stdout.String() != want || stderr.Len() != 0 {
				t.Fatalf("code=%d closed=%t options=%+v stdout=%q stderr=%q", code, closed, options, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunRedactsTypedExecutorFailure(t *testing.T) {
	path := proxyFile(t, "127.0.0.1:8080\n")
	secret := "synthetic-provider-secret"
	deps := dependencies{
		getenv: func(name string) string {
			if name == "DATABASE_URL" {
				return "postgres://configured"
			}
			return ""
		},
		newClient: func(pathe.ClientConfig) (pathe.Getter, error) { return fakeGetter{}, nil },
		openDatabase: func(context.Context, string) (databaseServices, func(), error) {
			return databaseServices{writer: fakeWriter{}}, func() {}, nil
		},
		newExecutor: func(synccontrol.ProductionExecutorOptions) (fullExecutor, error) {
			return fakeExecutor(func(context.Context, synccontrol.Target, synccontrol.Window) (map[synccontrol.Target]synccontrol.ProviderOutcome, error) {
				return nil, synccontrol.NewRunError(synccontrol.FailureProviderSync, errors.New(secret))
			}), nil
		},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", path}, fixedNow, &stdout, &stderr, deps)
	if code != 1 || stdout.Len() != 0 || strings.Contains(stderr.String(), secret) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	assertJSONLog(t, stderr.String(), "sync_command_failed", "provider_sync_failed")
}
