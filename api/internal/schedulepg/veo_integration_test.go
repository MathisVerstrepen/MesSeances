package schedulepg

import (
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/schedule"
)

func veoTestDataset() Dataset {
	d := noecinemasTestDataset()
	d.Provider = schedule.ProviderVeo
	r := &d.Showtimes[0]
	id := schedule.VeoShowingID("P1028", "same")
	r.Provider, r.ID, r.ProviderShowingID, r.TheaterID = schedule.ProviderVeo, "veo-showing-"+id, id, "veo-P1028"
	r.Movie = MovieRecord{Provider: schedule.ProviderVeo, ProviderID: "123", Slug: "veo-film-123", Title: "Véo film", RuntimeMinutes: 120}
	r.EndTime = r.StartTime.Add(80 * time.Minute)
	r.Room = ""
	r.BookingURL = "https://achat.veocinemas.fr/fixture/r/123"
	d.Theaters = []TheaterRecord{{Provider: schedule.ProviderVeo, ProviderID: "P1028", ID: r.TheaterID, Slug: r.TheaterID, Name: "Véo", Address: "1 rue", City: "Paris", PostalCode: "75001", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}
	t := d.Theaters[0]
	t.ProviderID = "EMS1361"
	t.ID = "veo-EMS1361"
	t.Slug = t.ID
	t.Name = "Castelia"
	d.Theaters = append(d.Theaters, t)
	cast := *r
	id = schedule.VeoShowingID("EMS1361", "emsx136100000001")
	cast.ID = "veo-showing-" + id
	cast.ProviderShowingID = id
	cast.TheaterID = t.ID
	cast.Movie = MovieRecord{Provider: schedule.ProviderVeo, ProviderID: "ticketingcine-A12B3", Slug: "veo-film-ticketingcine-A12B3", Title: "Ticket film", RuntimeMinutes: 90}
	cast.FirstPartDurationMinutes = 10
	cast.EndTime = cast.StartTime.Add(100 * time.Minute)
	cast.BookingURL = "https://castelginest.miracinemas.fr/#showsession?id=emsx136100000001"
	d.Showtimes = append(d.Showtimes, cast)
	d.Coverage = []schedule.PublicationCoverage{{TheaterID: r.TheaterID, ServiceDate: r.ServiceDate, Status: schedule.CoverageComplete, Basis: schedule.CoverageDateResponse}, {TheaterID: t.ID, ServiceDate: r.ServiceDate, Status: schedule.CoverageUnknown, Basis: schedule.CoverageUnproven}}
	return d
}

func TestVeoPostgresReloadHistoryAndCoverageIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	d := veoTestDataset()
	if err := schedule.ValidateDataset(d, true); err != nil {
		t.Fatal(err)
	}
	published, err := store.Replace(t.Context(), []Dataset{d})
	if err != nil {
		t.Fatal(err)
	}
	reloaded, _, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Theaters) != 2 || len(reloaded.Showtimes) != 2 {
		t.Fatal("reload counts")
	}
	for _, r := range reloaded.Showtimes {
		if r.Provider != schedule.ProviderVeo || r.Movie.PublicMovieID <= 0 {
			t.Fatal(r)
		}
		want := 80 * time.Minute
		if r.TheaterID == "veo-EMS1361" {
			want = 100 * time.Minute
		}
		if r.EndTime.Sub(r.StartTime) != want {
			t.Fatal("canonical end changed")
		}
	}
	for table, want := range map[string]int{"screening_history_theaters": 2, "screening_history_showtimes": 2, "public_movie_sources": 2, "movie_slug_aliases": 2, "cinema_activity_state": 2} {
		var n int
		if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != want {
			t.Fatal(table, n, err)
		}
	}
	assertVeoActivityCoverage(t, pool, d, published.Version)
	var complete bool
	if err := pool.QueryRow(t.Context(), `SELECT count(*)=1 FROM cinema_activity_coverage WHERE theater_id='veo-P1028' AND service_date=$1 AND generation=$2 AND status='complete' AND basis='date_response'`, d.Showtimes[0].ServiceDate, published.Version).Scan(&complete); err != nil || !complete {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE cinema_activity_coverage SET status='complete',basis='date_response' WHERE theater_id='veo-EMS1361'`); err == nil {
		t.Fatal("Castelia false completeness")
	}
	bad := veoTestDataset()
	bad.Showtimes[0].BookingURL += "?unsafe=1"
	if _, err := store.Replace(t.Context(), []Dataset{bad}); err == nil {
		t.Fatal("unsafe replacement")
	}
	after, _, err := store.Load(t.Context())
	if err != nil || len(after.Showtimes) != 2 {
		t.Fatal("invalid replacement changed snapshot", err)
	}
}

func TestVeoAlternateCheckoutPersistenceIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	for _, cinema := range []struct{ id, slug, session string }{{"P0448", "chateaurenard", "22309"}, {"W3187", "labarthe-sur-leze", "5093"}} {
		for _, segment := range []string{"", "reserver/"} {
			d := veoTestDataset()
			theater := &d.Theaters[0]
			theater.ProviderID, theater.ID, theater.Slug = cinema.id, "veo-"+cinema.id, "veo-"+cinema.id
			row := &d.Showtimes[0]
			row.ProviderShowingID = schedule.VeoShowingID(cinema.id, "same-session-"+segment)
			row.ID, row.TheaterID = "veo-showing-"+row.ProviderShowingID, theater.ID
			row.BookingURL = "https://achat.veocinemas.fr/" + cinema.slug + "/" + segment + "r/" + cinema.session
			d.Coverage[0].TheaterID = theater.ID
			if _, err := store.Replace(t.Context(), []Dataset{d}); err != nil {
				t.Fatal(err)
			}
			reloaded, _, err := store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, got := range reloaded.Showtimes {
				if got.ID == row.ID {
					found = true
					if got.BookingURL != row.BookingURL || !got.EndTime.Equal(row.EndTime) || got.Room != row.Room {
						t.Fatal("reload normalized source or changed end/room")
					}
				}
			}
			var booking string
			if err := pool.QueryRow(t.Context(), `SELECT booking_url FROM screening_history_showtimes WHERE id=$1`, row.ID).Scan(&booking); err != nil || booking != row.BookingURL || !found {
				t.Fatal("history lost original source alias", err)
			}
		}
	}
}

func assertVeoActivityCoverage(t *testing.T, pool *pgxpool.Pool, d Dataset, generation int64) {
	t.Helper()
	type key struct{ theater, date, status, basis string }
	want, got := map[key]int{}, map[key]int{}
	for _, coverage := range d.Coverage {
		want[key{coverage.TheaterID, coverage.ServiceDate, coverage.Status, coverage.Basis}]++
	}
	// The store also records unknown current-day evidence per known cinema.
	// Derive that day from the persisted publication instant, not the host clock
	// or a hard-coded total. If dates coincide, preserve multiset multiplicity.
	for _, theater := range d.Theaters {
		var day string
		if err := pool.QueryRow(t.Context(), `SELECT ((last_publication_at AT TIME ZONE 'Europe/Paris')-interval '3 hours')::date::text FROM cinema_activity_state WHERE theater_id=$1`, theater.ID).Scan(&day); err != nil {
			t.Fatal(err)
		}
		want[key{theater.ID, day, schedule.CoverageUnknown, schedule.CoverageUnproven}]++
	}
	rows, err := pool.Query(t.Context(), `SELECT c.theater_id,c.service_date::text,c.status,c.basis,
coalesce(c.provider='veo' AND c.generation=$1 AND c.source_generated_at=$2 AND c.detected_at=s.last_publication_at,false)
FROM cinema_activity_coverage c LEFT JOIN cinema_activity_state s ON s.theater_id=c.theater_id`, generation, d.GeneratedAt)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var k key
		var metadataValid bool
		if err := rows.Scan(&k.theater, &k.date, &k.status, &k.basis, &metadataValid); err != nil {
			t.Fatal(err)
		}
		if !metadataValid {
			t.Fatal("coverage provider/generation/source/publication timestamp changed", k)
		}
		got[k]++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("advertised and generated-day coverage mismatch: got=%v want=%v", got, want)
	}
}
