package schedulepg

import (
	"messeances/api/internal/schedule"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func ocineTestDataset() Dataset {
	d := cineodeTestDataset()
	d.Provider = schedule.ProviderOcine
	r := &d.Showtimes[0]
	r.Provider = d.Provider
	r.TheaterID = "ocine-309"
	r.Movie = MovieRecord{Provider: d.Provider, ProviderID: "123", Slug: "ocine-film-123", Title: "Ociné fixture", PosterURL: "https://fr.web.img6.acsta.net/pictures/26/10/01/poster.jpg"}
	r.ProviderShowingID = "309-123-" + strconv.FormatInt(r.StartTime.Unix(), 10) + "-past"
	r.ID = "ocine-showing-" + r.ProviderShowingID
	r.BookingURL = ""
	r.Language = ""
	r.ProviderVersion = ""
	d.Theaters = []TheaterRecord{{Provider: d.Provider, ProviderID: "309", ID: r.TheaterID, Slug: r.TheaterID, Name: "Ociné Maubeuge", City: "Maubeuge", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}
	return d
}
func TestOcineRoundTripConstraintsAndIsolationIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	if _, err := store.Replace(t.Context(), []Dataset{noecinemasTestDataset()}); err != nil {
		t.Fatal(err)
	}
	before, _, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	d := ocineTestDataset()
	booked := d.Showtimes[0]
	booked.ProviderShowingID = "309-123-" + strconv.FormatInt(booked.StartTime.Unix(), 10) + "-4"
	booked.ID = "ocine-showing-" + booked.ProviderShowingID
	booked.Language = schedule.LanguageVOSTFR
	booked.ProviderVersion = "VOSTF"
	booked.Room = "Salle 4"
	booked.EndTime = booked.StartTime.Add(2 * time.Hour)
	booked.BookingURL = "https://www.ocine.fr/maubeuge/reserver/F123/D" + strconv.FormatInt(booked.StartTime.Unix(), 10) + "/VO/4/"
	d.Showtimes = append(d.Showtimes, booked)
	for range 2 {
		if _, err := store.Replace(t.Context(), []Dataset{d}); err != nil {
			t.Fatal(err)
		}
	}
	after, _, err := store.Load(t.Context())
	if err != nil || len(after.Theaters) != 2 || len(after.Showtimes) != 3 {
		t.Fatal("reload", err)
	}
	for _, r := range after.Showtimes {
		if r.Provider != schedule.ProviderOcine {
			if !reflect.DeepEqual(r, before.Showtimes[0]) {
				t.Fatal("other provider changed")
			}
			continue
		}
		var want schedule.ShowtimeRecord
		for _, s := range d.Showtimes {
			if s.ID == r.ID {
				want = s
			}
		}
		if r.ID != want.ID || r.Movie.ProviderID != want.Movie.ProviderID || r.Movie.PosterURL != want.Movie.PosterURL || r.Movie.PublicMovieID <= 0 || r.Movie.RuntimeMinutes != 0 || !r.EndTime.Equal(want.EndTime) || r.BookingURL != want.BookingURL || r.Room != want.Room || r.Language != want.Language || r.ProviderVersion != want.ProviderVersion {
			t.Fatal("source fields changed")
		}
	}
	for table, want := range map[string]int{"screening_history_showtimes": 3, "public_movie_sources": 2, "movie_slug_aliases": 2} {
		historyCount(t, pool, "SELECT count(*) FROM "+table, want)
	}
	historyCount(t, pool, `SELECT count(*) FROM cinema_activity_coverage WHERE provider='ocine' AND status='complete'`, 0)
	for _, sql := range []string{`UPDATE showtimes SET booking_url=booking_url || '?booking=1' WHERE provider='ocine'`, `UPDATE screening_history_showtimes SET end_time=start_time+interval '1 minute' WHERE provider='ocine' AND booking_url=''`, `UPDATE showtimes SET booking_url=replace(booking_url,'/maubeuge/','/dunkerque/') WHERE provider='ocine' AND booking_url<>''`, `UPDATE showtimes SET booking_url=replace(booking_url,'/F123/','/F124/') WHERE provider='ocine' AND booking_url<>''`, `UPDATE showtimes SET language='VF' WHERE provider='ocine' AND booking_url<>''`, `UPDATE showtimes SET movie_provider_id='other-film' WHERE provider='ocine'`, `UPDATE showtimes SET first_part_duration_minutes=10 WHERE provider='ocine'`, `UPDATE showtimes SET service_date=service_date-1 WHERE provider='ocine'`, `UPDATE movies SET poster_url='https://evil.example/poster.jpg' WHERE provider='ocine'`, `UPDATE public_movie_sources SET source_slug='invented' WHERE source_provider='ocine'`, `UPDATE theaters SET provider_id='9999' WHERE provider='ocine'`, `UPDATE cinema_activity_coverage SET status='complete',basis='date_response' WHERE provider='ocine'`} {
		if _, err := pool.Exec(t.Context(), sql); err == nil {
			t.Fatal("unsafe SQL accepted", sql)
		}
	}
	bad := ocineTestDataset()
	bad.Showtimes[0].BookingURL = "https://www.ocine.fr/maubeuge/"
	if _, err := store.Replace(t.Context(), []Dataset{bad}); err == nil {
		t.Fatal("invalid replacement published")
	}
	final, _, err := store.Load(t.Context())
	if err != nil || !reflect.DeepEqual(after, final) {
		t.Fatal("failed replacement changed snapshot", err)
	}
}
func TestOcineEmptyCensusReloadIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	d := ocineTestDataset()
	d.Showtimes = []ShowtimeRecord{}
	template := d.Theaters[0]
	d.Theaters = nil
	for _, id := range []string{"309", "310", "461"} {
		th := template
		th.ProviderID = id
		th.ID = "ocine-" + id
		th.Slug = th.ID
		th.AvailableDates = []string{}
		d.Theaters = append(d.Theaters, th)
	}
	if _, err := store.Replace(t.Context(), []Dataset{d}); err != nil {
		t.Fatal(err)
	}
	reloaded, _, err := store.Load(t.Context())
	if err != nil || len(reloaded.Theaters) != 3 || len(reloaded.Showtimes) != 0 {
		t.Fatal("empty census lost", err)
	}
	for _, th := range reloaded.Theaters {
		if th.AvailableDates == nil || len(th.AvailableDates) != 0 || th.Address != "" || th.PostalCode != "" {
			t.Fatal("empty metadata changed")
		}
	}
	d.Theaters = d.Theaters[:2]
	if _, err := store.Replace(t.Context(), []Dataset{d}); err == nil {
		t.Fatal("incomplete empty census accepted")
	}
}
