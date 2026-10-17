package synccontrol

import (
	"context"
	"strings"
	"testing"

	"messeances/api/internal/noecinemas"
	"messeances/api/internal/schedule"
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
