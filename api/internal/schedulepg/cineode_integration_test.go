package schedulepg

import (
	"reflect"
	"strconv"
	"testing"

	"messeances/api/internal/schedule"
)

func cineodeTestDataset() Dataset {
	d := noecinemasTestDataset()
	d.Provider = schedule.ProviderCineode
	d.Coverage = nil
	r := &d.Showtimes[0]
	r.Provider = schedule.ProviderCineode
	r.TheaterID = "cineode-991"
	r.ProviderShowingID = "991-atelier-local-" + strconv.FormatInt(r.StartTime.Unix(), 10)
	r.ID = "cineode-showing-" + r.ProviderShowingID
	r.Movie = MovieRecord{Provider: schedule.ProviderCineode, ProviderID: "atelier-local", Slug: "cineode-film-atelier-local", Title: "L'été", PosterURL: "https://static.cotecine.fr/tb/Affiches/180x240/LE+BON.JPG"}
	r.EndTime = r.StartTime
	r.Language = schedule.LanguageVOSTFR
	r.ProviderVersion = "VOSTF"
	r.Room = ""
	r.FirstPartDurationMinutes = 0
	r.BookingURL = schedule.CineodeWebsite("991")
	d.Theaters = []TheaterRecord{{Provider: schedule.ProviderCineode, ProviderID: "991", ID: r.TheaterID, Slug: r.TheaterID, Name: "Le Don Camillo", City: "Montmirail", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}
	return d
}

func TestCineodeRoundTripConstraintsAndIsolationIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	if _, err := store.Replace(t.Context(), []Dataset{noecinemasTestDataset()}); err != nil {
		t.Fatal(err)
	}
	before, _, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	d := cineodeTestDataset()
	if err := schedule.ValidateDataset(d, true); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := store.Replace(t.Context(), []Dataset{d}); err != nil {
			t.Fatal(err)
		}
	}
	after, _, err := store.Load(t.Context())
	if err != nil || len(after.Showtimes) != 2 || len(after.Theaters) != 2 {
		t.Fatal("reload", err)
	}
	for _, r := range after.Showtimes {
		if r.Provider != schedule.ProviderCineode {
			if !reflect.DeepEqual(r, before.Showtimes[0]) {
				t.Fatal("other provider changed")
			}
			continue
		}
		want := d.Showtimes[0]
		if r.ID != want.ID || r.Movie.ProviderID != want.Movie.ProviderID || r.Movie.PosterURL != want.Movie.PosterURL || r.Movie.PublicMovieID <= 0 || r.Movie.RuntimeMinutes != 0 || !r.EndTime.Equal(r.StartTime) || r.BookingURL != want.BookingURL || r.Room != "" {
			t.Fatal("source fields changed")
		}
	}
	for table, want := range map[string]int{"screening_history_showtimes": 2, "public_movie_sources": 2, "movie_slug_aliases": 2} {
		historyCount(t, pool, "SELECT count(*) FROM "+table, want)
	}
	historyCount(t, pool, `SELECT count(*) FROM cinema_activity_coverage WHERE provider='cineode' AND status='complete'`, 0)
	for _, statement := range []string{
		`UPDATE showtimes SET booking_url=booking_url || '?booking=1' WHERE provider='cineode'`,
		`UPDATE screening_history_showtimes SET end_time=start_time+interval '1 minute' WHERE provider='cineode'`,
		`UPDATE showtimes SET movie_provider_id='other-film' WHERE provider='cineode'`,
		`UPDATE showtimes SET provider_version='VF' WHERE provider='cineode'`,
		`UPDATE showtimes SET room='invented' WHERE provider='cineode'`,
		`UPDATE showtimes SET first_part_duration_minutes=10 WHERE provider='cineode'`,
		`UPDATE showtimes SET service_date=service_date-1 WHERE provider='cineode'`,
		`UPDATE movies SET poster_url='https://static.cotecine.fr/tb/Affiches/180x240/a%2Bb.JPG' WHERE provider='cineode'`,
		`UPDATE public_movie_sources SET source_slug='invented' WHERE source_provider='cineode'`,
		`UPDATE theaters SET provider_id='9999' WHERE provider='cineode'`,
		`UPDATE cinema_activity_coverage SET status='complete',basis='date_response' WHERE provider='cineode'`,
	} {
		if _, err := pool.Exec(t.Context(), statement); err == nil {
			t.Fatal("unsafe SQL accepted", statement)
		}
	}
	bad := cineodeTestDataset()
	bad.Showtimes[0].BookingURL += "?extra=1"
	if _, err := store.Replace(t.Context(), []Dataset{bad}); err == nil {
		t.Fatal("invalid replacement published")
	}
	final, _, err := store.Load(t.Context())
	if err != nil || !reflect.DeepEqual(after, final) {
		t.Fatal("failed replacement changed snapshot", err)
	}
}

func TestCineodeEmptyCensusReloadIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	d := cineodeTestDataset()
	d.Showtimes = []ShowtimeRecord{}
	template := d.Theaters[0]
	d.Theaters = nil
	for _, id := range []string{"4640", "597", "4077", "593", "4626", "592", "4388", "4582", "4622", "3524", "981", "3955", "4426", "991", "600", "595", "636", "4420", "603", "601", "594", "3859", "4444", "4624", "4638", "4277", "4386", "4384", "3064", "4075", "3074", "4592", "4181", "4281", "3857", "3945", "3104"} {
		th := template
		th.ProviderID = id
		th.ID = "cineode-" + id
		th.Slug = th.ID
		th.AvailableDates = []string{}
		d.Theaters = append(d.Theaters, th)
	}
	if _, err := store.Replace(t.Context(), []Dataset{d}); err != nil {
		t.Fatal(err)
	}
	reloaded, _, err := store.Load(t.Context())
	if err != nil || len(reloaded.Theaters) != 37 || len(reloaded.Showtimes) != 0 {
		t.Fatal("empty census lost", err)
	}
	for _, th := range reloaded.Theaters {
		if th.AvailableDates == nil || len(th.AvailableDates) != 0 || th.Address != "" || th.PostalCode != "" {
			t.Fatal("empty/unknown metadata changed")
		}
	}
}
