package schedule

import (
	"strings"
	"testing"
	"time"
)

func cinemovidaValidationDataset() Dataset {
	d := cineodeTestDataset()
	d.Provider = ProviderCinemovida
	r := &d.Showtimes[0]
	r.Provider = ProviderCinemovida
	r.TheaterID = "cinemovida-P0176"
	r.ProviderShowingID = CinemovidaShowingID("P0176", "same session")
	r.ID = "cinemovida-showing-" + r.ProviderShowingID
	r.Movie = MovieRecord{Provider: ProviderCinemovida, ProviderID: "cEvent_1", Slug: "cinemovida-film-cEvent_1", Title: "Événement local"}
	r.EndTime = r.StartTime.Add(45 * time.Minute)
	r.Room = "Salle 10 PREMIUM MX4D"
	r.BookingURL = "https://achat.cinema-castillet.com/reserver/r/123"
	d.Theaters = []TheaterRecord{{Provider: ProviderCinemovida, ID: r.TheaterID, Slug: r.TheaterID, ProviderID: "P0176", Name: "Castillet", Address: "1 rue", City: "Perpignan", PostalCode: "66000", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}
	d.Coverage = []PublicationCoverage{{TheaterID: r.TheaterID, ServiceDate: r.ServiceDate, Status: CoverageComplete, Basis: CoverageDateResponse}}
	return d
}

func TestCinemovidaDomainCalendarAndSourcePolicies(t *testing.T) {
	for _, runtime := range []int{0, 120} {
		d := cinemovidaValidationDataset()
		d.Showtimes[0].Movie.RuntimeMinutes = runtime
		if err := ValidateDataset(d, true); err != nil {
			t.Fatal("source end/calendar rejected", err)
		}
		service, err := NewService(newTestSource(d), ServiceOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if theaters := service.Theaters(TheaterCatalogQuery{Chain: "CINEMOVIDA"}); len(theaters) != 1 || theaters[0].ID != "cinemovida-P0176" {
			t.Fatal("chain filter")
		}
		timeline, err := service.Timeline(TimelineQuery{Date: "2026-08-15", TheaterIDs: []string{"cinemovida-P0176"}, Language: LanguageAll})
		if err != nil || len(timeline.Theaters) != 1 || len(timeline.Theaters[0].Showtimes) != 1 {
			t.Fatal("first-day midnight calendar lost", err)
		}
		got := timeline.Theaters[0].Showtimes[0]
		if !got.EndTime.Equal(d.Showtimes[0].EndTime) || got.EstimatedEndTime != nil || got.Room != d.Showtimes[0].Room {
			t.Fatal("source end/room replaced")
		}
		d.Showtimes[0].EndTime = d.Showtimes[0].StartTime
		if err := ValidateDataset(d, true); err != nil {
			t.Fatal("sentinel rejected", err)
		}
	}
	if CinemovidaShowingID("P0176", "same") == CinemovidaShowingID("P0218", "same") || CinemovidaShowingID("P0176", "same") == CinemovidaShowingID("P0176", "same ") {
		t.Fatal("identity collapsed")
	}
	if recordProvider("", "cinemovida-film-cEvent_1") != ProviderCinemovida {
		t.Fatal("prefix inference")
	}
	for _, change := range []func(*Dataset){
		func(d *Dataset) { d.Showtimes[0].EndTime = d.Showtimes[0].StartTime.Add(-time.Minute) },
		func(d *Dataset) { d.Showtimes[0].FirstPartDurationMinutes = 1 },
		func(d *Dataset) { d.Showtimes[0].ServiceDate = "2026-08-14" },
		func(d *Dataset) { d.Showtimes[0].BookingURL = "https://achat.cinema-megacastillet.com/reserver/r/123" },
		func(d *Dataset) { d.Showtimes[0].BookingURL += "#" },
		func(d *Dataset) { d.Theaters[0].ProviderID = "P9999" },
		func(d *Dataset) { d.Coverage[0].Basis = CoverageUnproven },
	} {
		d := cinemovidaValidationDataset()
		change(&d)
		if ValidateDataset(d, true) == nil {
			t.Fatal("invalid source facts accepted")
		}
	}
	for _, raw := range []string{"https://acsta.net/x.jpg", "https://fr.web.img6.acsta.net/img/x.jpg"} {
		if !ValidCinemovidaPosterURL(raw) {
			t.Fatal("poster rejected")
		}
	}
	for _, raw := range []string{"https://acsta.net/../x.jpg", "https://acsta.net:443/x.jpg", "https://acsta.net/x.jpg?", "https://evil.test/x.jpg"} {
		if ValidCinemovidaPosterURL(raw) {
			t.Fatal("unsafe poster accepted")
		}
	}
	if !ValidCinemovidaIdentity("movie", "c"+strings.Repeat("x", 111)) || ValidCinemovidaIdentity("movie", "c"+strings.Repeat("x", 112)) {
		t.Fatal("namespace bounds")
	}
}

func TestCinemovidaOriginalBookingRoutes(t *testing.T) {
	for _, theater := range []string{"P0176", "P0218", "P7951", "P0876"} {
		root := "https://" + CinemovidaBookingHost(theater)
		for _, path := range []string{"/reserver/r/123", "/r/123"} {
			raw := root + path
			want := path == "/reserver/r/123" || theater == "P0876"
			if ValidCinemovidaBookingURL(raw, theater) != want {
				t.Fatal("route policy", theater, path)
			}
			for _, invalid := range []string{raw + "?", raw + "#", raw + "\n", " " + raw, strings.Replace(raw, "https:", "http:", 1), strings.Replace(raw, "/123", "/0123", 1), strings.Replace(raw, "/123", "/0", 1), strings.Replace(raw, "/123", "/%31", 1), strings.Replace(raw, "/123", "/../123", 1), strings.Replace(raw, root, root+":443", 1), strings.Replace(raw, "https://", "https://user@", 1)} {
				if ValidCinemovidaBookingURL(invalid, theater) {
					t.Fatal("raw alias accepted", theater)
				}
			}
		}
		if theater != "P0876" && ValidCinemovidaBookingURL("https://achat.concordecinema.com/r/123", theater) {
			t.Fatal("cross-theater route accepted")
		}
	}
}
