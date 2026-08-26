package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/geocoding"
)

type fakeStore struct{}

func (fakeStore) Select(context.Context, geocoding.Filters) ([]geocoding.Theater, error) {
	return nil, nil
}
func (fakeStore) Save(context.Context, *geocoding.Location, geocoding.Location) (bool, error) {
	return true, nil
}

type fakeProvider struct{}

func (fakeProvider) Search(context.Context, geocoding.Query) ([]geocoding.Candidate, error) {
	return nil, nil
}

func testNow() time.Time { return time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC) }

func TestRunHelpAndInvalidFlagsAvoidEnvironmentAndDatabase(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"-limit", "-1"}, {"-provider", "other"}, {"-theater-id="}, {"extra"}, {"-timeout", "4s"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			deps := dependencies{
				loadDotEnv: func() error { t.Fatal("dotenv loaded"); return nil },
				getenv:     func(string) string { t.Fatal("environment read"); return "" },
				openStore: func(context.Context, string) (geocoding.Store, func(), error) {
					t.Fatal("database opened")
					return nil, nil, nil
				},
			}
			code := runWithDependencies(context.Background(), args, testNow, &stdout, &stderr, deps)
			if code != 2 || stdout.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunPassesOptionsPrintsSummaryAndClosesStore(t *testing.T) {
	closed := false
	var got geocoding.RunOptions
	var gotTimeout time.Duration
	deps := dependencies{
		loadDotEnv: func() error { return nil },
		getenv: func(name string) string {
			if name == "DATABASE_URL" {
				return "postgres://configured"
			}
			return ""
		},
		openStore: func(context.Context, string) (geocoding.Store, func(), error) {
			return fakeStore{}, func() { closed = true }, nil
		},
		newProvider: func(timeout time.Duration) (geocoding.Provider, error) {
			gotTimeout = timeout
			return fakeProvider{}, nil
		},
		execute: func(_ context.Context, _ geocoding.Store, _ geocoding.Provider, options geocoding.RunOptions, _ func() time.Time) (geocoding.Summary, error) {
			got = options
			return geocoding.Summary{DryRun: true, Selected: 3, Skipped: 4, Matched: 1, Ambiguous: 1, NotFound: 1, Written: 0}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-dry-run", "-provider", "ugc", "-theater-id", "ugc-25", "-limit", "3", "-retry-ambiguous", "-timeout", "7s"}, testNow, &stdout, &stderr, deps)
	want := "geocode complete dry_run=true selected=3 skipped=4 matched=1 ambiguous=1 not_found=1 failed=0 written=0\n"
	if code != 0 || !closed || gotTimeout != 7*time.Second || got.Filters.Provider != "ugc" || got.Filters.TheaterID != "ugc-25" || got.Limit != 3 || !got.RetryAmbiguous || !got.DryRun || stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("code=%d closed=%t timeout=%s options=%+v stdout=%q stderr=%q", code, closed, gotTimeout, got, stdout.String(), stderr.String())
	}
}

func TestRunFailuresAreRedactedAndPartialSummaryIsPrinted(t *testing.T) {
	secret := "synthetic-secret"
	base := dependencies{loadDotEnv: func() error { return nil }, getenv: func(name string) string {
		if name == "DATABASE_URL" {
			return "postgres://configured"
		}
		return ""
	}, newProvider: func(time.Duration) (geocoding.Provider, error) { return fakeProvider{}, nil }, execute: func(context.Context, geocoding.Store, geocoding.Provider, geocoding.RunOptions, func() time.Time) (geocoding.Summary, error) {
		return geocoding.Summary{}, nil
	}}
	t.Run("startup", func(t *testing.T) {
		deps := base
		deps.openStore = func(context.Context, string) (geocoding.Store, func(), error) { return nil, nil, errors.New(secret) }
		var stdout, stderr bytes.Buffer
		if code := runWithDependencies(context.Background(), nil, testNow, &stdout, &stderr, deps); code != 1 || stdout.Len() != 0 || strings.Contains(stderr.String(), secret) {
			t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
		assertLog(t, stderr.String(), "database_startup_failed")
	})
	t.Run("partial", func(t *testing.T) {
		deps := base
		deps.openStore = func(context.Context, string) (geocoding.Store, func(), error) { return fakeStore{}, func() {}, nil }
		deps.execute = func(context.Context, geocoding.Store, geocoding.Provider, geocoding.RunOptions, func() time.Time) (geocoding.Summary, error) {
			return geocoding.Summary{Selected: 2, Failed: 1, Written: 1}, errors.New(secret)
		}
		var stdout, stderr bytes.Buffer
		if code := runWithDependencies(context.Background(), nil, testNow, &stdout, &stderr, deps); code != 1 || !strings.Contains(stdout.String(), "failed=1 written=1") || strings.Contains(stderr.String(), secret) {
			t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
		assertLog(t, stderr.String(), "geocoding_failed")
	})
}

func assertLog(t *testing.T, raw, code string) {
	t.Helper()
	var value map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &value) != nil || value["component"] != "geocode_theaters" || value["error_code"] != code {
		t.Fatalf("log=%q", raw)
	}
}
