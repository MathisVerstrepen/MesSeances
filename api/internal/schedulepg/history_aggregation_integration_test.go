package schedulepg

import (
	"fmt"
	"testing"

	"messeances/api/internal/schedule"
)

func TestHistoryAggregationIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	d := testDataset()
	// Two theaters share a city and a canonical movie. A second source in the
	// same theater must add a screening, not another movie or theater.
	d.Theaters[1].City = "Lille"
	alias := d.Showtimes[0]
	alias.ID = "ugc-showing-105"
	alias.ProviderShowingID = "105"
	alias.BookingURL = "https://www.ugc.fr/reservationSeances.html?id=105"
	alias.Movie.ProviderID = "205"
	alias.Movie.Slug = "ugc-film-205"
	alias.Movie.Title = "Source alias"
	d.Showtimes = append(d.Showtimes, alias)
	historyPublish(t, s, d)
	historyPublish(t, s, d) // Re-observation must not multiply either pair weight.
	var canonical int64
	if err := pool.QueryRow(t.Context(), `SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='200'`).Scan(&canonical); err != nil {
		t.Fatal(err)
	}
	historyExec(t, pool, `UPDATE public_movies SET redirect_to_id=$1 WHERE id=(SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='201')`, canonical)
	historyExec(t, pool, `UPDATE public_movie_sources SET public_movie_id=$1 WHERE source_provider='ugc' AND source_movie_id='205'`, canonical)
	// Durable theater passes allow only empty or one UGC_ILLIMITE entry;
	// exercise that membership together with multiple normalized movie genres.
	historyExec(t, pool, `UPDATE public_movies SET title='Same title',genres=ARRAY['Drame']`)
	historyExec(t, pool, `INSERT INTO public_movie_metadata_overrides(public_movie_id,title,title_overridden,runtime_minutes,runtime_minutes_overridden,genres,genres_overridden)
 VALUES($1,'Current title',true,0,true,ARRAY['Drame','drame','Action'],true)`, canonical)
	r := historyGet(t, s, schedule.StatisticsQuery{Genre: "drame", Pass: "UGC_ILLIMITE"})
	assertHistorySums(t, r)
	if r.Totals.Showtimes != 6 || r.Totals.Movies != 3 || r.Totals.Theaters != 3 || r.Totals.Cities != 2 {
		t.Fatal("weighted totals", r.Totals)
	}
	top := r.TopMovies.ByShowtimes[0]
	if top.Slug != fmt.Sprintf("film-%d", canonical) || top.Title != "Current title" || top.ShowtimeCount != 4 || top.TheaterCount != 2 {
		t.Fatal("canonical pair weights", top)
	}
	city := r.Local.Cities[0]
	if city.Slug != "lille" || city.ShowtimeCount != 5 || city.MovieCount != 2 || city.TheaterCount != 2 {
		t.Fatal("city distinct identities", city)
	}
	// Local ranking prioritizes distinct movies, not the pair's screening weight.
	if r.Local.Theaters[0].ID != "ugc-26" || r.Local.Theaters[0].MovieCount != 2 || r.Local.Theaters[0].ShowtimeCount != 2 || r.Local.Theaters[1].ID != "ugc-25" || r.Local.Theaters[1].MovieCount != 1 || r.Local.Theaters[1].ShowtimeCount != 3 {
		t.Fatal("theater distinct identities", r.Local.Theaters)
	}
	if len(r.Genres) != 2 || r.Genres[0].Value != "drame" || r.Genres[0].Count != 3 || r.Genres[1].Value != "action" || r.Genres[1].Count != 1 || r.Runtimes[3].Count != 1 {
		t.Fatal("metadata is counted per movie, not per pair or screening", r.Genres, r.Runtimes)
	}
	for _, tc := range []struct {
		name      string
		query     schedule.StatisticsQuery
		showtimes int
		movies    int
	}{
		{"all", schedule.StatisticsQuery{}, 6, 3},
		{"bounded", schedule.StatisticsQuery{Date: "2026-08-14", DateTo: "2026-08-16"}, 6, 3},
		{"single date", schedule.StatisticsQuery{Date: "2026-08-15"}, 6, 3},
		{"multicity", schedule.StatisticsQuery{City: []string{"lille", "lyon"}}, 6, 3},
		{"theater intersection", schedule.StatisticsQuery{City: []string{"lille"}, Theater: []string{"ugc-25", "ugc-99"}}, 3, 1},
		{"combined", schedule.StatisticsQuery{Chain: "ugc", Language: "VOSTFR", Format: "2D", Genre: "drame", Pass: "UGC_ILLIMITE"}, 3, 1},
		{"multigenre", schedule.StatisticsQuery{Genre: "action"}, 4, 1},
		{"unknown", schedule.StatisticsQuery{Language: "unknown"}, 0, 0},
		{"empty", schedule.StatisticsQuery{Date: "2026-08-16"}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := historyGet(t, s, tc.query)
			assertHistorySums(t, got)
			if got.Totals.Showtimes != tc.showtimes || got.Totals.Movies != tc.movies {
				t.Fatal("filtered weights", got.Totals)
			}
		})
	}
}
