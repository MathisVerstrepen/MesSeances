package synccontrol

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/grandecran"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type unusedGrandEcranGetter struct{}

func (unusedGrandEcranGetter) Get(context.Context, grandecran.Operation, string) ([]byte, error) {
	panic("unused")
}
func (unusedGrandEcranGetter) RequestCount() int { return 27 }
func configureGrandEcranTestExecutor(t *testing.T, e *ProductionExecutor, window Window) {
	t.Helper()
	e.newGrandEcran = func() (grandecran.Getter, error) { return unusedGrandEcranGetter{}, nil }
	e.syncGrandEcran = func(context.Context, grandecran.Getter, grandecran.SyncOptions) (schedule.Dataset, grandecran.SyncSummary, error) {
		d := validDataset(t, schedule.ProviderUGC, window)
		d.Provider = schedule.ProviderGrandEcran
		d.Theaters = []schedule.TheaterRecord{{Provider: schedule.ProviderGrandEcran, ID: "grandecran-G028P", ProviderID: "G028P", Slug: "grandecran-G028P", Name: "Grand Ecran Test", Address: "1 rue Test", City: "Paris", PostalCode: "75013", AvailableDates: []string{window.From}, AcceptedPasses: []string{}}}
		r := d.Showtimes[0]
		id := "G028P-" + strings.Repeat("a", 64)
		r.Provider, r.ID, r.ProviderShowingID, r.TheaterID = schedule.ProviderGrandEcran, "grandecran-showing-"+id, id, "grandecran-G028P"
		r.Movie = schedule.MovieRecord{Provider: schedule.ProviderGrandEcran, ProviderID: "cEvent_1", Slug: "grandecran-film-cEvent_1", Title: "Event"}
		r.EndTime = r.StartTime
		r.Language, r.ProviderVersion, r.Room, r.BookingURL = schedule.LanguageVOSTFR, "VOSTFR", "", "https://achat.grandecran.fr/test/r/123"
		d.Showtimes = []schedule.ShowtimeRecord{r}
		return d, grandecran.SyncSummary{Cinemas: 1, Movies: 1, Showtimes: 1, Requests: 3, GeneratedAt: d.GeneratedAt}, nil
	}
}
func TestGrandEcranExecutorAndScheduledManager(t *testing.T) {
	window := Window{From: "2026-08-17"}
	writes, enrichments := 0, 0
	e := &ProductionExecutor{now: time.Now, logger: slog.New(slog.DiscardHandler), operationTimeout: time.Second,
		writer: writerFunc(func(_ context.Context, d []schedule.Dataset) (int64, error) {
			writes++
			if len(d) != 1 || d[0].Provider != schedule.ProviderGrandEcran {
				t.Fatal("wrong publication")
			}
			return 1, nil
		}),
		enrich: func(_ context.Context, m []enrichment.Movie) (*enrichment.Summary, error) {
			enrichments++
			if writes != 1 || len(m) != 1 || m[0].SourceProvider != "grandecran" || m[0].ProviderID != "cEvent_1" || m[0].RuntimeMinutes != 0 {
				t.Fatal("source enrichment")
			}
			return nil, errors.New("synthetic-enrichment-secret")
		},
	}
	configureGrandEcranTestExecutor(t, e, window)
	out, err := e.Run(t.Context(), TargetGrandEcran, window)
	if err != nil || out[TargetGrandEcran].Sync.Requests != 27 || writes != 1 || enrichments != 1 || out[TargetGrandEcran].Enrichment.Status != EnrichmentDegraded {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	e.syncGrandEcran = func(context.Context, grandecran.Getter, grandecran.SyncOptions) (schedule.Dataset, grandecran.SyncSummary, error) {
		return schedule.Dataset{}, grandecran.SyncSummary{}, &grandecran.RequestError{Operation: grandecran.OperationRoom, Kind: syncproxy.FailureChallenge, StatusCode: 403}
	}
	_, err = e.Run(t.Context(), TargetGrandEcran, window)
	var re *RunError
	if !errors.As(err, &re) || re.Provider != TargetGrandEcran || re.Stage != StageProviderFetch || writes != 1 || enrichments != 1 || !strings.Contains(strings.Join(re.logs[TargetGrandEcran], " "), "category=challenge") {
		t.Fatalf("failure=%v", err)
	}
	manager, err := newTestManager(t.Context(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) { return ProviderOutcome{}, nil }))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if _, _, err := manager.StartScheduled(Occurrence{ScheduleID: 1, Provider: TargetGrandEcran, Revision: 1, ScheduledFor: time.Now(), Attempt: 0}); err != nil {
		t.Fatal(err)
	}
	status := waitForTerminal(t, manager)
	if status.State != StateSucceeded || status.Providers["grandecran"].State != ProviderSucceeded || len(status.Providers) != 9 {
		t.Fatal("scheduled status")
	}
	status.Providers["grandecran"] = ProviderStatus{State: "mutated"}
	if manager.Status().Providers["grandecran"].State != ProviderSucceeded {
		t.Fatal("status mutation")
	}
}
