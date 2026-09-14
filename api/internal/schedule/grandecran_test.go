package schedule

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGrandEcranSharedBookingCorpus(t *testing.T) {
	body, err := os.ReadFile("../../../web/tests/fixtures/grandecran-booking-urls.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Valid   []string `json:"valid"`
		Invalid []string `json:"invalid"`
	}
	if err := json.Unmarshal(body, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Valid) == 0 || len(corpus.Invalid) == 0 {
		t.Fatal("shared booking corpus must contain valid and invalid cases")
	}
	for _, group := range []struct {
		name string
		urls []string
		want bool
	}{{"valid", corpus.Valid, true}, {"invalid", corpus.Invalid, false}} {
		for i, raw := range group.urls {
			t.Run(group.name+"/"+strconv.Itoa(i), func(t *testing.T) {
				if got := ValidGrandEcranBookingURL(raw); got != group.want {
					t.Fatalf("ValidGrandEcranBookingURL(%q) = %t, want %t", raw, got, group.want)
				}
			})
		}
	}
	for _, size := range []int{4096, 4097} {
		t.Run("bytes/"+strconv.Itoa(size), func(t *testing.T) {
			const prefix = "https://achat.grandecran.fr/test/r/"
			raw := prefix + strings.Repeat("1", size-len(prefix))
			if len(raw) != size {
				t.Fatal("incorrect boundary fixture size")
			}
			if got, want := ValidGrandEcranBookingURL(raw), size == 4096; got != want {
				t.Fatalf("%d-byte booking accepted = %t, want %t", size, got, want)
			}
		})
	}
}

