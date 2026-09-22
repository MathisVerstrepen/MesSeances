package schedulepg

import (
	"fmt"
	"reflect"
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
	assertHistoryChains(t, r, []schedule.HistoryChainRank{{Chain: schedule.ProviderUGC, ShowtimeCount: 6, MovieCount: 3, TheaterCount: 3}})
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
	// Local ranking prioritizes screenings while preserving distinct movie counts.
	if r.Local.Theaters[0].ID != "ugc-25" || r.Local.Theaters[0].MovieCount != 1 || r.Local.Theaters[0].ShowtimeCount != 3 || r.Local.Theaters[1].ID != "ugc-26" || r.Local.Theaters[1].MovieCount != 2 || r.Local.Theaters[1].ShowtimeCount != 2 {
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
		theaters  int
	}{
		{"all", schedule.StatisticsQuery{}, 6, 3, 3},
		{"bounded", schedule.StatisticsQuery{Date: "2026-08-14", DateTo: "2026-08-16"}, 6, 3, 3},
		{"single date", schedule.StatisticsQuery{Date: "2026-08-15"}, 6, 3, 3},
		{"multicity", schedule.StatisticsQuery{City: []string{"lille", "lyon"}}, 6, 3, 3},
		{"theater intersection", schedule.StatisticsQuery{City: []string{"lille"}, Theater: []string{"ugc-25", "ugc-99"}}, 3, 1, 1},
		{"multicity theater intersection", schedule.StatisticsQuery{City: []string{"lille", "lyon"}, Theater: []string{"ugc-25", "ugc-99"}}, 4, 2, 2},
		{"disjoint city theater", schedule.StatisticsQuery{City: []string{"lyon"}, Theater: []string{"ugc-25", "ugc-26"}}, 0, 0, 0},
		{"combined", schedule.StatisticsQuery{Chain: "ugc", Language: "VOSTFR", Format: "2D", Genre: "drame", Pass: "UGC_ILLIMITE"}, 3, 1, 2},
		{"multigenre", schedule.StatisticsQuery{Genre: "action"}, 4, 1, 2},
		{"canonical film", schedule.StatisticsQuery{Film: fmt.Sprintf("film-%d", canonical)}, 4, 1, 2},
		{"unknown film", schedule.StatisticsQuery{Film: "film-9223372036854775807"}, 0, 0, 0},
		{"excluded chain", schedule.StatisticsQuery{Chain: "kinepolis"}, 0, 0, 0},
		{"excluded format", schedule.StatisticsQuery{Format: "IMAX"}, 0, 0, 0},
		{"excluded genre", schedule.StatisticsQuery{Genre: "comédie"}, 0, 0, 0},
		{"excluded pass", schedule.StatisticsQuery{Pass: "unknown"}, 0, 0, 0},
		{"unknown", schedule.StatisticsQuery{Language: "unknown"}, 0, 0, 0},
		{"empty", schedule.StatisticsQuery{Date: "2026-08-16"}, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := historyGet(t, s, tc.query)
			assertHistorySums(t, got)
			if got.Totals.Showtimes != tc.showtimes || got.Totals.Movies != tc.movies {
				t.Fatal("filtered weights", got.Totals)
			}
			want := []schedule.HistoryChainRank{}
			if tc.showtimes > 0 {
				want = append(want, schedule.HistoryChainRank{Chain: schedule.ProviderUGC, ShowtimeCount: tc.showtimes, MovieCount: tc.movies, TheaterCount: tc.theaters})
			}
			assertHistoryChains(t, got, want)
		})
	}
}

func assertHistoryChains(t *testing.T, r schedule.HistoryStatistics, want []schedule.HistoryChainRank) {
	t.Helper()
	if r.Chains == nil || !reflect.DeepEqual(r.Chains, want) {
		t.Fatalf("chains=%+v want=%+v", r.Chains, want)
	}
	var showtimes, theaters int
	for _, chain := range r.Chains {
		showtimes += chain.ShowtimeCount
		theaters += chain.TheaterCount
	}
	if showtimes != r.Totals.Showtimes || theaters != r.Totals.Theaters {
		t.Fatalf("chain totals=%d/%d global=%+v", showtimes, theaters, r.Totals)
	}
}

