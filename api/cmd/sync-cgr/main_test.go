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

	"messeances/api/internal/cgr"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/schedule"
	"messeances/api/internal/synccontrol"
)

func fixedNow() time.Time { return time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC) }

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
	if entry["msg"] != event || entry["error_code"] != code || entry["component"] != "sync_cgr" {
		t.Fatalf("log=%+v", entry)
	}
}

type fakeGetter struct{}

func (fakeGetter) Get(context.Context, cgr.Operation, string) ([]byte, error) { return nil, nil }
func (fakeGetter) RequestCount() int                                          { return 0 }

type fakeWriter struct{}

func (fakeWriter) Replace(context.Context, []schedule.Dataset) (schedule.PublicationResult, error) {
	return schedule.PublicationResult{}, nil
}

type fakeExecutor func(context.Context, synccontrol.Target, synccontrol.Window) (map[synccontrol.Target]synccontrol.ProviderOutcome, error)

func (f fakeExecutor) Run(ctx context.Context, target synccontrol.Target, window synccontrol.Window) (map[synccontrol.Target]synccontrol.ProviderOutcome, error) {
	return f(ctx, target, window)
}

func TestRunRejectsInvalidFlagsAndConfiguration(t *testing.T) {
	for _, args := range [][]string{{"-from", "invalid"}, {"unexpected"}, {"-through", "2026-08-26"}} {
		var stdout, stderr bytes.Buffer
		code := runWithDependencies(context.Background(), args, fixedNow, &stdout, &stderr, dependencies{})
		if code != 2 || stdout.Len() != 0 {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}

	secret := "synthetic-secret-duration"
	deps := dependencies{getenv: func(name string) string {
		if name != "SYNC_REQUEST_TIMEOUT" {
			t.Fatalf("unexpected environment lookup %q", name)
		}
		return secret
	}}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), nil, fixedNow, &stdout, &stderr, deps)
	if code != 2 || stdout.Len() != 0 || strings.Contains(stderr.String(), secret) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
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
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "Usage of sync-cgr:") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunOptionalProxyFileAndTimingReachCGRClient(t *testing.T) {
	path := proxyFile(t, "127.0.0.1:8080\n")
	tests := []struct {
		name        string
		environment string
		args        []string
		wantTimeout time.Duration
		wantProxies int
	}{
		{name: "direct", environment: "7s", wantTimeout: 7 * time.Second},
		{name: "proxy and override", environment: "synthetic-secret", args: []string{"-proxy-file", path, "-timeout", "8s"}, wantTimeout: 8 * time.Second, wantProxies: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got cgr.ClientConfig
			deps := dependencies{
				getenv: func(name string) string {
					if name != "SYNC_REQUEST_TIMEOUT" {
						t.Fatalf("unexpected environment lookup %q", name)
					}
					return test.environment
				},
				newClient: func(config cgr.ClientConfig) (cgr.Getter, error) {
					got = config
					return nil, errors.New("stop after client construction")
				},
			}
			var stdout, stderr bytes.Buffer
			code := runWithDependencies(context.Background(), test.args, fixedNow, &stdout, &stderr, deps)
			if code != 2 || got.Timeout != test.wantTimeout || len(got.Proxies) != test.wantProxies || strings.Contains(stderr.String(), test.environment) {
				t.Fatalf("code=%d config=%+v stdout=%q stderr=%q", code, got, stdout.String(), stderr.String())
			}
			assertJSONLog(t, stderr.String(), "configuration_failed", "configuration_error")
		})
	}

	secret := "synthetic-proxy-password"
	invalid := proxyFile(t, "http://user:"+secret+"@missing-port\n")
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", invalid}, fixedNow, &stdout, &stderr, dependencies{})
	if code != 2 || stdout.Len() != 0 || strings.Contains(stderr.String(), secret) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	assertJSONLog(t, stderr.String(), "configuration_failed", "configuration_error")
}

func TestRunFullPathUsesCGRTargetAndRendersOutput(t *testing.T) {
	generated := time.Date(2026, 8, 25, 12, 30, 0, 0, time.UTC)
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
		newClient: func(cgr.ClientConfig) (cgr.Getter, error) { return fakeGetter{}, nil },
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
				if target != synccontrol.TargetCGR || window.From != "2026-08-26" {
					t.Fatalf("target=%s window=%+v", target, window)
				}
				return map[synccontrol.Target]synccontrol.ProviderOutcome{synccontrol.TargetCGR: {Sync: synccontrol.SyncOutcome{Version: 7, Cinemas: 73, Showtimes: 12811, GeneratedAt: generated}, Enrichment: synccontrol.EnrichmentOutcome{Status: synccontrol.EnrichmentComplete, Counts: &synccontrol.EnrichmentCounts{Reused: 1, Matched: 2, ReviewRequired: 3, Unmatched: 4, Failed: 5}}}}, nil
			}), nil
		},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-from", "2026-08-26"}, fixedNow, &stdout, &stderr, deps)
	want := "sync complete provider=cgr version=7 cinemas=73 showtimes=12811 generated_at=2026-08-25T12:30:00Z\n" +
		"enrichment=complete reused=1 matched=2 review_required=3 unmatched=4 failed=5\n"
	if code != 0 || !closed || options.Writer == nil || options.NewCGR == nil || options.NewUGC != nil || options.NewKinepolis != nil || options.NewPathe != nil || options.OperationTimeout != 37*time.Second || stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("code=%d closed=%t options=%+v stdout=%q stderr=%q", code, closed, options, stdout.String(), stderr.String())
	}
}

func TestRunRedactsTypedExecutorFailure(t *testing.T) {
	secret := "synthetic-provider-secret"
	deps := dependencies{
		getenv: func(name string) string {
			if name == "DATABASE_URL" {
				return "postgres://configured"
			}
			return ""
		},
		newClient: func(cgr.ClientConfig) (cgr.Getter, error) { return fakeGetter{}, nil },
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
	code := runWithDependencies(context.Background(), nil, fixedNow, &stdout, &stderr, deps)
	if code != 1 || stdout.Len() != 0 || strings.Contains(stderr.String(), secret) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	assertJSONLog(t, stderr.String(), "sync_command_failed", "provider_sync_failed")
}
