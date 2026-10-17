package synccontrol

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"messeances/api/internal/cinemovida"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type unusedCinemovidaGetter struct{}

func (unusedCinemovidaGetter) Get(context.Context, cinemovida.Operation, string) ([]byte, error) {
	return nil, nil
}
func (unusedCinemovidaGetter) Checkout(context.Context, string, time.Time) ([]byte, error) {
	return nil, nil
}
func (unusedCinemovidaGetter) RequestCount() int { return 42 }

func configureCinemovidaTestExecutor(t *testing.T, e *ProductionExecutor, w Window) {
	t.Helper()
	e.newCinemovida = func() (cinemovida.Getter, error) { return unusedCinemovidaGetter{}, nil }
	e.syncCinemovida = func(ctx context.Context, _ cinemovida.Getter, o cinemovida.SyncOptions) (schedule.Dataset, cinemovida.SyncSummary, error) {
		if _, ok := ctx.Deadline(); !ok || o.From != w.From || o.Now.IsZero() {
			t.Error("missing operation context/options")
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 24*time.Minute {
			t.Error("short operation timeout truncates source-end acquisition")
		}
		d := validDataset(t, schedule.ProviderUGC, w)
		d.Provider = schedule.ProviderCinemovida
		d.Coverage = nil
		r := &d.Showtimes[0]
		r.Provider = schedule.ProviderCinemovida
		r.TheaterID = "cinemovida-P0176"
		r.Movie = schedule.MovieRecord{Provider: schedule.ProviderCinemovida, ProviderID: "cEvent_1", Slug: "cinemovida-film-cEvent_1", Title: "Ciné-Movida fixture"}
		r.ProviderShowingID = schedule.CinemovidaShowingID("P0176", "same session")
		r.ID = "cinemovida-showing-" + r.ProviderShowingID
		r.EndTime = r.StartTime.Add(45 * time.Minute)
		r.FirstPartDurationMinutes = 0
		r.Room = "Salle 10 PREMIUM MX4D"
		r.Language = schedule.LanguageVF
		r.ProviderVersion = "VF"
		r.BookingURL = "https://achat.cinema-castillet.com/reserver/r/123"
		d.Theaters = []schedule.TheaterRecord{{Provider: schedule.ProviderCinemovida, ID: r.TheaterID, Slug: r.TheaterID, ProviderID: "P0176", Name: "Castillet", Address: "1 rue", City: "Perpignan", PostalCode: "66000", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}
		return d, cinemovida.SyncSummary{Cinemas: 1, Movies: 1, Requests: 42, Showtimes: 1, GeneratedAt: d.GeneratedAt, EndsAttempted: 1, EndsRecovered: 1}, nil
	}
}

func TestCinemovidaExecutorPublicationUnknownRuntimeAndSafeFailure(t *testing.T) {
	writes, enrichments := 0, 0
	observer := &captureSyncObserver{}
	e, err := NewProductionExecutor(ProductionExecutorOptions{Writer: writerFunc(func(_ context.Context, d []schedule.Dataset) (int64, error) {
		writes++
		if len(d) != 1 || d[0].Provider != schedule.ProviderCinemovida || !d[0].Showtimes[0].EndTime.After(d[0].Showtimes[0].StartTime) {
			t.Fatal("source end lost before commit")
		}
		return 4, nil
	}), NewCinemovida: func() (cinemovida.Getter, error) { return unusedCinemovidaGetter{}, nil }, Now: time.Now, Logger: slog.New(slog.DiscardHandler), Observer: observer, OperationTimeout: time.Second, Enrich: func(_ context.Context, movies []enrichment.Movie) (*enrichment.Summary, error) {
		enrichments++
		if writes != 1 || len(movies) != 1 || movies[0].SourceProvider != "cinemovida" || movies[0].RuntimeMinutes != 0 {
			t.Fatal("unknown runtime not enriched after commit")
		}
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	w := Window{From: "2026-08-17"}
	configureCinemovidaTestExecutor(t, e, w)
	out, err := e.Run(context.Background(), TargetCinemovida, w)
	if err != nil || writes != 1 || enrichments != 1 || out[TargetCinemovida].Sync.Requests != 42 || out[TargetCinemovida].Sync.Version != 4 {
		t.Fatal("publication/metrics", out, err)
	}
	e.syncCinemovida = func(context.Context, cinemovida.Getter, cinemovida.SyncOptions) (schedule.Dataset, cinemovida.SyncSummary, error) {
		return schedule.Dataset{}, cinemovida.SyncSummary{}, &cinemovida.RequestError{Operation: cinemovida.OperationCheckout, Kind: syncproxy.FailureChallenge, StatusCode: 403}
	}
	_, err = e.Run(t.Context(), TargetCinemovida, w)
	var re *RunError
	if !errors.As(err, &re) || re.Stage != StageProviderFetch || failureDetails(TargetCinemovida, StageProviderFetch, err, SyncOutcome{}).Category != categoryChallenge || writes != 1 || enrichments != 1 || len(observer.observations) != 2 || observer.observations[1].provider != "cinemovida" || observer.observations[1].result != "failed" {
		t.Fatal("failed acquisition published or lost telemetry", err)
	}
	parent, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	wantDeadline, _ := parent.Deadline()
	e.syncCinemovida = func(ctx context.Context, _ cinemovida.Getter, _ cinemovida.SyncOptions) (schedule.Dataset, cinemovida.SyncSummary, error) {
		if got, ok := ctx.Deadline(); !ok || !got.Equal(wantDeadline) {
			t.Fatal("crawl budget overrides parent deadline")
		}
		cancel()
		return schedule.Dataset{}, cinemovida.SyncSummary{}, ctx.Err()
	}
	if _, err := e.Run(parent, TargetCinemovida, w); err == nil || writes != 1 {
		t.Fatal("parent cancellation lost or published")
	}
}
