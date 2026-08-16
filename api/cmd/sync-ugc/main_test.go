package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"movieflow/api/internal/schedule"
	"movieflow/api/internal/ugc"
)

type fakeWriter struct {
	calls   int
	version int64
	err     error
}

func (w *fakeWriter) Replace(context.Context, schedule.Dataset) (int64, error) {
	w.calls++
	return w.version, w.err
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
	}, openDatabase: func(context.Context, string) (schedule.SnapshotWriter, func(), error) {
		databaseCalled = true
		return nil, nil, nil
	}}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", path, "-from", "2026-08-15", "-through", "2026-08-15"}, &stdout, &stderr, fixedNow, deps)
	if code != 2 || syncCalled || databaseCalled || !strings.Contains(stderr.String(), "DATABASE_URL is required") {
		t.Fatalf("code=%d sync=%v db=%v stderr=%q", code, syncCalled, databaseCalled, stderr.String())
	}
}

func TestRunDiagnosticNeverTouchesDatabase(t *testing.T) {
	path := proxyFile(t, "http://127.0.0.1:8080\n")
	deps := dependencies{getenv: func(string) string { t.Fatal("environment lookup called"); return "" }, sync: fakeSync, openDatabase: func(context.Context, string) (schedule.SnapshotWriter, func(), error) {
		t.Fatal("database opener called")
		return nil, nil, nil
	}}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", path, "-cinema-id", "25", "-from", "2026-08-15", "-through", "2026-08-15"}, &stdout, &stderr, fixedNow, deps)
	if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "mode=single_cinema persisted=false") || strings.Contains(stdout.String(), "version=") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunCompletePersistsExactlyOnceAndCloses(t *testing.T) {
	path := proxyFile(t, "http://127.0.0.1:8080\n")
	writer := &fakeWriter{version: 7}
	closed := false
	deps := dependencies{getenv: func(string) string { return "postgres://configured" }, sync: fakeSync, openDatabase: func(context.Context, string) (schedule.SnapshotWriter, func(), error) {
		return writer, func() { closed = true }, nil
	}}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", path, "-from", "2026-08-15", "-through", "2026-08-15"}, &stdout, &stderr, fixedNow, deps)
	if code != 0 || writer.calls != 1 || !closed || stderr.Len() != 0 || !strings.Contains(stdout.String(), "mode=all_cinemas persisted=true version=7") {
		t.Fatalf("code=%d calls=%d closed=%v stdout=%q stderr=%q", code, writer.calls, closed, stdout.String(), stderr.String())
	}
}

func TestRunDatabaseErrorIsRedacted(t *testing.T) {
	path := proxyFile(t, "http://127.0.0.1:8080\n")
	secret := "synthetic-password"
	deps := dependencies{getenv: func(string) string { return "postgres://user:" + secret + "@bad" }, sync: fakeSync, openDatabase: func(context.Context, string) (schedule.SnapshotWriter, func(), error) {
		return nil, nil, errors.New("parse " + secret)
	}}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"-proxy-file", path, "-from", "2026-08-15", "-through", "2026-08-15"}, &stdout, &stderr, fixedNow, deps)
	if code != 1 || strings.Contains(stdout.String()+stderr.String(), secret) || !strings.Contains(stderr.String(), "database startup failed") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
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
	if code != 2 || !strings.Contains(stderr.String(), "proxy file unavailable") || strings.Contains(stderr.String(), "date window") {
		t.Fatalf("14-day code=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = runWithDependencies(context.Background(), []string{"-proxy-file", missing, "-from", "2026-10-18", "-through", "2026-11-01"}, &stdout, &stderr, fixedNow, dependencies{})
	if code != 2 || !strings.Contains(stderr.String(), "date window") || strings.Contains(stderr.String(), "proxy file") {
		t.Fatalf("15-day code=%d stderr=%q", code, stderr.String())
	}
}
