package synccontrol

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/noecinemas"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type unusedNoeCinemasGetter struct{}

func (unusedNoeCinemasGetter) Get(context.Context, noecinemas.Operation, string) ([]byte, error) {
	panic("unused")
}
func (unusedNoeCinemasGetter) RequestCount() int { return 27 }
func configureNoeCinemasTestExecutor(t *testing.T, e *ProductionExecutor, w Window) {
	t.Helper()
	e.newNoeCinemas = func() (noecinemas.Getter, error) { return unusedNoeCinemasGetter{}, nil }
	e.syncNoeCinemas = func(context.Context, noecinemas.Getter, noecinemas.SyncOptions) (schedule.Dataset, noecinemas.SyncSummary, error) {
		d := validDataset(t, schedule.ProviderUGC, w)
		d.Provider = schedule.ProviderNoeCinemas
		d.Theaters = []schedule.TheaterRecord{{Provider: schedule.ProviderNoeCinemas, ID: "noecinemas-P8088", ProviderID: "P8088", Slug: "noecinemas-P8088", Name: "Noé Test", Address: "1 rue Test", City: "L'Aigle", PostalCode: "61300", AvailableDates: []string{w.From}, AcceptedPasses: []string{}}}
		r := d.Showtimes[0]
		id := "P8088-" + strings.Repeat("a", 64)
		r.Provider, r.ID, r.ProviderShowingID, r.TheaterID = schedule.ProviderNoeCinemas, "noecinemas-showing-"+id, id, "noecinemas-P8088"
		r.Movie = schedule.MovieRecord{Provider: schedule.ProviderNoeCinemas, ProviderID: "cEvent_1", Slug: "noecinemas-film-cEvent_1", Title: "Event"}
		r.EndTime = r.StartTime
		r.FirstPartDurationMinutes = 0
		r.Language, r.ProviderVersion, r.Room, r.BookingURL = schedule.LanguageVFSTF, "VFSTF", "", "https://achat.cinema-laigle.com/reserver/r/123"
		d.Showtimes = []schedule.ShowtimeRecord{r}
		return d, noecinemas.SyncSummary{Cinemas: 1, Movies: 1, Showtimes: 1, GeneratedAt: d.GeneratedAt}, nil
	}
}
func TestNoeCinemasExecutorAndScheduledManager(t *testing.T) {
	w := Window{From: "2026-08-17"}
	writes, enrichments := 0, 0
	e := &ProductionExecutor{now: time.Now, logger: slog.New(slog.DiscardHandler), operationTimeout: time.Second, writer: writerFunc(func(_ context.Context, d []schedule.Dataset) (int64, error) {
		writes++
		if len(d) != 1 || d[0].Provider != schedule.ProviderNoeCinemas {
			t.Fatal("publication")
		}
		return 1, nil
	}), enrich: func(_ context.Context, m []enrichment.Movie) (*enrichment.Summary, error) {
		enrichments++
		if writes != 1 || len(m) != 1 || m[0].SourceProvider != "noecinemas" || m[0].RuntimeMinutes != 0 {
			t.Fatal("unknown runtime enrichment")
		}
		return nil, errors.New("secret-enrichment")
	}}
	configureNoeCinemasTestExecutor(t, e, w)
	out, err := e.Run(t.Context(), TargetNoeCinemas, w)
	if err != nil || out[TargetNoeCinemas].Sync.Requests != 27 || writes != 1 || enrichments != 1 || out[TargetNoeCinemas].Enrichment.Status != EnrichmentDegraded {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	e.syncNoeCinemas = func(context.Context, noecinemas.Getter, noecinemas.SyncOptions) (schedule.Dataset, noecinemas.SyncSummary, error) {
		return schedule.Dataset{}, noecinemas.SyncSummary{}, &noecinemas.RequestError{Operation: noecinemas.OperationRoom, Kind: syncproxy.FailureChallenge, StatusCode: 403}
	}
	_, err = e.Run(t.Context(), TargetNoeCinemas, w)
	var re *RunError
	if !errors.As(err, &re) || re.Provider != TargetNoeCinemas || writes != 1 || enrichments != 1 || !strings.Contains(strings.Join(re.logs[TargetNoeCinemas], " "), "category=challenge") {
		t.Fatal("failed source published")
	}
	m, err := newTestManager(t.Context(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) { return ProviderOutcome{}, nil }))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, _, err = m.StartScheduled(Occurrence{ScheduleID: 1, Provider: TargetNoeCinemas, Revision: 1, ScheduledFor: time.Now()}); err != nil {
		t.Fatal(err)
	}
	status := waitForTerminal(t, m)
	if status.State != StateSucceeded || status.Providers["noecinemas"].State != ProviderSucceeded || len(status.Providers) != 10 {
		t.Fatal("scheduled state")
	}
}
