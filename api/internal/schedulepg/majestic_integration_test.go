package schedulepg

import (
	"reflect"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func majesticTestDataset() Dataset {
	d := noecinemasTestDataset()
	d.Provider = schedule.ProviderMajestic
	d.Coverage = nil
	r := &d.Showtimes[0]
	r.Provider = schedule.ProviderMajestic
	r.ID = "majestic-showing-emsx070000000001"
	r.ProviderShowingID = "emsx070000000001"
	r.TheaterID = "majestic-EMS0700"
	r.Movie = MovieRecord{Provider: schedule.ProviderMajestic, ProviderID: "ABCDE", Slug: "majestic-film-ABCDE", Title: "Film", RuntimeMinutes: 100, PosterURL: "https://images.monnaie-services.com/movie_poster/600/FRABCDE/ABCDEFG1.webp"}
	r.FirstPartDurationMinutes = 10
	r.EndTime = r.StartTime.Add(110 * time.Minute)
	r.Room = "Salle source"
	r.BookingURL = "https://www.ticketingcine.com/EMS0700/FRABCDE#showsession?id=emsx070000000001&ps=ticketingcine"
	d.Theaters = []TheaterRecord{{Provider: schedule.ProviderMajestic, ProviderID: "EMS0700", ID: r.TheaterID, Slug: r.TheaterID, Name: "Cinema", Address: "1 rue", City: "Ville", PostalCode: "60000", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}
	local := *r
	local.ID = "majestic-showing-emsx070000000002"
	local.ProviderShowingID = "emsx070000000002"
	local.Movie = MovieRecord{Provider: schedule.ProviderMajestic, ProviderID: "EMS0700-emsx0700HC12", Slug: "majestic-film-EMS0700-emsx0700HC12", Title: "Event", PosterURL: "https://images.monnaie-services.com/ems_spectacle/120/0700/HC12.jpg?ts=123"}
	local.EndTime = local.StartTime
	local.BookingURL = "https://www.ticketingcine.com?EMS0700#showsession?id=emsx070000000002"
	d.Showtimes = append(d.Showtimes, local)
	return d
}

func TestMajesticRoundTripConstraintsAndIsolationIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	other := noecinemasTestDataset()
	if _, err := store.Replace(t.Context(), []Dataset{other}); err != nil {
		t.Fatal(err)
	}
	before, _, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	d := majesticTestDataset()
	if err := schedule.ValidateDataset(d, true); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := store.Replace(t.Context(), []Dataset{d}); err != nil {
			t.Fatal(err)
		}
	}
	after, _, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Theaters) != 2 || len(after.Showtimes) != 3 {
		t.Fatal("counts")
	}
	for _, r := range after.Showtimes {
		if r.Provider != schedule.ProviderMajestic {
			if !reflect.DeepEqual(r, before.Showtimes[0]) {
				t.Fatal("other provider changed")
			}
			continue
		}
		want := d.Showtimes[0]
		if r.Movie.ProviderID != "ABCDE" {
			want = d.Showtimes[1]
		}
		if r.ID != want.ID || r.BookingURL != want.BookingURL || r.Room != want.Room || r.FirstPartDurationMinutes != 10 || !r.EndTime.Equal(want.EndTime) || r.Movie.PosterURL != want.Movie.PosterURL || r.Movie.PublicMovieID <= 0 {
			t.Fatal("source field roundtrip")
		}
	}
	for table, want := range map[string]int{"screening_history_showtimes": 3, "public_movie_sources": 3, "movie_slug_aliases": 3} {
		historyCount(t, pool, "SELECT count(*) FROM "+table, want)
	}
	historyCount(t, pool, "SELECT count(*) FROM cinema_activity_coverage WHERE provider='majestic' AND status='complete'", 0)
	for _, statement := range []string{
		`UPDATE showtimes SET booking_url=booking_url || '&extra=1' WHERE provider='majestic'`,
		`UPDATE screening_history_showtimes SET booking_url='https://www.ticketingcine.com/EMS0654/FRABCDE#showsession?id=emsx070000000001' WHERE provider='majestic'`,
		`UPDATE showtimes SET movie_provider_id='ZZZZZ' WHERE provider='majestic' AND movie_provider_id='ABCDE'`,
		`UPDATE movies SET poster_url='https://evil.example/a' WHERE provider='majestic'`,
		`UPDATE public_movie_sources SET source_movie_id='EMS0700-emsx0654HC12' WHERE source_provider='majestic'`,
		`UPDATE cinema_activity_coverage SET status='complete',basis='date_response' WHERE provider='majestic'`,
		`UPDATE showtimes SET first_part_duration_minutes=-1 WHERE provider='majestic'`,
		`UPDATE theaters SET provider_id='EMS9999' WHERE provider='majestic'`,
	} {
		if _, err := pool.Exec(t.Context(), statement); err == nil {
			t.Fatal("unsafe SQL accepted", statement)
		}
	}
	bad := majesticTestDataset()
	bad.Showtimes[0].BookingURL += "&extra=1"
	if _, err := store.Replace(t.Context(), []Dataset{bad}); err == nil {
		t.Fatal("invalid dataset published")
	}
	final, _, err := store.Load(t.Context())
	if err != nil || !reflect.DeepEqual(after, final) {
		t.Fatal("failed replacement changed active snapshot", err)
	}
}

func TestMajesticEmptyCensusReloadIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	d := majesticTestDataset()
	d.Showtimes = []ShowtimeRecord{}
	template := d.Theaters[0]
	d.Theaters = nil
	for _, site := range []string{"EMS0700", "EMS0654", "EMS1333", "EMS1212", "EMS1299", "EMS1211", "EMS0957"} {
		theater := template
		theater.ProviderID = site
		theater.ID = "majestic-" + site
		theater.Slug = theater.ID
		theater.AvailableDates = []string{}
		d.Theaters = append(d.Theaters, theater)
	}
	if _, err := store.Replace(t.Context(), []Dataset{d}); err != nil {
		t.Fatal(err)
	}
	reloaded, _, err := store.Load(t.Context())
	if err != nil || len(reloaded.Theaters) != 7 || len(reloaded.Showtimes) != 0 {
		t.Fatal("explicit empty census did not reload", err)
	}
}
