package schedulepg

import (
	"reflect"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func cinemovidaTestDataset() Dataset {
	d := noecinemasTestDataset()
	d.Provider = schedule.ProviderCinemovida
	d.Coverage = nil
	r := d.Showtimes[0]
	r.Provider = schedule.ProviderCinemovida
	r.TheaterID = "cinemovida-P0176"
	r.ProviderShowingID = schedule.CinemovidaShowingID("P0176", "same raw session")
	r.ID = "cinemovida-showing-" + r.ProviderShowingID
	r.Movie = MovieRecord{Provider: schedule.ProviderCinemovida, ProviderID: "cEvent_1", Slug: "cinemovida-film-cEvent_1", Title: "Événement local", PosterURL: "https://fr.web.img6.acsta.net/img/a.jpg", Overview: "Résumé", Genres: []string{"Drame"}}
	r.EndTime = r.StartTime.Add(45 * time.Minute)
	r.FirstPartDurationMinutes = 0
	r.Language = schedule.LanguageVOSTFR
	r.ProviderVersion = "VOSTF"
	r.Format = schedule.Format4DX
	r.Room = "Salle 10 PREMIUM MX4D"
	r.BookingURL = "https://achat.cinema-castillet.com/reserver/r/123"
	d.Theaters = []TheaterRecord{}
	for _, id := range []string{"P0176", "P0218", "P7951", "P0876"} {
		dates := []string{}
		if id == "P0176" || id == "P0218" {
			dates = []string{r.ServiceDate}
		}
		publicID := "cinemovida-" + id
		d.Theaters = append(d.Theaters, TheaterRecord{Provider: schedule.ProviderCinemovida, ProviderID: id, ID: publicID, Slug: publicID, Name: "Ciné-Movida " + id, Address: "1 rue du cinéma", City: "Paris", PostalCode: "75001", AvailableDates: dates, AcceptedPasses: []string{}})
	}
	second := r
	second.TheaterID = "cinemovida-P0218"
	second.ProviderShowingID = schedule.CinemovidaShowingID("P0218", "same raw session")
	second.ID = "cinemovida-showing-" + second.ProviderShowingID
	second.BookingURL = "https://achat.cinema-megacastillet.com/reserver/r/123"
	missing := r
	missing.ProviderShowingID = schedule.CinemovidaShowingID("P0176", "missing end")
	missing.ID = "cinemovida-showing-" + missing.ProviderShowingID
	missing.Movie = MovieRecord{Provider: schedule.ProviderCinemovida, ProviderID: "123", Slug: "cinemovida-film-123", Title: "Film", RuntimeMinutes: 90}
	missing.EndTime = missing.StartTime
	d.Showtimes = []ShowtimeRecord{r, second, missing}
	d.Coverage = []schedule.PublicationCoverage{{TheaterID: r.TheaterID, ServiceDate: r.ServiceDate, Status: schedule.CoverageComplete, Basis: schedule.CoverageDateResponse}, {TheaterID: second.TheaterID, ServiceDate: r.ServiceDate, Status: schedule.CoverageUnknown, Basis: schedule.CoverageAcceptedOmission}}
	return d
}

func TestCinemovidaRoundTripHistoryCatalogCoverageAndIsolationIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	if _, err := store.Replace(t.Context(), []Dataset{noecinemasTestDataset()}); err != nil {
		t.Fatal(err)
	}
	before, _, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	d := cinemovidaTestDataset()
	if err := schedule.ValidateDataset(d, true); err != nil {
		t.Fatal(err)
	}
	var generations []int64
	for range 2 {
		publication, err := store.Replace(t.Context(), []Dataset{d})
		if err != nil {
			t.Fatal(err)
		}
		generations = append(generations, publication.Version)
	}
	after, _, err := store.Load(t.Context())
	if err != nil || len(after.Theaters) != 5 || len(after.Showtimes) != 4 {
		t.Fatal("reload counts", err)
	}
	for _, r := range after.Showtimes {
		if r.Provider != schedule.ProviderCinemovida {
			if !reflect.DeepEqual(r, before.Showtimes[0]) {
				t.Fatal("other provider changed")
			}
			continue
		}
		var want ShowtimeRecord
		for _, source := range d.Showtimes {
			if source.ID == r.ID {
				want = source
			}
		}
		if want.ID == "" || r.Movie.PublicMovieID <= 0 || r.Movie.ProviderID != want.Movie.ProviderID || r.Movie.RuntimeMinutes != want.Movie.RuntimeMinutes || !r.EndTime.Equal(want.EndTime) || r.Room != want.Room || r.BookingURL != want.BookingURL || r.Format != want.Format || r.Language != want.Language || r.ProviderVersion != want.ProviderVersion || r.FirstPartDurationMinutes != 0 {
			t.Fatal("canonical source values lost", r.ID)
		}
	}
	for table, want := range map[string]int{"screening_history_theaters": 5, "screening_history_showtimes": 4, "public_movie_sources": 3, "movie_slug_aliases": 3} {
		historyCount(t, pool, "SELECT count(*) FROM "+table, want)
	}
	historyCount(t, pool, `SELECT count(*) FROM movie_slug_aliases WHERE source_provider='cinemovida' AND alias_kind='source' AND slug='cinemovida-film-'||source_movie_id`, 2)
	// Coverage is an append-only acquisition ledger keyed by publication
	// generation, unlike deduplicated showtime history. Both replacements must
	// retain exactly one complete response and one accepted omission each.
	if generations[0] == generations[1] {
		t.Fatal("replacement reused publication generation")
	}
	for _, generation := range generations {
		var matches bool
		if err := pool.QueryRow(t.Context(), `SELECT count(*)=2
AND count(*) FILTER (WHERE theater_id=$2 AND service_date=$4::date AND status='complete' AND basis='date_response')=1
AND count(*) FILTER (WHERE theater_id=$3 AND service_date=$4::date AND status='unknown' AND basis='accepted_omission')=1
FROM cinema_activity_coverage WHERE provider='cinemovida' AND generation=$1 AND basis IN ('date_response','accepted_omission')`, generation, d.Showtimes[0].TheaterID, d.Showtimes[1].TheaterID, d.Showtimes[0].ServiceDate).Scan(&matches); err != nil || !matches {
			t.Fatalf("acquisition coverage changed for generation %d: matches=%t err=%v", generation, matches, err)
		}
	}
	for _, table := range []string{"showtimes", "screening_history_showtimes"} {
		for _, change := range []string{
			`end_time=start_time-interval '1 minute'`, `first_part_duration_minutes=1`, `service_date=service_date-1`,
			`booking_url='https://achat.cinema-castillet.com/reserver/r/123?x=1'`, `booking_url='https://achat.concordecinema.com/reserver/r/123'`,
			`provider_showing_id='P9999-'||repeat('a',64)`,
		} {
			if _, err := pool.Exec(t.Context(), `UPDATE `+table+` SET `+change+` WHERE provider='cinemovida' AND theater_id='cinemovida-P0176'`); err == nil {
				t.Fatal("unsafe SQL accepted", table, change)
			}
		}
	}
	for _, statement := range []string{
		`UPDATE public_movie_sources SET source_slug='invented' WHERE source_provider='cinemovida'`,
		`UPDATE movie_slug_aliases SET slug='cinemovida-film-wrong' WHERE source_provider='cinemovida'`,
		`UPDATE theaters SET provider_id='P9999' WHERE provider='cinemovida'`,
	} {
		if _, err := pool.Exec(t.Context(), statement); err == nil {
			t.Fatal("unsafe identity SQL accepted")
		}
	}
	bad := cinemovidaTestDataset()
	bad.Showtimes[0].BookingURL += "?x=1"
	if _, err := store.Replace(t.Context(), []Dataset{bad}); err == nil {
		t.Fatal("invalid snapshot published")
	}
	final, _, err := store.Load(t.Context())
	if err != nil || !reflect.DeepEqual(after, final) {
		t.Fatal("atomic failure changed prior data", err)
	}
}
