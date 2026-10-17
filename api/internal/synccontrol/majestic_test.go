package synccontrol

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"messeances/api/internal/majestic"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type unusedMajesticGetter struct{}

func (unusedMajesticGetter) Program(context.Context, string) ([]byte, error) { return nil, nil }
func (unusedMajesticGetter) RequestCount() int                               { return 7 }
func configureMajesticTestExecutor(t *testing.T, e *ProductionExecutor, w Window) {
	t.Helper()
	e.newMajestic = func() (majestic.Getter, error) { return unusedMajesticGetter{}, nil }
	e.syncMajestic = func(context.Context, majestic.Getter, majestic.SyncOptions) (schedule.Dataset, majestic.SyncSummary, error) {
		d := validDataset(t, schedule.ProviderUGC, w)
		d.Provider = schedule.ProviderMajestic
		r := &d.Showtimes[0]
		r.Provider = schedule.ProviderMajestic
		r.ID = "majestic-showing-emsx070000000001"
		r.ProviderShowingID = "emsx070000000001"
		r.TheaterID = "majestic-EMS0700"
		r.Movie = schedule.MovieRecord{Provider: schedule.ProviderMajestic, ProviderID: "ABCDE", Slug: "majestic-film-ABCDE", Title: "Majestic fixture", RuntimeMinutes: 100}
		r.FirstPartDurationMinutes = 10
		r.EndTime = r.StartTime.Add(110 * time.Minute)
		r.Room = "Salle source"
		r.BookingURL = "https://www.ticketingcine.com/EMS0700/FRABCDE#showsession?id=emsx070000000001&ps=ticketingcine"
		d.Theaters = []schedule.TheaterRecord{{Provider: schedule.ProviderMajestic, ID: r.TheaterID, Slug: r.TheaterID, ProviderID: "EMS0700", Name: "Cinema", Address: "1 rue", City: "Ville", PostalCode: "60000", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}
		return d, majestic.SyncSummary{Cinemas: 1, Movies: 1, Requests: 7, Showtimes: 1, GeneratedAt: d.GeneratedAt}, nil
	}
}
func TestMajesticExecutorAtomicPublicationAndSafeFailure(t *testing.T) {
	writes := 0
	observer := &captureSyncObserver{}
	e, err := NewProductionExecutor(ProductionExecutorOptions{Writer: writerFunc(func(_ context.Context, d []schedule.Dataset) (int64, error) {
		writes++
		if len(d) != 1 || d[0].Provider != schedule.ProviderMajestic {
			t.Fatal("wrong publication")
		}
		return 4, nil
	}), NewMajestic: func() (majestic.Getter, error) { return unusedMajesticGetter{}, nil }, Now: time.Now, Logger: slog.New(slog.DiscardHandler), Observer: observer, OperationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	w := Window{From: "2026-08-17"}
	configureMajesticTestExecutor(t, e, w)
	out, err := e.Run(t.Context(), TargetMajestic, w)
	if err != nil || writes != 1 || out[TargetMajestic].Sync.Requests != 7 || out[TargetMajestic].Sync.Version != 4 {
		t.Fatal("publication/metrics", out, err)
	}
	e.syncMajestic = func(context.Context, majestic.Getter, majestic.SyncOptions) (schedule.Dataset, majestic.SyncSummary, error) {
		return schedule.Dataset{}, majestic.SyncSummary{}, &majestic.RequestError{Kind: syncproxy.FailureChallenge, StatusCode: 403}
	}
	_, err = e.Run(t.Context(), TargetMajestic, w)
	var re *RunError
	if !errors.As(err, &re) || re.Stage != StageProviderFetch || writes != 1 || len(observer.observations) != 2 || observer.observations[1].provider != "majestic" || observer.observations[1].result != "failed" {
		t.Fatal("failed fetch published or lost telemetry", err)
	}
}
