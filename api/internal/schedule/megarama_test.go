package schedule

import (
	"strings"
	"testing"
	"time"
)

func TestMegaramaURLAndIdentityBoundaries(t *testing.T) {
	const id = "emsx056500000001"
	for _, url := range []string{"https://bordeaux.megarama.fr", "https://bordeaux.megarama.fr/", "https://bordeaux.megarama.fr#showsession?id=" + id, "https://bordeaux.megarama.fr/#showsession?id=" + id} {
		if !ValidMegaramaBookingURL(url, "EMS0565", id) {
			t.Errorf("valid URL rejected: %s", url)
		}
	}
	for _, url := range []string{"http://bordeaux.megarama.fr/", "https://bordeaux.megarama.fr.evil.test/", "https://user@bordeaux.megarama.fr/", "https://bordeaux.megarama.fr:443/", "https://bordeaux.megarama.fr/?x=1", "https://bordeaux.megarama.fr/%2e%2e/", "https://bordeaux.megarama.fr/#showsession?id=emsx056500000002", "https://boulogne.megarama.fr/", "https://bordeaux.megarama.fr/#showsession?id=" + id + "&x=1"} {
		if ValidMegaramaBookingURL(url, "EMS0565", id) {
			t.Error("unsafe URL accepted")
		}
	}
	for cinema, host := range megaramaBookingHosts {
		showing := "emsx" + cinema[3:] + "00000001"
		if !ValidMegaramaBookingURL("https://"+host+"#showsession?id="+showing, cinema, showing) || ValidMegaramaBookingURL("https://"+host, cinema, "") || ValidMegaramaBookingURL("https://"+host+"#showsession?id="+id, "EMS0565", id) {
			t.Fatal("booking alias scope")
		}
	}
	for _, id := range []string{"ABCDE", "EMS0565-emsx0565HC12"} {
		if !validMegaramaIdentity("movie", id) {
			t.Fatal("valid source ID")
		}
	}
	for _, id := range []string{"abcde", "EMS0565-emsx1315HC12", "EMS0565-emsx0565HC" + strings.Repeat("1", 115)} {
		if validMegaramaIdentity("movie", id) {
			t.Fatal("invalid source ID")
		}
	}
}

func TestMegaramaEffectiveEndSourceTMDBAndUnknown(t *testing.T) {
	location, _ := time.LoadLocation(Timezone)
	start := time.Date(2026, 8, 15, 19, 0, 0, 0, location)
	for _, test := range []struct {
		source, tmdb, first, display int
		tmdbID                       int64
		want                         int
	}{
		{100, 120, 10, 999, 42, 110}, {0, 120, 10, 999, 42, 130}, {0, 120, 0, 999, 42, 120}, {0, 0, 10, 999, 42, 0}, {0, 120, 10, 999, 0, 0},
	} {
		r := ShowtimeRecord{Provider: ProviderMegarama, ID: "megarama-showing-emsx056500000001", ProviderShowingID: "emsx056500000001", TheaterID: "megarama-EMS0565", ServiceDate: "2026-08-15", Movie: MovieRecord{Provider: ProviderMegarama, ProviderID: "ABCDE", Slug: "megarama-film-ABCDE", Title: "Film", RuntimeMinutes: test.source, PublicMovieID: 1}, StartTime: start, FirstPartDurationMinutes: test.first, Language: LanguageVFSTF, ProviderVersion: "VFSTF", Format: Format2D, Room: "1", BookingURL: "https://bordeaux.megarama.fr/"}
		r.EndTime, _ = MegaramaEnd(start, test.source, test.first)
		d := Dataset{SchemaVersion: SchemaVersion, Provider: ProviderMegarama, Scope: ScopeAll, Timezone: Timezone, GeneratedAt: start.UTC(), Window: Window{From: "2026-08-15", Through: "2026-08-15"}, Showtimes: []ShowtimeRecord{r}, Theaters: []TheaterRecord{{Provider: ProviderMegarama, ID: r.TheaterID, ProviderID: "EMS0565", Slug: r.TheaterID, Name: "Megarama", City: "Bordeaux", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{}}}, PublicMovies: []PublicMovieRecord{{ID: 1, IdentityAnchorProvider: ProviderMegarama, IdentityAnchorSourceID: "ABCDE", Title: "Film", RuntimeMinutes: test.display, TMDBID: test.tmdbID, TMDBRuntimeMinutes: test.tmdb, UpdatedAt: start.UTC()}}, MovieSources: []PublicMovieSourceRecord{{Provider: ProviderMegarama, SourceMovieID: "ABCDE", PublicMovieID: 1, SourceSlug: "megarama-film-ABCDE", Title: "Film", RuntimeMinutes: test.source}}}
		d.Theaters[0].Address, d.Theaters[0].PostalCode = "1 rue", "33000"
		if err := ValidateDataset(d, true); err != nil {
			t.Fatal(err)
		}
		view := NewSnapshotView(d, SnapshotRevision{})
		got := materializeRecord(view, r)
		if got.EndTime.Sub(got.StartTime) != time.Duration(test.want)*time.Minute {
			t.Fatal("runtime precedence or first part")
		}
		service, err := NewService(testSource{view: view}, ServiceOptions{Now: testServiceNow})
		if err != nil {
			t.Fatal(err)
		}
		slots, err := service.SearchSlot(SlotQuery{TheaterIDs: []string{r.TheaterID}, Date: r.ServiceDate, StartAfter: "18:00", FinishBefore: "23:00", Language: LanguageVF})
		if err != nil || (len(slots) > 0) != (test.want > 0) {
			t.Fatalf("unknown end/VFSTF slot filter: %v", err)
		}
	}
	for _, values := range [][2]int{{-1, 0}, {0, -1}, {int(^uint(0) >> 1), 1}, {1, int(^uint(0) >> 1)}} {
		if _, ok := MegaramaEnd(start, values[0], values[1]); ok {
			t.Fatal("unsafe duration arithmetic")
		}
	}
}
