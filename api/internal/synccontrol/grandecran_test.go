package synccontrol

import (
	"context"
	"strings"
	"testing"

	"messeances/api/internal/grandecran"
	"messeances/api/internal/schedule"
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
