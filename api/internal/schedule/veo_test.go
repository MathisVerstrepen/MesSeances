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

func TestVeoGueretRelayStructuralPolicy(t *testing.T) {
	const raw = "https://relay.mvtx.us/ticketing/dbz?code_theater=P1394&gid_entity=fixture_123.opaque&tags=Localization.Language.French,Format.Projection.Digital&perfd=20261016&perft=15:00"
	valid := []string{raw, strings.ReplaceAll(strings.ReplaceAll(raw, ",", "%2C"), ":00", "%3a00"), strings.ReplaceAll(raw, "20261016", "20000229"), strings.ReplaceAll(raw, "fixture_123.opaque", strings.Repeat("%41", 1024))}
	for _, value := range valid {
		if !ValidVeoCheckoutURL(value) || !ValidVeoBookingURL(value, "G06DB", VeoShowingID("G06DB", "synthetic")) {
			t.Fatal("structural source rejected")
		}
		for _, theater := range []string{"P1394", "P1028", "W0067", "EMS1361"} {
			if ValidVeoBookingURL(value, theater, VeoShowingID(theater, "synthetic")) {
				t.Fatal("relay escaped G06DB")
			}
		}
	}
	bad := []string{raw + "&tags=x", raw + "&x=1", raw + "&", raw + "#", raw + "\n", raw + " ", strings.ReplaceAll(raw, "code_theater", "code%5ftheater"), strings.ReplaceAll(raw, "P1394", "G06DB"), strings.ReplaceAll(raw, "20261016", "20260229"), strings.ReplaceAll(raw, "20261016", "19000229"), strings.ReplaceAll(raw, "20261016", "00001016"), strings.ReplaceAll(raw, "15:00", "24:00"), strings.ReplaceAll(raw, "15:00", "15:60"), strings.ReplaceAll(raw, "https:", "http:"), strings.ReplaceAll(raw, "relay.mvtx.us", "relay.mvtx.us.evil.test"), strings.ReplaceAll(raw, "relay.mvtx.us", "relay.mvtx.us:443"), strings.ReplaceAll(raw, "https://", "https://user@"), strings.ReplaceAll(raw, "/ticketing/dbz", "/ticketing/%64bz"), strings.ReplaceAll(raw, "/ticketing/dbz", "/x/../ticketing/dbz"), "https://achat.cinema-senechal.com/r/1"}
	for _, value := range []string{"", "%", "%GG", "%252C", "%26x%3D1", "%3B", "%23", "%3F", "%2F", "%5C", "%00", "%0A", "+", "%20", "é", "%C3%A9", "a=b", strings.Repeat("a", 1025)} {
		bad = append(bad, strings.ReplaceAll(raw, "fixture_123.opaque", value))
	}
	for i, value := range bad {
		if ValidVeoCheckoutURL(value) || ValidVeoBookingURL(value, "G06DB", VeoShowingID("G06DB", "synthetic")) {
			t.Fatalf("unsafe relay case %d accepted", i)
		}
	}
}

func TestVeoCheckoutSourceForms(t *testing.T) {
	for _, cinema := range []struct{ id, slug, session string }{{"P0448", "chateaurenard", "22309"}, {"W3187", "labarthe-sur-leze", "5093"}} {
		for _, segment := range []string{"", "reserver/"} {
			raw := "https://achat.veocinemas.fr/" + cinema.slug + "/" + segment + "r/" + cinema.session
			showing := VeoShowingID(cinema.id, "opaque-session")
			if !ValidVeoCheckoutURL(raw) || !ValidVeoBookingURL(raw, cinema.id, showing) {
				t.Fatal("source alias rejected", raw)
			}
			if ValidVeoBookingURL(raw, "W0067", showing) || ValidVeoBookingURL(raw, "EMS1361", VeoShowingID("EMS1361", "emsx136100000001")) {
				t.Fatal("cross-cinema/platform identity accepted")
			}
			for _, bad := range []string{
				raw + "?", raw + "?x=1", raw + "#", raw + "/", raw + "/extra", raw + "\n", raw + "\r", " " + raw, raw + "\x00",
				strings.Replace(raw, "https:", "http:", 1), strings.Replace(raw, ".fr/", ".fr:443/", 1), strings.Replace(raw, "https://", "https://user@", 1), strings.Replace(raw, "achat.", "evil.achat.", 1),
				strings.Replace(raw, "/"+cinema.session, "/0", 1), strings.Replace(raw, "/"+cinema.session, "/01", 1), strings.Replace(raw, "/r/", "/R/", 1), strings.Replace(raw, "/r/", "/%72/", 1),
				strings.Replace(raw, "/"+cinema.slug+"/", "/"+cinema.slug+"/../"+cinema.slug+"/", 1),
				strings.Replace(raw, "/"+cinema.slug+"/", "/"+cinema.slug+"/reserver/reserver/", 1),
				strings.Replace(raw, "/"+cinema.slug+"/", "/"+cinema.slug+"/Reserver/", 1),
				strings.Replace(raw, "/"+cinema.slug+"/", "/"+cinema.slug+"/reserver//", 1),
				strings.Replace(raw, "/"+cinema.slug+"/", "/"+cinema.slug+"/%72eserver/", 1),
				strings.Replace(raw, "/"+cinema.slug+"/", "/"+cinema.slug+"/reserver/F1/D1792065000/VF/", 1),
				raw + strings.Repeat("1", 4097),
			} {
				if ValidVeoCheckoutURL(bad) || ValidVeoBookingURL(bad, cinema.id, showing) {
					t.Fatal("unsafe source accepted", bad)
				}
			}
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
