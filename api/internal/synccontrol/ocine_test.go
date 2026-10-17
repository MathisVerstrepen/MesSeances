package synccontrol

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"messeances/api/internal/ocine"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type unusedOcineGetter struct{}

func (unusedOcineGetter) Page(context.Context, string, string) ([]byte, error) { return nil, nil }
func (unusedOcineGetter) Checkout(context.Context, string) ([]byte, error)     { return nil, nil }
func (unusedOcineGetter) RequestCount() int                                    { return 27 }
func configureOcineTestExecutor(t *testing.T, e *ProductionExecutor, w Window) {
	t.Helper()
	e.newOcine = func() (ocine.Getter, error) { return unusedOcineGetter{}, nil }
	e.syncOcine = func(ctx context.Context, _ ocine.Getter, o ocine.SyncOptions) (schedule.Dataset, ocine.SyncSummary, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) < 24*time.Minute || o.From != w.From || o.Now.IsZero() {
			t.Error("missing provider-specific crawl bound/options")
		}
		d := validDataset(t, schedule.ProviderUGC, w)
		d.Provider = schedule.ProviderOcine
		d.Coverage = nil
		r := &d.Showtimes[0]
		r.Provider = d.Provider
		r.TheaterID = "ocine-309"
		r.Movie = schedule.MovieRecord{Provider: d.Provider, ProviderID: "atelier-local", Slug: "ocine-film-atelier-local", Title: "Ociné fixture"}
		r.ProviderShowingID = "309-atelier-local-" + strconv.FormatInt(r.StartTime.Unix(), 10) + "-past"
		r.ID = "ocine-showing-" + r.ProviderShowingID
		r.EndTime = r.StartTime
		r.FirstPartDurationMinutes = 0
		r.Room = ""
		r.Language = ""
		r.ProviderVersion = ""
		r.BookingURL = ""
		d.Theaters = []schedule.TheaterRecord{{Provider: d.Provider, ID: r.TheaterID, Slug: r.TheaterID, ProviderID: "309", Name: "Ociné Maubeuge", City: "Maubeuge", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}
		if err := schedule.ValidateDataset(d, true); err != nil {
			t.Fatal("invalid Ociné fixture", err)
		}
		return d, ocine.SyncSummary{Cinemas: 1, Movies: 1, Requests: 27, Showtimes: 1, GeneratedAt: d.GeneratedAt}, nil
	}
}
func TestOcineExecutorAtomicPublicationAndSafeFailure(t *testing.T) {
	writes := 0
	e, err := NewProductionExecutor(ProductionExecutorOptions{Writer: writerFunc(func(_ context.Context, d []schedule.Dataset) (int64, error) {
		writes++
		if len(d) != 1 || d[0].Provider != schedule.ProviderOcine {
			t.Fatal("wrong publication")
		}
		return 4, nil
	}), NewOcine: func() (ocine.Getter, error) { return unusedOcineGetter{}, nil }, Now: time.Now, Logger: slog.New(slog.DiscardHandler), OperationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	w := Window{From: "2026-08-17"}
	configureOcineTestExecutor(t, e, w)
	out, err := e.Run(t.Context(), TargetOcine, w)
	if err != nil || writes != 1 || out[TargetOcine].Sync.Requests != 27 || out[TargetOcine].Sync.Version != 4 {
		t.Fatal("publication/metrics", out, err)
	}
	e.syncOcine = func(context.Context, ocine.Getter, ocine.SyncOptions) (schedule.Dataset, ocine.SyncSummary, error) {
		return schedule.Dataset{}, ocine.SyncSummary{}, &ocine.RequestError{Kind: syncproxy.FailureChallenge, StatusCode: 403}
	}
	_, err = e.Run(t.Context(), TargetOcine, w)
	var re *RunError
	if !errors.As(err, &re) || re.Stage != StageProviderFetch || writes != 1 {
		t.Fatal("failed acquisition published", err)
	}
}
