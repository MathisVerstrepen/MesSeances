package schedule

import (
	"strings"
	"testing"
	"time"
)

func cinewestValidationDataset(platform string) Dataset {
	d := mk2ValidationDataset()
	d.Provider = ProviderCinewest
	id, movieID, raw := "cineoffice-royanlelido", "cineoffice-1", "1"
	if platform == "ticketingcine" {
		id, movieID, raw = "ticketingcine-EMS1185", "ticketingcine-ABCDE", "emsx118500000001"
	}
	if platform == "webediamovies" {
		id, movieID = "webediamovies-W8400", "webediamovies-1"
	}
	r := &d.Showtimes[0]
	r.ProviderShowingID, _ = CinewestShowingID(id, raw)
	r.ID, r.Provider, r.TheaterID = "cinewest-showing-"+r.ProviderShowingID, ProviderCinewest, "cinewest-"+id
	r.StartTime = time.Date(2026, 9, 14, 20, 0, 0, 123456000, time.FixedZone("CEST", 7200))
	r.EndTime = r.StartTime
	r.ServiceDate = "2026-09-14"
	r.Movie = MovieRecord{Provider: ProviderCinewest, ProviderID: movieID, Slug: "cinewest-film-" + movieID, Title: "Event", PublicMovieID: 1}
	r.Language, r.ProviderVersion, r.Room, r.BookingURL = LanguageVF, "VF", "Salle 1", CinewestWebsite(id)
	if platform == "cineoffice" {
		r.EndTime = r.StartTime.Add(187 * time.Minute)
		r.Language, r.ProviderVersion = "", "VERSION_MUET"
	}
	if platform == "ticketingcine" {
		r.FirstPartDurationMinutes = 10
	}
	d.Window = Window{From: r.ServiceDate, Through: r.ServiceDate}
	d.Theaters = []TheaterRecord{{Provider: ProviderCinewest, ID: r.TheaterID, ProviderID: id, Slug: r.TheaterID, Name: "Cinema", Address: "1 Rue", City: "Royan", PostalCode: "17200", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}
	d.PublicMovies = []PublicMovieRecord{{ID: 1, IdentityAnchorProvider: ProviderCinewest, IdentityAnchorSourceID: movieID, Title: r.Movie.Title, UpdatedAt: d.GeneratedAt}}
	d.MovieSources = []PublicMovieSourceRecord{{Provider: ProviderCinewest, SourceMovieID: movieID, SourceSlug: r.Movie.Slug, PublicMovieID: 1, Title: r.Movie.Title}}
	d.MovieAliases = []MovieSlugAliasRecord{{Slug: r.Movie.Slug, Provider: ProviderCinewest, SourceMovieID: movieID, PublicMovieID: 1, Kind: "source"}}
	return d
}
func TestCinewestEndMaterializationAndValidation(t *testing.T) {
	for _, platform := range []string{"cineoffice", "ticketingcine", "webediamovies"} {
		t.Run(platform, func(t *testing.T) {
			d := cinewestValidationDataset(platform)
			if err := ValidateDataset(d, true); err != nil {
				t.Fatal(err)
			}
			without := materializeRecord(NewSnapshotView(d), d.Showtimes[0])
			if without.EstimatedEndTime != nil {
				t.Fatal("estimate without runtime")
			}
			d.PublicMovies[0].RuntimeMinutes, d.PublicMovies[0].TMDBRuntimeMinutes, d.PublicMovies[0].TMDBID = 120, 120, 42
			got := materializeRecord(NewSnapshotView(d), d.Showtimes[0])
			switch platform {
			case "cineoffice":
				if !got.EndTime.Equal(d.Showtimes[0].EndTime) || got.EstimatedEndTime != nil {
					t.Fatal("published end replaced")
				}
			case "ticketingcine":
				if !got.EndTime.Equal(got.StartTime.Add(130*time.Minute)) || got.EstimatedEndTime != nil {
					t.Fatal("ticketing fallback")
				}
			case "webediamovies":
				if !got.EndTime.Equal(got.StartTime) || got.EstimatedEndTime == nil || !got.EstimatedEndTime.Equal(got.StartTime.Add(135*time.Minute)) {
					t.Fatal("Capitole canonical/estimate")
				}
			}
		})
	}
	for _, change := range []func(*Dataset){func(d *Dataset) { d.Showtimes[0].EndTime = d.Showtimes[0].StartTime }, func(d *Dataset) { d.Showtimes[0].FirstPartDurationMinutes = 1 }, func(d *Dataset) { d.Showtimes[0].ProviderVersion = "VF" }, func(d *Dataset) { d.Showtimes[0].Language = "VF" }, func(d *Dataset) { d.Showtimes[0].Room = "" }, func(d *Dataset) { d.MovieAliases[0].Slug = "cinewest-film-cineoffice-2" }, func(d *Dataset) { d.Theaters[0].City = "" }} {
		d := cinewestValidationDataset("cineoffice")
		change(&d)
		if ValidateDataset(d, true) == nil {
			t.Fatal("invalid office contract accepted")
		}
	}
}
func TestCinewestIdentitiesAndURLParity(t *testing.T) {
	for _, id := range CinewestTheaterIDs() {
		if !ValidCinewestIdentity("theater", id) || !ValidCinewestBookingURL(CinewestWebsite(id), id, "") {
			t.Fatal("manifest")
		}
	}
	for _, id := range []string{"cineoffice-1", "webediamovies-1", "ticketingcine-ABCDE", "ticketingcine-EMS0042-emsx0042HC12", "cineoffice-" + strings.Repeat("1", 103)} {
		if !ValidCinewestIdentity("movie", id) {
			t.Fatal("movie identity", id)
		}
	}
	for _, id := range []string{"1", "cineoffice-0", "cineoffice-01", "ticketingcine-EMS0042-emsx1185HC12", "ticketingcine-EMS0001-emsx0001HC1", "cineoffice-" + strings.Repeat("1", 104)} {
		if ValidCinewestIdentity("movie", id) {
			t.Fatal("invalid identity")
		}
	}
	a, _ := CinewestShowingID("cineoffice-royanlelido", "1")
	b, _ := CinewestShowingID("cineoffice-vitreaurore", "1")
	if a == b {
		t.Fatal("cinema collision")
	}
	for _, raw := range []string{"", "a\x00b", strings.Repeat("1", 2049)} {
		if _, ok := CinewestShowingID("cineoffice-royanlelido", raw); ok {
			t.Fatal("raw bound")
		}
	}
	ticket := "https://www.etoilecinemas-bethune.fr/#showsession?id=emsx118500000001"
	id, _ := CinewestShowingID("ticketingcine-EMS1185", "emsx118500000001")
	if !ValidCinewestBookingURL(ticket, "ticketingcine-EMS1185", id) || !ValidCinewestBookingURL("https://www.capitolestudios-reserver.cotecine.fr/reserver/r/244471", "webediamovies-W8400", "") {
		t.Fatal("booking")
	}
	for _, raw := range []string{ticket + "&token=x", strings.Replace(ticket, "1185", "1317", 1), strings.Replace(ticket, "https:", "http:", 1), strings.Replace(ticket, ".fr/", ".fr:443/", 1), strings.Replace(ticket, "www.", "user@www.", 1), "https://ws.ticketingcine.com/site", "https://www.cine-royan.com/?api_token=x", "https://www.cine-royan.com/#"} {
		if ValidCinewestBookingURL(raw, "ticketingcine-EMS1185", id) {
			t.Fatal("unsafe booking")
		}
	}
	for _, poster := range []string{"https://cinemedia.cine.digital/medias/1/29/290f220e-8ab4-4ed2-afd5-141fcb6dd229.jpeg", "https://images.monnaie-services.com/movie_poster/600/FRABCDE/ABCDEFGH.webp", "https://all.web.img.acsta.net/img/6f/af/6faf7d9aa879bd9374e773f31db44956.jpg"} {
		if !ValidCinewestPosterURL(poster) {
			t.Fatal("safe poster")
		}
		for _, bad := range []string{poster + "?", poster + "#", poster + "?api_token=x", strings.Replace(poster, "https:", "http:", 1), strings.Replace(poster, "https://", "https://user@", 1)} {
			if ValidCinewestPosterURL(bad) {
				t.Fatal("unsafe poster")
			}
		}
	}
}
