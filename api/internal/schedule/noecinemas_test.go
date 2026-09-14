package schedule

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNoeCinemasSharedBookingCorpus(t *testing.T) {
	b, err := os.ReadFile("../../../web/tests/fixtures/noecinemas-booking-urls.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct{ Valid, Invalid []string }
	if err = json.Unmarshal(b, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Valid) < 24 || len(corpus.Invalid) == 0 {
		t.Fatal("incomplete shared corpus")
	}
	for _, group := range []struct {
		name string
		urls []string
		want bool
	}{{"valid", corpus.Valid, true}, {"invalid", corpus.Invalid, false}} {
		for i, raw := range group.urls {
			t.Run(group.name+strconv.Itoa(i), func(t *testing.T) {
				if ValidNoeCinemasBookingURL(raw) != group.want {
					t.Fatal("booking corpus mismatch")
				}
			})
		}
	}
	const prefix = "https://achat.cinema-laigle.com/reserver/r/"
	for _, size := range []int{4096, 4097} {
		if ValidNoeCinemasBookingURL(prefix+strings.Repeat("1", size-len(prefix))) != (size == 4096) {
			t.Fatal("URL byte bound")
		}
	}
	id := "P8088-" + strings.Repeat("a", 64)
	if !ValidBookingURL(ProviderNoeCinemas, prefix+"123", id, "P8088") {
		t.Fatal("valid context")
	}
	for _, tc := range []struct {
		provider         Provider
		showing, theater string
	}{{ProviderGrandEcran, id, "P8088"}, {ProviderNoeCinemas, id, "B0181"}, {ProviderNoeCinemas, "B0181-" + strings.Repeat("a", 64), "P8088"}} {
		if ValidBookingURL(tc.provider, prefix+"123", tc.showing, tc.theater) {
			t.Fatal("context mismatch")
		}
	}
	if ValidNoeCinemasBookingURL(prefix+"123", "W8391") {
		t.Fatal("invented empty cinema policy")
	}
}
func noecinemasValidationDataset() Dataset {
	d := grandecranValidationDataset()
	d.Provider = ProviderNoeCinemas
	r := &d.Showtimes[0]
	id := "P8088-" + strings.Repeat("a", 64)
	r.Provider, r.ID, r.ProviderShowingID, r.TheaterID = ProviderNoeCinemas, "noecinemas-showing-"+id, id, "noecinemas-P8088"
	r.Movie = MovieRecord{Provider: ProviderNoeCinemas, ProviderID: "cEvent_1", Slug: "noecinemas-film-cEvent_1", Title: "Event", PublicMovieID: 1}
	r.Language, r.ProviderVersion, r.BookingURL = LanguageVFSTF, "VFSTF", "https://achat.cinema-laigle.com/reserver/r/123"
	d.Theaters = []TheaterRecord{{Provider: ProviderNoeCinemas, ID: r.TheaterID, ProviderID: "P8088", Slug: r.TheaterID, Name: "Noé Test", Address: "1 rue Test", City: "L'Aigle", PostalCode: "61300", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}
	d.PublicMovies = []PublicMovieRecord{{ID: 1, IdentityAnchorProvider: ProviderNoeCinemas, IdentityAnchorSourceID: r.Movie.ProviderID, Title: r.Movie.Title, UpdatedAt: d.GeneratedAt}}
	d.MovieSources = []PublicMovieSourceRecord{{Provider: ProviderNoeCinemas, SourceMovieID: r.Movie.ProviderID, SourceSlug: r.Movie.Slug, PublicMovieID: 1, Title: r.Movie.Title}}
	d.MovieAliases = []MovieSlugAliasRecord{{Slug: r.Movie.Slug, Provider: ProviderNoeCinemas, SourceMovieID: r.Movie.ProviderID, PublicMovieID: 1, Kind: "source"}}
	return d
}
func TestNoeCinemasIdentityMetadataAndEnds(t *testing.T) {
	for _, kind := range []string{"theater", "movie", "showing"} {
		for _, id := range []string{"P8088", "p8088", "1", "01", "cEvent_1", strings.Repeat("1", 112), strings.Repeat("1", 113), "P8088-" + strings.Repeat("a", 64), "P8088\n"} {
			if ValidNoeCinemasIdentity(kind, id) != ValidGrandEcranIdentity(kind, id) {
				t.Fatal("identity grammar")
			}
		}
	}
	for _, tc := range []struct {
		raw, want string
		valid     bool
	}{{"", "", true}, {"https://all.web.img.acsta.net/x.jpg", "https://all.web.img.acsta.net/x.jpg", true}, {"https://unsupported.test/x.jpg", "", true}, {"https://acsta.net/../x", "", false}, {"https://acsta.net/x?", "", false}} {
		got, valid := NoeCinemasSourcePosterURL(tc.raw)
		if got != tc.want || valid != tc.valid {
			t.Fatal("poster policy")
		}
	}
	for _, runtime := range []int{0, 93} {
		d := noecinemasValidationDataset()
		d.Showtimes[0].Movie.RuntimeMinutes = runtime
		d.MovieSources[0].RuntimeMinutes = runtime
		if err := ValidateDataset(d, true); err != nil {
			t.Fatal(err)
		}
	}
	for _, mutate := range []func(*Dataset){func(d *Dataset) { d.Showtimes[0].EndTime = d.Showtimes[0].StartTime.Add(time.Minute) }, func(d *Dataset) { d.Showtimes[0].FirstPartDurationMinutes = 1 }, func(d *Dataset) { d.MovieAliases[0].Slug = "noecinemas-film-1" }, func(d *Dataset) { d.MovieSources[0].SourceSlug = "noecinemas-film-1" }, func(d *Dataset) { d.Showtimes[0].Movie.RuntimeMinutes = -1 }, func(d *Dataset) {
		d.Showtimes[0].ProviderShowingID = "B0181-" + strings.Repeat("a", 64)
		d.Showtimes[0].ID = "noecinemas-showing-" + d.Showtimes[0].ProviderShowingID
	}} {
		d := noecinemasValidationDataset()
		mutate(&d)
		if ValidateDataset(d, true) == nil {
			t.Fatal("invalid dataset")
		}
	}
}
