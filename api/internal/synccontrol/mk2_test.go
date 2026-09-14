package synccontrol

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/mk2"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type unusedMK2Fetcher struct{}

func (unusedMK2Fetcher) FetchCinemas(context.Context) ([]byte, error)         { panic("unused") }
func (unusedMK2Fetcher) FetchFilms(context.Context) ([]byte, error)           { panic("unused") }
func (unusedMK2Fetcher) FetchComplex(context.Context, string) ([]byte, error) { panic("unused") }
func (unusedMK2Fetcher) RequestCount() int                                    { return 27 }
func configureMK2TestExecutor(t *testing.T, e *ProductionExecutor, window Window) {
	t.Helper()
	e.newMK2 = func() (mk2.Fetcher, error) { return unusedMK2Fetcher{}, nil }
	e.syncMK2 = func(context.Context, mk2.Fetcher, mk2.SyncOptions) (schedule.Dataset, mk2.SyncSummary, error) {
		d := validDataset(t, schedule.ProviderUGC, window)
		d.Provider = schedule.ProviderMK2
		d.Theaters = []schedule.TheaterRecord{{Provider: schedule.ProviderMK2, ID: "mk2-0004", ProviderID: "0004", Slug: "mk2-0004", Name: "MK2 Bibliothèque", Address: "128 avenue de France", City: "Paris", PostalCode: "75013", AvailableDates: []string{window.From}, AcceptedPasses: []string{}}}
		r := d.Showtimes[0]
		r.Provider, r.ID, r.ProviderShowingID, r.TheaterID = schedule.ProviderMK2, "mk2-showing-0004-140350", "0004-140350", "mk2-0004"
		r.Movie = schedule.MovieRecord{Provider: schedule.ProviderMK2, ProviderID: "HO00006568", Slug: "mk2-film-HO00006568", Title: "Silent"}
		r.EndTime = r.StartTime
		r.Language, r.ProviderVersion, r.Room, r.BookingURL = "", "Muet", "", schedule.MK2BookingPrefix+"0004&sessionId=140350"
		d.Showtimes = []schedule.ShowtimeRecord{r}
		return d, mk2.SyncSummary{Cinemas: 1, Movies: 1, Showtimes: 1, Requests: 3, GeneratedAt: d.GeneratedAt}, nil
	}
}
func TestMK2ExecutorMetricsFailuresAndNonfatalEnrichment(t *testing.T) {
	window := Window{From: "2026-08-17"}
	writes, enrichments := 0, 0
	e := &ProductionExecutor{now: time.Now, logger: slog.New(slog.DiscardHandler), operationTimeout: time.Second,
		writer: writerFunc(func(_ context.Context, d []schedule.Dataset) (int64, error) {
			writes++
			if len(d) != 1 || d[0].Provider != schedule.ProviderMK2 {
				t.Fatal("wrong publication")
			}
			return 1, nil
		}),
		enrich: func(_ context.Context, m []enrichment.Movie) (*enrichment.Summary, error) {
			enrichments++
			if len(m) != 1 || m[0].SourceProvider != "mk2" || m[0].ProviderID != "HO00006568" || m[0].RuntimeMinutes != 0 {
				t.Fatal("source enrichment")
			}
			return nil, errors.New("synthetic-enrichment-secret")
		},
	}
	configureMK2TestExecutor(t, e, window)
	out, err := e.Run(t.Context(), TargetMK2, window)
	if err != nil || out[TargetMK2].Sync.Requests != 27 || writes != 1 || enrichments != 1 || out[TargetMK2].Enrichment.Status != EnrichmentDegraded {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	e.syncMK2 = func(context.Context, mk2.Fetcher, mk2.SyncOptions) (schedule.Dataset, mk2.SyncSummary, error) {
		return schedule.Dataset{}, mk2.SyncSummary{}, &mk2.RequestError{Operation: mk2.OperationComplex, Kind: syncproxy.FailureChallenge, StatusCode: 403}
	}
	_, err = e.Run(t.Context(), TargetMK2, window)
	var re *RunError
	if !errors.As(err, &re) || re.Provider != TargetMK2 || re.Stage != StageProviderFetch || writes != 1 || enrichments != 1 {
		t.Fatalf("fetch failure=%v", err)
	}
	configureMK2TestExecutor(t, e, window)
	e.writer = writerFunc(func(context.Context, []schedule.Dataset) (int64, error) { return 0, errors.New("publication-secret") })
	if _, err = e.Run(t.Context(), TargetMK2, window); err == nil || strings.Contains(err.Error(), "secret") || enrichments != 1 {
		t.Fatal("publication failure", err)
	}
}
func TestMK2ManagerScheduledStatus(t *testing.T) {
	manager, err := newTestManager(t.Context(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) { return ProviderOutcome{}, nil }))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if _, _, err := manager.StartScheduled(Occurrence{ScheduleID: 1, Provider: TargetMK2, Revision: 1, ScheduledFor: time.Now().UTC(), Attempt: 0}); err != nil {
		t.Fatal(err)
	}
	status := waitForTerminal(t, manager)
	if status.State != StateSucceeded || status.Providers["mk2"].State != ProviderSucceeded || status.Providers["ugc"].State != ProviderNotRequested || len(status.Providers) != 8 || status.Occurrence == nil || status.Occurrence.Provider != TargetMK2 {
		t.Fatalf("status=%+v", status)
	}
	status.Providers["mk2"] = ProviderStatus{State: "mutated"}
	if manager.Status().Providers["mk2"].State != ProviderSucceeded {
		t.Fatal("status mutation")
	}
}
