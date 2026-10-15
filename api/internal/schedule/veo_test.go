package schedule

import (
	"strings"
	"testing"
	"time"
)

func TestVeoIdentitiesAndBookingSafety(t *testing.T) {
	for _, tc := range []struct {
		kind, id string
		valid    bool
	}{{"theater", "P1028", true}, {"theater", "EMS1361", true}, {"theater", "EMS9999", false}, {"movie", "123", true}, {"movie", "cEvent_1", true}, {"movie", "ticketingcine-A12B3", true}, {"movie", strings.Repeat("1", 112), true}, {"movie", strings.Repeat("1", 113), false}, {"movie", "0123", false}, {"showing", VeoShowingID("P1028", "opaque source"), true}} {
		if ValidVeoIdentity(tc.kind, tc.id) != tc.valid {
			t.Fatal(tc)
		}
	}
	if VeoShowingID("P1028", "same") == VeoShowingID("W0067", "same") || VeoShowingID("P1028", "same") == VeoShowingID("P1028", "same ") {
		t.Fatal("hash collision namespace")
	}
	showing := VeoShowingID("EMS1361", "emsx136100000001")
	for _, raw := range []string{"https://castelginest.miracinemas.fr", "https://castelginest.miracinemas.fr/", "https://castelginest.miracinemas.fr/#showsession?id=emsx136100000001"} {
		if !ValidVeoBookingURL(raw, "EMS1361", showing) {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"https://castelginest.miracinemas.fr/#showsession?id=emsx136100000002", "https://castelginest.miracinemas.fr/?x=1", "https://castelginest.miracinemas.fr:443/", "https://user@castelginest.miracinemas.fr/", "https://castelginest.miracinemas.fr/%2f", "https://castelginest.miracinemas.fr/#showsession?id=emsx136100000001&x=1"} {
		if ValidVeoBookingURL(raw, "EMS1361", showing) {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"https://achat.veocinemas.fr/fixture/r/1?x=1", "https://achat.veocinemas.fr/fixture/r/1#x", "https://achat.veocinemas.fr:443/fixture/r/1", "https://achat.veocinemas.fr/fixture/r/0", "https://achat.veocinemas.fr/Fixture/r/1", "https://achat.veocinemas.fr/../fixture/r/1", "https://achat.veocinemas.fr/fixture%2f/r/1"} {
		if ValidVeoCheckoutURL(raw) {
			t.Fatal(raw)
		}
	}
}

func TestVeoDatasetTimingAndPlatformChecks(t *testing.T) {
	loc, _ := time.LoadLocation(Timezone)
	start := time.Date(2026, 10, 15, 23, 30, 0, 0, loc)
	fixture := func(ticket bool) Dataset {
		cinema, movie, booking := "P1028", "123", "https://achat.veocinemas.fr/fixture/r/1"
		if ticket {
			cinema, movie, booking = "EMS1361", "ticketingcine-A12B3", "https://castelginest.miracinemas.fr/"
		}
		id := VeoShowingID(cinema, "emsx136100000001")
		r := ShowtimeRecord{Provider: ProviderVeo, ID: "veo-showing-" + id, ProviderShowingID: id, TheaterID: "veo-" + cinema, ServiceDate: "2026-10-15", StartTime: start, EndTime: start, Language: LanguageVF, Format: Format2D, BookingURL: booking, Movie: MovieRecord{Provider: ProviderVeo, ProviderID: movie, Slug: "veo-film-" + movie, Title: "Fixture", Genres: []string{}}}
		r.ProviderVersion = "VF"
		return Dataset{SchemaVersion: SchemaVersion, Provider: ProviderVeo, Scope: ScopeAll, Timezone: Timezone, GeneratedAt: start.UTC(), Window: Window{From: "2026-10-15", Through: "2026-10-15"}, Theaters: []TheaterRecord{{Provider: ProviderVeo, ProviderID: cinema, ID: r.TheaterID, Slug: r.TheaterID, Name: "Fixture", Address: "1 rue", City: "Paris", PostalCode: "75001", AvailableDates: []string{"2026-10-15"}, AcceptedPasses: []string{}}}, Showtimes: []ShowtimeRecord{r}}
	}
	for _, ticket := range []bool{false, true} {
		d := fixture(ticket)
		if err := ValidateDataset(d, true); err != nil {
			t.Fatal(ticket, err)
		}
		d.Showtimes[0].Movie.RuntimeMinutes = 120
		if ticket {
			d.Showtimes[0].FirstPartDurationMinutes = 10
			d.Showtimes[0].EndTime = start.Add(130 * time.Minute)
		}
		if err := ValidateDataset(d, true); err != nil {
			t.Fatal(err)
		}
		if !ticket {
			d.Showtimes[0].EndTime = start.Add(70 * time.Minute)
			if err := ValidateDataset(d, true); err != nil {
				t.Fatal("exact end recomputed from runtime", err)
			}
		}
		d.Showtimes[0].EndTime = start.Add(-time.Minute)
		if ValidateDataset(d, true) == nil {
			t.Fatal("negative end accepted")
		}
	}
	d := fixture(false)
	d.Showtimes[0].Movie.ProviderID = "ticketingcine-A12B3"
	d.Showtimes[0].Movie.Slug = "veo-film-ticketingcine-A12B3"
	if ValidateDataset(d, true) == nil {
		t.Fatal("cross-platform movie")
	}
	d = fixture(false)
	d.Showtimes[0].FirstPartDurationMinutes = 1
	if ValidateDataset(d, true) == nil {
		t.Fatal("box firstpart")
	}
	d = fixture(true)
	d.Coverage = []PublicationCoverage{{TheaterID: d.Theaters[0].ID, ServiceDate: "2026-10-15", Status: CoverageComplete, Basis: CoverageDateResponse}}
	if ValidateDataset(d, true) == nil {
		t.Fatal("Castelia completeness")
	}
}
