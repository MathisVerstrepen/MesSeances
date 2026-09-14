package schedule

import (
	"strings"
	"testing"
	"time"
)

func TestCinevilleIdentityAndURLBoundaries(t *testing.T) {
	for _, id := range []string{"1", "2714300920262", "-693091020261", "9223372036854775807", "-9223372036854775808"} {
		if !ValidCinevilleIdentity("movie", id) {
			t.Fatalf("valid visa %s", id)
		}
	}
	for _, id := range []string{"0", "-0", "+1", "01", "-01", "1.0", "1e3", "9223372036854775808", "-9223372036854775809", " 1", "1 "} {
		if ValidCinevilleIdentity("movie", id) || ValidCinevilleIdentity("theater", id) {
			t.Fatalf("invalid identity %s", id)
		}
	}
	for _, id := range []string{"1-1", "639-149056"} {
		if !ValidCinevilleIdentity("showing", id) {
			t.Fatal("valid scoped id")
		}
	}
	for _, id := range []string{"1", "01-1", "1-01", "1-0", "-1-1", "1--1", "1-1-1", "1-9223372036854775808"} {
		if ValidCinevilleIdentity("showing", id) {
			t.Fatal("invalid scoped id")
		}
	}
	const booking = "https://www.cineville.fr/vad/639/1/9"
	if !ValidCinevilleBookingURL(booking, "639", "639-1") {
		t.Fatal("valid booking")
	}
	for _, raw := range []string{strings.Replace(booking, "https:", "http:", 1), strings.Replace(booking, "www.", "", 1), strings.Replace(booking, "www.", "user@www.", 1), strings.Replace(booking, ".fr/", ".fr:443/", 1), booking + "?", booking + "#", booking + "/", booking + "/../9", strings.Replace(booking, "/1/", "/%31/", 1), strings.Replace(booking, "/639/", "/707/", 1), strings.Replace(booking, "/1/", "/2/", 1), booking + "?x=1", "https://evil.test/vad/639/1/9", "https://www.cineville.fr/vad/639/1/0"} {
		if ValidCinevilleBookingURL(raw, "639", "639-1") {
			t.Fatalf("unsafe booking %s", raw)
		}
	}
	for _, name := range []string{"poster.jpg", "safe-name_123.webp"} {
		if !ValidCinevillePosterURL(CinevillePosterPrefix + name) {
			t.Fatal("valid poster")
		}
	}
	for _, name := range []string{"", "../a.jpg", "a/1.jpg", "a\\1.jpg", "a%2F1.jpg", "a?x", "a#x", "a\n.jpg", "a..jpg", ".jpg"} {
		if ValidCinevillePosterURL(CinevillePosterPrefix + name) {
			t.Fatal("unsafe poster")
		}
	}
	if ValidCinevillePosterURL("https://storage.googleapis.com/other-bucket/images/a.jpg") {
		t.Fatal("bucket widened")
	}
}

func cinevilleValidationDataset() Dataset {
	loc, _ := time.LoadLocation(Timezone)
	start := time.Date(2026, 8, 15, 19, 0, 0, 0, loc)
	r := ShowtimeRecord{Provider: ProviderCineville, ID: "cineville-showing-639-1", ProviderShowingID: "639-1", TheaterID: "cineville-639", ServiceDate: "2026-08-15", Movie: MovieRecord{Provider: ProviderCineville, ProviderID: "-693091020261", Slug: "cineville-film--693091020261", Title: "Event", PublicMovieID: 1}, StartTime: start, EndTime: start, Language: LanguageVF, ProviderVersion: "VF", Format: Format2D, Room: "4", BookingURL: "https://www.cineville.fr/vad/639/1/9"}
	return Dataset{SchemaVersion: SchemaVersion, Provider: ProviderCineville, Scope: ScopeAll, Timezone: Timezone, GeneratedAt: start.UTC(), Window: Window{From: r.ServiceDate, Through: r.ServiceDate}, Showtimes: []ShowtimeRecord{r}, Theaters: []TheaterRecord{{Provider: ProviderCineville, ID: r.TheaterID, ProviderID: "639", Slug: r.TheaterID, Name: "Katorza", City: "Quimper", PostalCode: "29000", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}, PublicMovies: []PublicMovieRecord{{ID: 1, IdentityAnchorProvider: ProviderCineville, IdentityAnchorSourceID: r.Movie.ProviderID, Title: r.Movie.Title, UpdatedAt: start.UTC()}}, MovieSources: []PublicMovieSourceRecord{{Provider: ProviderCineville, SourceMovieID: r.Movie.ProviderID, SourceSlug: r.Movie.Slug, PublicMovieID: 1, Title: r.Movie.Title}}}
}
func TestCinevilleEndRemainsUnknownAfterMaterialization(t *testing.T) {
	for _, runtime := range []int{0, 93} {
		d := cinevilleValidationDataset()
		d.Showtimes[0].Movie.RuntimeMinutes = runtime
		d.MovieSources[0].RuntimeMinutes = runtime
		d.PublicMovies[0].RuntimeMinutes = 120
		d.PublicMovies[0].TMDBRuntimeMinutes = 120
		d.PublicMovies[0].TMDBID = 42
		if err := ValidateDataset(d, true); err != nil {
			t.Fatal(err)
		}
		view := NewSnapshotView(d, SnapshotRevision{})
		r := d.Showtimes[0]
		got := materializeRecord(view, r)
		if !got.EndTime.Equal(got.StartTime) {
			t.Fatal("end manufactured")
		}
		service, err := NewService(testSource{view: view}, ServiceOptions{Now: testServiceNow})
		if err != nil {
			t.Fatal(err)
		}
		slots, err := service.SearchSlot(SlotQuery{TheaterIDs: []string{r.TheaterID}, Date: r.ServiceDate, StartAfter: "18:00", FinishBefore: "23:00", Language: LanguageVF})
		if err != nil || len(slots) != 0 {
			t.Fatal("unknown end offered as fit")
		}
	}
}
func TestCinevilleDatasetRejectsContractViolations(t *testing.T) {
	for _, mutate := range []func(*Dataset){func(d *Dataset) { d.Showtimes[0].EndTime = d.Showtimes[0].StartTime.Add(time.Minute) }, func(d *Dataset) { d.Showtimes[0].FirstPartDurationMinutes = 1 }, func(d *Dataset) { d.Theaters[0].PostalCode = "" }, func(d *Dataset) { d.Theaters[0].City = "" }, func(d *Dataset) {
		d.Showtimes[0].ProviderShowingID = "707-1"
		d.Showtimes[0].ID = "cineville-showing-707-1"
	}, func(d *Dataset) { d.Showtimes[0].Room = "Main" }} {
		d := cinevilleValidationDataset()
		mutate(&d)
		if ValidateDataset(d, true) == nil {
			t.Fatal("contract violation accepted")
		}
	}
}