func TestHistoryChainAggregationIntegration(t *testing.T) {
	for _, tc := range []struct {
		name          string
		kineShowtimes int
		kineMovies    int
		ugcFirst      bool
	}{
		{"screenings before movies", 6, 1, false},
		{"movies before provider key", 5, 1, true},
		{"provider key tie", 5, 4, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := newHistoryPool(t)
			s := NewStore(pool)
			empty := historyGet(t, s, schedule.StatisticsQuery{})
			assertHistoryChains(t, empty, []schedule.HistoryChainRank{})
			assertHistorySums(t, empty)

			ugc, kine := testDataset(), kinepolisTestDataset()
			base := kine.Showtimes[0]
			kine.Showtimes = nil
			for i := range tc.kineShowtimes {
				showing := base
				showing.ProviderShowingID = fmt.Sprintf("VS%d", i+1)
				showing.ID = "kinepolis-showing-" + showing.ProviderShowingID
				showing.BookingURL = "https://kinepolis.fr/direct-vista-redirect/" + showing.ProviderShowingID + "/0/LOM/0"
				showing.Movie.ProviderID = fmt.Sprintf("HO%d", 200+i%tc.kineMovies)
				showing.Movie.Slug = "kinepolis-film-" + showing.Movie.ProviderID
				showing.Movie.Title = "Kinepolis " + showing.Movie.ProviderID
				kine.Showtimes = append(kine.Showtimes, showing)
			}
			historyPublish(t, s, ugc, kine)
			historyPublish(t, s, ugc, kine) // Re-observation cannot inflate chain counts.
			var canonical int64
			if err := pool.QueryRow(t.Context(), `SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='200'`).Scan(&canonical); err != nil {
				t.Fatal(err)
			}
			// One canonical film appears in both chains and two UGC theaters.
			historyExec(t, pool, `UPDATE public_movie_sources SET public_movie_id=$1 WHERE source_provider='kinepolis' AND source_movie_id='HO200'`, canonical)
			ugcRank := schedule.HistoryChainRank{Chain: schedule.ProviderUGC, ShowtimeCount: 5, MovieCount: 4, TheaterCount: 3}
			kineRank := schedule.HistoryChainRank{Chain: schedule.ProviderKinepolis, ShowtimeCount: tc.kineShowtimes, MovieCount: tc.kineMovies, TheaterCount: 1}
			want := []schedule.HistoryChainRank{kineRank, ugcRank}
			if tc.ugcFirst {
				want = []schedule.HistoryChainRank{ugcRank, kineRank}
			}
			r := historyGet(t, s, schedule.StatisticsQuery{})
			assertHistoryChains(t, r, want)
			assertHistorySums(t, r)
			if r.Totals.Movies != 4+tc.kineMovies-1 {
				t.Fatal("shared film must count once globally, once per chain", r.Totals, r.Chains)
			}
			shared := historyGet(t, s, schedule.StatisticsQuery{Film: fmt.Sprintf("film-%d", canonical)})
			assertHistoryChains(t, shared, []schedule.HistoryChainRank{
				{Chain: schedule.ProviderKinepolis, ShowtimeCount: (tc.kineShowtimes + tc.kineMovies - 1) / tc.kineMovies, MovieCount: 1, TheaterCount: 1},
				{Chain: schedule.ProviderUGC, ShowtimeCount: 2, MovieCount: 1, TheaterCount: 2},
			})
			if shared.Totals.Movies != 1 {
				t.Fatal("canonical film filter counted sources instead of films", shared.Totals)
			}
			for _, rank := range want {
				filtered := historyGet(t, s, schedule.StatisticsQuery{Chain: string(rank.Chain)})
				assertHistoryChains(t, filtered, []schedule.HistoryChainRank{rank})
			}
			filtered := historyGet(t, s, schedule.StatisticsQuery{Chain: "ugc", City: []string{"lomme"}})
			assertHistoryChains(t, filtered, []schedule.HistoryChainRank{})
			// Coverage and options still contain both providers, not chain rows.
			if len(filtered.Coverage.Providers) != 2 || len(filtered.Options.Chains) != 2 {
				t.Fatal("empty filter lost inventory", filtered.Coverage, filtered.Options)
			}
		})
	}
}
