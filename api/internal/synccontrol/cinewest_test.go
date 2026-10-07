package synccontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/cinewest"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type unusedCinewestFetcher struct{ cinewest.Fetcher }

func (unusedCinewestFetcher) RequestCount() int { return 42 }

func configureCinewestTestExecutor(t *testing.T, e *ProductionExecutor, window Window) {
	t.Helper()
	e.newCinewest = func() (cinewest.Fetcher, error) { return unusedCinewestFetcher{}, nil }
	e.syncCinewest = func(context.Context, cinewest.Fetcher, cinewest.SyncOptions) (schedule.Dataset, cinewest.SyncSummary, error) {
		d := validDataset(t, schedule.ProviderUGC, window)
		d.Provider = schedule.ProviderCinewest
		const theater = "ticketingcine-EMS1378"
		d.Theaters = []schedule.TheaterRecord{{Provider: schedule.ProviderCinewest, ID: "cinewest-" + theater, ProviderID: theater, Slug: "cinewest-" + theater, Name: "LE LIDO", Address: "Place de la Gare", City: "Royan", PostalCode: "17200", AvailableDates: []string{window.From}, AcceptedPasses: []string{}}}
		r := d.Showtimes[0]
		id, _ := schedule.CinewestShowingID(theater, "1")
		r.Provider, r.ID, r.ProviderShowingID, r.TheaterID = schedule.ProviderCinewest, "cinewest-showing-"+id, id, "cinewest-"+theater
		r.Movie = schedule.MovieRecord{Provider: schedule.ProviderCinewest, ProviderID: "ticketingcine-ABCDE", Slug: "cinewest-film-ticketingcine-ABCDE", Title: "Published event", RuntimeMinutes: 90}
		r.FirstPartDurationMinutes = 15
		r.EndTime = r.StartTime.Add(105 * time.Minute)
		r.Language, r.ProviderVersion, r.Room, r.BookingURL = schedule.LanguageVF, "VF", "Salle 1", schedule.CinewestWebsite(theater)
		d.Showtimes = []schedule.ShowtimeRecord{r}
		return d, cinewest.SyncSummary{Cinemas: 1, Movies: 1, Showtimes: 1, Requests: 42, GeneratedAt: d.GeneratedAt}, nil
	}
}

func TestCinewestSkippedCountInAdminOutcomeAndLogs(t *testing.T) {
	window := Window{From: "2026-08-17"}
	var logs bytes.Buffer
	e := &ProductionExecutor{now: time.Now, logger: slog.New(slog.NewTextHandler(&logs, nil)), operationTimeout: time.Second, writer: writerFunc(func(context.Context, []schedule.Dataset) (int64, error) {
		return 1, nil
	})}
	configureCinewestTestExecutor(t, e, window)
	syncFixture := e.syncCinewest
	e.syncCinewest = func(ctx context.Context, f cinewest.Fetcher, opts cinewest.SyncOptions) (schedule.Dataset, cinewest.SyncSummary, error) {
		d, summary, err := syncFixture(ctx, f, opts)
		summary.Skipped = 2
		return d, summary, err
	}
	manager, err := newTestManager(t.Context(), time.Now, e)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	status := manager.executeProvider(TargetCinewest, window)
	if status.State != ProviderSucceeded || status.Outcome == nil || status.Outcome.Sync.Skipped != 2 || status.Outcome.Sync.Showtimes != 1 {
		t.Fatal("skipped count missing from admin provider outcome")
	}
	body, err := json.Marshal(status)
	if err != nil || !bytes.Contains(body, []byte(`"skipped":2`)) {
		t.Fatal("skipped count missing from admin JSON", err)
	}
	if !strings.Contains(logs.String(), "result=succeeded") || !strings.Contains(logs.String(), "skipped=2") {
		t.Fatal("skipped count missing from operational completion log")
	}
	// Existing publication failure logs must retain fetched omission counts too.
	e.writer = writerFunc(func(context.Context, []schedule.Dataset) (int64, error) {
		return 0, errors.New("synthetic publication failure")
	})
	_, err = e.Run(t.Context(), TargetCinewest, window)
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.Stage != StagePublication {
		t.Fatal("publication failure not reported", err)
	}
	joined := strings.Join(runErr.logs[TargetCinewest], "\n")
	if !strings.Contains(joined, "event=provider_failed") || !strings.Contains(joined, "skipped=2") {
		t.Fatal("skipped count missing from admin failure log")
	}
}

func TestCinewestExecutorAndScheduledStatus(t *testing.T) {
	window := Window{From: "2026-08-17"}
	writes := 0
	e := &ProductionExecutor{now: time.Now, logger: slog.New(slog.DiscardHandler), operationTimeout: time.Second, writer: writerFunc(func(_ context.Context, d []schedule.Dataset) (int64, error) {
		writes++
		if len(d) != 1 || d[0].Provider != schedule.ProviderCinewest {
			t.Fatal("wrong publication")
		}
		return 1, nil
	})}
	configureCinewestTestExecutor(t, e, window)
	out, err := e.Run(t.Context(), TargetCinewest, window)
	if err != nil || writes != 1 || out[TargetCinewest].Sync.Requests != 42 {
		t.Fatal("Cinewest execution", err)
	}
	e.syncCinewest = func(context.Context, cinewest.Fetcher, cinewest.SyncOptions) (schedule.Dataset, cinewest.SyncSummary, error) {
		return schedule.Dataset{}, cinewest.SyncSummary{}, &cinewest.RequestError{Operation: "program", Kind: syncproxy.FailureChallenge, StatusCode: 403}
	}
	_, err = e.Run(t.Context(), TargetCinewest, window)
	var re *RunError
	if !errors.As(err, &re) || re.Provider != TargetCinewest || re.Stage != StageProviderFetch || writes != 1 {
		t.Fatal("partial publication", err)
	}
	e.newCinewest = func() (cinewest.Fetcher, error) { return nil, errors.New("synthetic secret") }
	if _, err := e.Run(t.Context(), TargetCinewest, window); err == nil || writes != 1 {
		t.Fatal("client failure")
	}
	manager, err := newTestManager(t.Context(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) { return ProviderOutcome{}, nil }))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if _, _, err := manager.StartScheduled(Occurrence{ScheduleID: 1, Provider: TargetCinewest, Revision: 1, ScheduledFor: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	status := waitForTerminal(t, manager)
	if status.State != StateSucceeded || len(status.Providers) != 10 || status.Providers["cinewest"].State != ProviderSucceeded || status.Occurrence.Provider != TargetCinewest {
		t.Fatal("scheduled status")
	}
	status.Providers["cinewest"] = ProviderStatus{State: "mutated"}
	if manager.Status().Providers["cinewest"].State != ProviderSucceeded {
		t.Fatal("status alias")
	}
}
