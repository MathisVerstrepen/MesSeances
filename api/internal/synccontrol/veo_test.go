package synccontrol

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
	"messeances/api/internal/veo"
)

type unusedVeoGetter struct{}

func (unusedVeoGetter) Get(context.Context, veo.Operation, string) ([]byte, error)  { panic("unused") }
func (unusedVeoGetter) Checkout(context.Context, string, time.Time) ([]byte, error) { panic("unused") }
func (unusedVeoGetter) RequestCount() int                                           { return 29 }
func configureVeoTestExecutor(t *testing.T, e *ProductionExecutor, w Window) {
	t.Helper()
	e.newVeo = func() (veo.Getter, error) { return unusedVeoGetter{}, nil }
	e.syncVeo = func(context.Context, veo.Getter, veo.SyncOptions) (schedule.Dataset, veo.SyncSummary, error) {
		d := validDataset(t, schedule.ProviderUGC, w)
		d.Provider = schedule.ProviderVeo
		d.Theaters = []schedule.TheaterRecord{{Provider: schedule.ProviderVeo, ID: "veo-P1028", ProviderID: "P1028", Slug: "veo-P1028", Name: "Véo fixture", Address: "1 rue", City: "Paris", PostalCode: "75001", AvailableDates: []string{w.From}, AcceptedPasses: []string{}}}
		r := d.Showtimes[0]
		id := schedule.VeoShowingID("P1028", "opaque")
		r.Provider, r.ID, r.ProviderShowingID, r.TheaterID = schedule.ProviderVeo, "veo-showing-"+id, id, "veo-P1028"
		r.Movie = schedule.MovieRecord{Provider: schedule.ProviderVeo, ProviderID: "cEvent_1", Slug: "veo-film-cEvent_1", Title: "Event"}
		r.EndTime = r.StartTime
		r.FirstPartDurationMinutes = 0
		r.Language, r.ProviderVersion, r.Room, r.BookingURL = schedule.LanguageVFSTF, "VFSTF", "", "https://achat.veocinemas.fr/fixture/r/123"
		d.Showtimes = []schedule.ShowtimeRecord{r}
		return d, veo.SyncSummary{Cinemas: 1, Movies: 1, Showtimes: 1, GeneratedAt: d.GeneratedAt}, nil
	}
}
func TestVeoExecutorAndScheduledStatus(t *testing.T) {
	w := Window{From: "2026-08-17"}
	writes, enrichments := 0, 0
	e := &ProductionExecutor{now: time.Now, logger: slog.New(slog.DiscardHandler), operationTimeout: time.Second, writer: writerFunc(func(_ context.Context, d []schedule.Dataset) (int64, error) {
		writes++
		if len(d) != 1 || d[0].Provider != schedule.ProviderVeo {
			t.Fatal("publication")
		}
		return 1, nil
	}), enrich: func(_ context.Context, m []enrichment.Movie) (*enrichment.Summary, error) {
		enrichments++
		if writes != 1 || len(m) != 1 || m[0].SourceProvider != "veo" || m[0].RuntimeMinutes != 0 {
			t.Fatal("enrichment")
		}
		return nil, errors.New("secret")
	}}
	configureVeoTestExecutor(t, e, w)
	out, err := e.Run(t.Context(), TargetVeo, w)
	if err != nil || writes != 1 || enrichments != 1 || out[TargetVeo].Sync.Requests != 29 || out[TargetVeo].Enrichment.Status != EnrichmentDegraded {
		t.Fatal(out, err)
	}
	e.syncVeo = func(context.Context, veo.Getter, veo.SyncOptions) (schedule.Dataset, veo.SyncSummary, error) {
		return schedule.Dataset{}, veo.SyncSummary{}, &veo.RequestError{Operation: veo.OperationCheckout, Kind: syncproxy.FailureChallenge, StatusCode: 403}
	}
	_, err = e.Run(t.Context(), TargetVeo, w)
	var run *RunError
	if !errors.As(err, &run) || writes != 1 || enrichments != 1 || !strings.Contains(strings.Join(run.logs[TargetVeo], " "), "category=challenge") {
		t.Fatal(err)
	}
	m, err := newTestManager(t.Context(), time.Now, executorFunc(func(context.Context, Target, Window) (ProviderOutcome, error) { return ProviderOutcome{}, nil }))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, _, err := m.StartScheduled(Occurrence{ScheduleID: 1, Provider: TargetVeo, Revision: 1, ScheduledFor: time.Now()}); err != nil {
		t.Fatal(err)
	}
	status := waitForTerminal(t, m)
	if status.State != StateSucceeded || status.Providers["veo"].State != ProviderSucceeded || len(status.Providers) != 11 {
		t.Fatal(status)
	}
}
