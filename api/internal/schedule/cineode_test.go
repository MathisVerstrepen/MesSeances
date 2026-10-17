package schedule

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func cineodeTestDataset() Dataset {
	d := testDataset()
	d.Provider = ProviderCineode
	d.Showtimes = d.Showtimes[:1]
	d.Coverage = nil
	r := &d.Showtimes[0]
	r.Provider = ProviderCineode
	r.TheaterID = "cineode-991"
	r.ServiceDate = "2026-08-15"
	loc, _ := time.LoadLocation(Timezone)
	r.StartTime = time.Date(2026, 8, 15, 0, 15, 0, 0, loc)
	r.EndTime = r.StartTime
	r.ProviderShowingID = "991-atelier-local-" + strconv.FormatInt(r.StartTime.Unix(), 10)
	r.ID = "cineode-showing-" + r.ProviderShowingID
	r.Movie = MovieRecord{Provider: ProviderCineode, ProviderID: "atelier-local", Slug: "cineode-film-atelier-local", Title: "L'été"}
	r.Language = LanguageVOSTFR
	r.ProviderVersion = "VOSTF"
	r.Room = ""
	r.FirstPartDurationMinutes = 0
	r.BookingURL = CineodeWebsite("991")
	d.Theaters = []TheaterRecord{{Provider: ProviderCineode, ID: r.TheaterID, Slug: r.TheaterID, ProviderID: "991", Name: "Le Don Camillo", City: "Montmirail", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}
	return d
}

func TestCineodeSourcePoliciesAndPublicReads(t *testing.T) {
	d := cineodeTestDataset()
	if err := ValidateDataset(d, true); err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"https://fr.web.img6.acsta.net/img/aa/poster.jpg", "https://static.cotecine.fr/tb/Affiches/180x240/4332_638323.JPG", "https://static.cotecine.fr/tb/Affiches/180x240/LE+BON+LA+BRUTE.JPG"} {
		if !ValidCineodePosterURL(url) {
			t.Fatal("source poster rejected", url)
		}
	}
	for _, suffix := range []string{"a.jpg", "a%2b.JPG", "a/b.JPG", "a.JPG?x=1", "a.JPG#x", "../a.JPG", "a.JPG/", "a.JPG%23"} {
		if ValidCineodePosterURL("https://static.cotecine.fr/tb/Affiches/180x240/" + suffix) {
			t.Fatal("unsafe poster accepted")
		}
	}
	for _, id := range []string{"0", "01", "Local-Film", "a--b", strings.Repeat("a", 116)} {
		if ValidCineodeIdentity("movie", id) {
			t.Fatal("unsafe identity accepted")
		}
	}
	service, err := NewService(newTestSource(d), ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	list := service.Theaters(TheaterCatalogQuery{Chain: "cineode"})
	if len(list) != 1 || list[0].ID != "cineode-991" {
		t.Fatal("chain read")
	}
	timeline, err := service.Timeline(TimelineQuery{Date: "2026-08-15", TheaterIDs: []string{"cineode-991"}, Language: LanguageAll})
	if err != nil || len(timeline.Theaters) != 1 || len(timeline.Theaters[0].Showtimes) != 1 {
		t.Fatal("calendar/public read", err)
	}
	for _, change := range []func(*Dataset){
		func(d *Dataset) { d.Showtimes[0].BookingURL += "?booking=1" },
		func(d *Dataset) {
			d.Showtimes[0].Movie.ProviderID = "other-film"
			d.Showtimes[0].Movie.Slug = "cineode-film-other-film"
		},
		func(d *Dataset) {
			d.Showtimes[0].StartTime = d.Showtimes[0].StartTime.Add(time.Nanosecond)
			d.Showtimes[0].EndTime = d.Showtimes[0].StartTime
		},
		func(d *Dataset) { d.Showtimes[0].EndTime = d.Showtimes[0].StartTime.Add(time.Minute) },
		func(d *Dataset) { d.Showtimes[0].ServiceDate = "2026-08-14" },
		func(d *Dataset) { d.Showtimes[0].Room = "Salle inventée" },
		func(d *Dataset) { d.Showtimes[0].ProviderVersion = "VF" },
	} {
		bad := cineodeTestDataset()
		change(&bad)
		if ValidateDataset(bad, true) == nil {
			t.Fatal("invalid source facts accepted")
		}
	}
}
