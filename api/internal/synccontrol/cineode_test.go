package synccontrol

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"messeances/api/internal/cineode"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type unusedCineodeGetter struct{}

func (unusedCineodeGetter) Page(context.Context, string, string) ([]byte, error) { return nil, nil }
func (unusedCineodeGetter) RequestCount() int                                    { return 39 }

func configureCineodeTestExecutor(t *testing.T, e *ProductionExecutor, w Window) {
	t.Helper()
	e.newCineode = func() (cineode.Getter, error) { return unusedCineodeGetter{}, nil }
	e.syncCineode = func(ctx context.Context, _ cineode.Getter, o cineode.SyncOptions) (schedule.Dataset, cineode.SyncSummary, error) {
		if _, ok := ctx.Deadline(); !ok || o.From != w.From || o.Now.IsZero() {
			t.Error("missing operation context/options")
		}
		d := validDataset(t, schedule.ProviderUGC, w)
		d.Provider = schedule.ProviderCineode
		d.Coverage = nil
		r := &d.Showtimes[0]
		r.Provider = schedule.ProviderCineode
		r.TheaterID = "cineode-991"
		r.Movie = schedule.MovieRecord{Provider: schedule.ProviderCineode, ProviderID: "atelier-local", Slug: "cineode-film-atelier-local", Title: "Cineode fixture"}
		r.ProviderShowingID = "991-atelier-local-" + strconv.FormatInt(r.StartTime.Unix(), 10)
		r.ID = "cineode-showing-" + r.ProviderShowingID
		r.EndTime = r.StartTime
		r.FirstPartDurationMinutes = 0
		r.Room = ""
		r.Language = schedule.LanguageVF
		r.ProviderVersion = "VF"
		r.BookingURL = schedule.CineodeWebsite("991")
		d.Theaters = []schedule.TheaterRecord{{Provider: schedule.ProviderCineode, ID: r.TheaterID, Slug: r.TheaterID, ProviderID: "991", Name: "Le Don Camillo", City: "Montmirail", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}
		return d, cineode.SyncSummary{Cinemas: 1, Movies: 1, Requests: 39, Showtimes: 1, GeneratedAt: d.GeneratedAt}, nil
	}
}

func TestCineodeExecutorAtomicPublicationAndSafeFailure(t *testing.T) {
	writes := 0
	observer := &captureSyncObserver{}
	e, err := NewProductionExecutor(ProductionExecutorOptions{Writer: writerFunc(func(_ context.Context, d []schedule.Dataset) (int64, error) {
		writes++
		if len(d) != 1 || d[0].Provider != schedule.ProviderCineode {
			t.Fatal("wrong publication")
		}
		return 4, nil
	}), NewCineode: func() (cineode.Getter, error) { return unusedCineodeGetter{}, nil }, Now: time.Now, Logger: slog.New(slog.DiscardHandler), Observer: observer, OperationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	w := Window{From: "2026-08-17"}
	configureCineodeTestExecutor(t, e, w)
	out, err := e.Run(t.Context(), TargetCineode, w)
	if err != nil || writes != 1 || out[TargetCineode].Sync.Requests != 39 || out[TargetCineode].Sync.Version != 4 {
		t.Fatal("publication/metrics", out, err)
	}
	e.syncCineode = func(context.Context, cineode.Getter, cineode.SyncOptions) (schedule.Dataset, cineode.SyncSummary, error) {
		return schedule.Dataset{}, cineode.SyncSummary{}, &cineode.RequestError{Kind: syncproxy.FailureChallenge, StatusCode: 403}
	}
	_, err = e.Run(t.Context(), TargetCineode, w)
	var re *RunError
	if !errors.As(err, &re) || re.Stage != StageProviderFetch || writes != 1 || len(observer.observations) != 2 || observer.observations[1].provider != "cineode" || observer.observations[1].result != "failed" {
		t.Fatal("failed acquisition published or lost telemetry", err)
	}
}
