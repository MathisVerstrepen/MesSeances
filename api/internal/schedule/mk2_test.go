package schedule

import (
	"strings"
	"testing"
	"time"
)

func mk2ValidationDataset() Dataset {
	d := cinevilleValidationDataset()
	d.Provider = ProviderMK2
	r := &d.Showtimes[0]
	r.Provider, r.ID, r.ProviderShowingID, r.TheaterID = ProviderMK2, "mk2-showing-0004-140350", "0004-140350", "mk2-0004"
	r.Movie = MovieRecord{Provider: ProviderMK2, ProviderID: "HO00006568", Slug: "mk2-film-HO00006568", Title: "Silent", PublicMovieID: 1, PosterURL: MK2PosterPrefix + "HO00006568"}
	r.Language, r.ProviderVersion, r.Room, r.BookingURL = "", "Muet", "", MK2BookingPrefix+"0004&sessionId=140350"
	d.Theaters = []TheaterRecord{{Provider: ProviderMK2, ID: r.TheaterID, ProviderID: "0004", Slug: r.TheaterID, Name: "MK2 Bibliothèque", Address: "128 avenue de France", City: "Paris", PostalCode: "75013", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}
	d.PublicMovies = []PublicMovieRecord{{ID: 1, IdentityAnchorProvider: ProviderMK2, IdentityAnchorSourceID: r.Movie.ProviderID, Title: r.Movie.Title, UpdatedAt: d.GeneratedAt}}
	d.MovieSources = []PublicMovieSourceRecord{{Provider: ProviderMK2, SourceMovieID: r.Movie.ProviderID, SourceSlug: r.Movie.Slug, PublicMovieID: 1, Title: r.Movie.Title}}
	d.MovieAliases = []MovieSlugAliasRecord{{Slug: r.Movie.Slug, Provider: ProviderMK2, SourceMovieID: r.Movie.ProviderID, PublicMovieID: 1, Kind: "source"}}
	return d
}
func TestMK2IdentitiesAndExactURLBytes(t *testing.T) {
	for _, tc := range []struct {
		kind      string
		good, bad []string
	}{
		{"theater", []string{"0004", "1", strings.Repeat("1", 124)}, []string{"", "0", "0000", "+4", " 4", "4x", strings.Repeat("1", 125)}},
		{"movie", []string{"HO00006568", "HO0", "HO" + strings.Repeat("1", 117)}, []string{"HO", "ho1", "HO-1", "HO1?", "HO" + strings.Repeat("1", 118)}},
		{"showing", []string{"0004-140350", "1-" + strings.Repeat("1", 114)}, []string{"0004-01", "0-1", "0004-0", "0004-+1", "0004-1-1", "1-" + strings.Repeat("1", 115)}},
	} {
		for _, v := range tc.good {
			if !ValidMK2Identity(tc.kind, v) {
				t.Fatalf("valid %s %s", tc.kind, v)
			}
		}
		for _, v := range tc.bad {
			if ValidMK2Identity(tc.kind, v) {
				t.Fatalf("invalid %s %s", tc.kind, v)
			}
		}
	}
	booking := MK2BookingPrefix + "0004&sessionId=140350"
	if !ValidMK2BookingURL(booking, "0004", "0004-140350") {
		t.Fatal("valid booking")
	}
	for _, raw := range []string{booking + "#", booking + "&x=1", booking + " ", " " + booking, strings.Replace(booking, "https:", "http:", 1), strings.Replace(booking, "www.", "user@www.", 1), strings.Replace(booking, ".com/", ".com:443/", 1), strings.Replace(booking, "0004", "%30%30%30%34", 1), strings.Replace(booking, "0004", "0005", 1), strings.Replace(booking, "140350", "140351", 1), "https://www.mk2.com/panier/seance/tickets?sessionId=140350&cinemaId=0004"} {
		if ValidMK2BookingURL(raw, "0004", "0004-140350") {
			t.Fatal("unsafe booking", raw)
		}
	}
	poster := MK2PosterPrefix + "HO00006568"
	if !ValidMK2PosterURL(poster) {
		t.Fatal("valid poster")
	}
	for _, raw := range []string{poster + "?", poster + "#", poster + "/", poster + ".jpg", poster + " ", MK2PosterPrefix + "../HO1", MK2PosterPrefix + "%48O1", strings.Replace(poster, ".com/", ".com:443/", 1)} {
		if ValidMK2PosterURL(raw) {
			t.Fatal("unsafe poster", raw)
		}
	}
}
func TestMK2ScopedSilentUnknownEndAndEstimates(t *testing.T) {
	for _, runtime := range []int{0, 93} {
		d := mk2ValidationDataset()
		d.Showtimes[0].Movie.RuntimeMinutes = runtime
		d.MovieSources[0].RuntimeMinutes = runtime
		if err := ValidateDataset(d, true); err != nil {
			t.Fatal(err)
		}
		d.PublicMovies[0].RuntimeMinutes, d.PublicMovies[0].TMDBRuntimeMinutes, d.PublicMovies[0].TMDBID = 120, 120, 42
		got := materializeRecord(NewSnapshotView(d), d.Showtimes[0])
		if got.Language != "" || got.Room != "" || !got.EndTime.Equal(got.StartTime) || got.EstimatedEndTime == nil || !got.EstimatedEndTime.Equal(got.StartTime.Add(135*time.Minute)) {
			t.Fatalf("materialized=%+v", got)
		}
	}
	for _, mutate := range []func(*Dataset){
		func(d *Dataset) { d.Showtimes[0].ProviderVersion = "VF" }, func(d *Dataset) { d.Showtimes[0].Language = "VF" }, func(d *Dataset) { d.Showtimes[0].Room = "1" }, func(d *Dataset) { d.Showtimes[0].EndTime = d.Showtimes[0].StartTime.Add(time.Minute) }, func(d *Dataset) { d.Showtimes[0].FirstPartDurationMinutes = 1 }, func(d *Dataset) { d.Theaters[0].Address = "" }, func(d *Dataset) { d.Theaters[0].PostalCode = "" }, func(d *Dataset) { d.Showtimes[0].BookingURL = MK2BookingPrefix + "0005&sessionId=140350" }, func(d *Dataset) { d.MovieAliases[0].Slug = "mk2-film-HO9" },
	} {
		d := mk2ValidationDataset()
		mutate(&d)
		if ValidateDataset(d, true) == nil {
			t.Fatal("contract violation accepted")
		}
	}
	d := cinevilleValidationDataset()
	d.Showtimes[0].Language, d.Showtimes[0].ProviderVersion = "", "Muet"
	if ValidateDataset(d, true) == nil {
		t.Fatal("silent rule widened to legacy provider")
	}
}
