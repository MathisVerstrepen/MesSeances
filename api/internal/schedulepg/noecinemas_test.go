package schedulepg

import (
	"messeances/api/internal/schedule"
	"strings"
)

func noecinemasTestDataset() Dataset {
	d := grandecranTestDataset()
	d.Provider = schedule.ProviderNoeCinemas
	r := &d.Showtimes[0]
	id := "P8088-" + strings.Repeat("a", 64)
	r.Provider, r.ID, r.ProviderShowingID, r.TheaterID = schedule.ProviderNoeCinemas, "noecinemas-showing-"+id, id, "noecinemas-P8088"
	r.Movie = MovieRecord{Provider: schedule.ProviderNoeCinemas, ProviderID: "200", Slug: "noecinemas-film-200", Title: "Noé independent film"}
	r.Language, r.ProviderVersion, r.BookingURL = schedule.LanguageVFSTF, "VFSTF", "https://achat.cinema-laigle.com/reserver/r/123"
	d.Theaters = []TheaterRecord{{Provider: schedule.ProviderNoeCinemas, ID: r.TheaterID, ProviderID: "P8088", Slug: r.TheaterID, Name: "Noé Test", Address: "1 rue Test", City: "L'Aigle", PostalCode: "61300", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}
	return d
}
