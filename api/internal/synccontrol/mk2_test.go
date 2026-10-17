package synccontrol

import (
	"context"
	"testing"

	"messeances/api/internal/mk2"
	"messeances/api/internal/schedule"
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