func grandecranValidationDataset() Dataset {
	d := cinevilleValidationDataset()
	d.Provider = ProviderGrandEcran
	r := &d.Showtimes[0]
	id := "G028P-" + strings.Repeat("a", 64)
	r.Provider, r.ID, r.ProviderShowingID, r.TheaterID = ProviderGrandEcran, "grandecran-showing-"+id, id, "grandecran-G028P"
	r.Movie = MovieRecord{Provider: ProviderGrandEcran, ProviderID: "cEvent_1", Slug: "grandecran-film-cEvent_1", Title: "Event", PublicMovieID: 1}
	r.Language, r.ProviderVersion, r.Room, r.BookingURL = LanguageVOSTFR, "VOSTFR", "", "https://achat.grandecran.fr/test/r/123"
	d.Theaters = []TheaterRecord{{Provider: ProviderGrandEcran, ID: r.TheaterID, ProviderID: "G028P", Slug: r.TheaterID, Name: "Grand Ecran Test", Address: "1 rue Test", City: "Paris", PostalCode: "75001", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}
	d.PublicMovies = []PublicMovieRecord{{ID: 1, IdentityAnchorProvider: ProviderGrandEcran, IdentityAnchorSourceID: r.Movie.ProviderID, Title: r.Movie.Title, UpdatedAt: d.GeneratedAt}}
	d.MovieSources = []PublicMovieSourceRecord{{Provider: ProviderGrandEcran, SourceMovieID: r.Movie.ProviderID, SourceSlug: r.Movie.Slug, PublicMovieID: 1, Title: r.Movie.Title}}
	d.MovieAliases = []MovieSlugAliasRecord{{Slug: r.Movie.Slug, Provider: ProviderGrandEcran, SourceMovieID: r.Movie.ProviderID, PublicMovieID: 1, Kind: "source"}}
	return d
}
func TestGrandEcranIdentityBookingPosterPolicy(t *testing.T) {
	for _, tc := range []struct {
		kind      string
		good, bad []string
	}{
		{"theater", []string{"G028P", "P9488"}, []string{"g028p", "P948", "P94888", "G028P\n"}},
		{"movie", []string{"1", "cEvent_1-x", strings.Repeat("1", 112), "c" + strings.Repeat("a", 111)}, []string{"", "0", "01", "c", "Cevent", "-1", "c/a", "1\n", strings.Repeat("1", 113), "c" + strings.Repeat("a", 112)}},
		{"showing", []string{"G028P-" + strings.Repeat("a", 64)}, []string{"G028P-" + strings.Repeat("A", 64), "G028P-" + strings.Repeat("a", 63)}},
	} {
		for _, id := range tc.good {
			if !ValidGrandEcranIdentity(tc.kind, id) {
				t.Errorf("valid %s %s", tc.kind, id)
			}
		}
		for _, id := range tc.bad {
			if ValidGrandEcranIdentity(tc.kind, id) {
				t.Errorf("invalid %s %s", tc.kind, id)
			}
		}
	}
	booking := "https://achat.grandecran.fr/test/r/123"
	if !ValidGrandEcranBookingURL(booking) {
		t.Fatal("valid booking")
	}
	for _, raw := range []string{booking + "?", booking + "#", booking + "/", booking + " ", " " + booking, booking + "\n", strings.Replace(booking, "https:", "http:", 1), strings.Replace(booking, "achat.grandecran.fr", "ACHAT.GRANDECRAN.FR", 1), strings.Replace(booking, "achat.grandecran.fr", "achat.grandecran.fr.evil.test", 1), strings.Replace(booking, "achat.", "user@achat.", 1), strings.Replace(booking, ".fr/", ".fr:443/", 1), strings.Replace(booking, "test/", "test//", 1), strings.Replace(booking, "test/", "../", 1), strings.Replace(booking, "test/", "%74est/", 1), strings.Replace(booking, "123", "0123", 1), strings.Replace(booking, "test/", "test--x/", 1), strings.Replace(booking, "test/", "test\\x/", 1)} {
		if ValidGrandEcranBookingURL(raw) {
			t.Error("unsafe booking", raw)
		}
	}
	if !ValidGrandEcranPosterURL("https://fr.web.img6.acsta.net/test.jpg") {
		t.Fatal("valid poster")
	}
	for _, raw := range []string{"https://acsta.net.evil.test/test.jpg", "https://acsta.net/test.jpg?", "https://acsta.net/../x", "https://acsta.net/%2e/x", "https://acsta.net:443/x", "https://user@acsta.net/x", "https://acsta.net/x#", "https://acsta.net/x y"} {
		if ValidGrandEcranPosterURL(raw) {
			t.Error("unsafe poster", raw)
		}
	}
}
func TestGrandEcranSourceEndsAndResponseOnlyEstimates(t *testing.T) {
	for _, runtime := range []int{0, 93} {
		d := grandecranValidationDataset()
		d.Showtimes[0].Movie.RuntimeMinutes = runtime
		d.MovieSources[0].RuntimeMinutes = runtime
		if err := ValidateDataset(d, true); err != nil {
			t.Fatal(err)
		}
		d.PublicMovies[0].RuntimeMinutes, d.PublicMovies[0].TMDBRuntimeMinutes, d.PublicMovies[0].TMDBID = 120, 120, 42
		got := materializeRecord(NewSnapshotView(d), d.Showtimes[0])
		if got.Room != "" || !got.EndTime.Equal(got.StartTime) || got.EstimatedEndTime == nil || !got.EstimatedEndTime.Equal(got.StartTime.Add(135*time.Minute)) {
			t.Fatalf("materialized=%+v", got)
		}
	}
	for _, mutate := range []func(*Dataset){func(d *Dataset) { d.Showtimes[0].EndTime = d.Showtimes[0].StartTime.Add(time.Minute) }, func(d *Dataset) { d.Showtimes[0].FirstPartDurationMinutes = 1 }, func(d *Dataset) {
		d.Showtimes[0].ProviderShowingID = "P9488-" + strings.Repeat("a", 64)
		d.Showtimes[0].ID = "grandecran-showing-" + d.Showtimes[0].ProviderShowingID
	}, func(d *Dataset) { d.MovieAliases[0].Slug = "grandecran-film-1" }, func(d *Dataset) { d.Showtimes[0].Movie.RuntimeMinutes = -1 }} {
		d := grandecranValidationDataset()
		mutate(&d)
		if ValidateDataset(d, true) == nil {
			t.Fatal("contract violation accepted")
		}
	}
}
