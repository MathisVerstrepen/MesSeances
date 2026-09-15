package schedulepg

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"messeances/api/internal/schedule"
)

func TestHistoryCanonicalIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	historyPublish(t, s, testDataset(), kinepolisTestDataset())
	// Prune every original generation and source schedule row before classification changes.
	empty := testDataset()
	empty.Theaters = nil
	empty.Showtimes = nil
	other := kinepolisTestDataset()
	other.Theaters = nil
	other.Showtimes = nil
	for range 3 {
		historyPublish(t, s, empty, other)
	}
	historyCount(t, pool, `SELECT count(*) FROM showtimes`, 0)
	var a, b int64
	if err := pool.QueryRow(t.Context(), `SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='200'`).Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT public_movie_id FROM public_movie_sources WHERE source_provider='kinepolis' AND source_movie_id='HO200'`).Scan(&b); err != nil {
		t.Fatal(err)
	}
	base := historyGet(t, s, schedule.StatisticsQuery{})
	if base.Totals.Movies != 5 {
		t.Fatal(base.Totals)
	}
	historyExec(t, pool, `UPDATE public_movies SET redirect_to_id=$1 WHERE id=$2`, a, b)
	merged := historyGet(t, s, schedule.StatisticsQuery{})
	assertHistorySums(t, merged)
	if merged.Totals.Movies != 4 || merged.TopMovies.ByShowtimes[0].Slug != fmt.Sprintf("film-%d", a) || merged.TopMovies.ByShowtimes[0].ShowtimeCount != 3 || merged.TopMovies.ByShowtimes[0].TheaterCount != 3 {
		t.Fatal("redirect identity", merged.TopMovies)
	}
	historyExec(t, pool, `INSERT INTO public_movie_metadata_overrides(public_movie_id,title,title_overridden,runtime_minutes,runtime_minutes_overridden,genres,genres_overridden) VALUES($1,'Current override',true,0,true,ARRAY['\u2003Drame\u00a0','drame','Drame','Émotion'],true)`, a)
	// Set actual Unicode values through parameters, not SQL backslash escapes.
	historyExec(t, pool, `UPDATE public_movie_metadata_overrides SET genres=$2 WHERE public_movie_id=$1`, a, []string{"\u2003Drame\u00a0", "drame", "Drame", "Émotion"})
	filtered := historyGet(t, s, schedule.StatisticsQuery{Genre: "DRAME"})
	assertHistorySums(t, filtered)
	if filtered.Totals.Showtimes != 3 || filtered.Totals.Movies != 1 || filtered.TopMovies.ByShowtimes[0].Title != "Current override" || filtered.Runtimes[3].Count != 1 || len(filtered.Genres) != 2 || filtered.Genres[0].Count != 1 {
		t.Fatal("current override classification", filtered.Totals, filtered.Genres)
	}
	// Split via current durable source assignment, without rewriting history.
	historyExec(t, pool, `UPDATE public_movies SET redirect_to_id=NULL,genres=ARRAY['Comédie'] WHERE id=$1`, b)
	historyExec(t, pool, `UPDATE public_movie_sources SET public_movie_id=$1 WHERE source_provider='ugc' AND source_movie_id='200'`, b)
	split := historyGet(t, s, schedule.StatisticsQuery{Genre: "comédie"})
	if split.Totals.Showtimes != 3 || split.Totals.Movies != 1 || split.TopMovies.ByShowtimes[0].Slug != fmt.Sprintf("film-%d", b) {
		t.Fatal("source reassignment", split.Totals)
	}
	historyExec(t, pool, `UPDATE public_movie_sources SET public_movie_id=$1 WHERE source_provider='ugc' AND source_movie_id='200'`, a)
	split = historyGet(t, s, schedule.StatisticsQuery{})
	if split.Totals.Movies != 5 {
		t.Fatal("split distinct count", split.Totals)
	}
	historyCount(t, pool, `SELECT count(*) FROM screening_history_showtimes`, 6)
	// One snapshot even while a second connection publishes and changes classification.
	err := s.historyRead(t.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var before, after []byte
		args := []any{"", "", []string{}, []string{}, "", "", "", "", ""}
		if err := tx.QueryRow(ctx, historyStatisticsSQL, args...).Scan(&before); err != nil {
			return err
		}
		if _, err := s.Replace(ctx, []Dataset{testDataset()}); err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, `UPDATE public_movie_metadata_overrides SET title='Changed concurrently' WHERE public_movie_id=$1`, a); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, historyStatisticsSQL, args...).Scan(&after); err != nil {
			return err
		}
		if string(before) != string(after) {
			return fmt.Errorf("history snapshot changed inside read transaction")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A nonflattened chain is an integrity failure, not silently dropped records.
	var c int64
	if err := pool.QueryRow(t.Context(), `SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='201'`).Scan(&c); err != nil {
		t.Fatal(err)
	}
	historyExec(t, pool, `UPDATE public_movies SET redirect_to_id=$1 WHERE id=$2`, c, b)
	historyExec(t, pool, `UPDATE public_movies SET redirect_to_id=$1 WHERE id=$2`, b, a)
	if _, err := s.HistoryStatistics(t.Context(), schedule.StatisticsQuery{}); err == nil {
		t.Fatal("nonflat redirect accepted")
	}
	if _, err := s.HistoryOptions(t.Context(), schedule.HistoryOptionsQuery{Kind: "genre"}); err == nil {
		t.Fatal("nonflat options accepted")
	}
}

func TestHistorySQLGoParityIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	for _, value := range []string{"Écran", "ΣΟΣ", "İ", "ẞ", "\u2003LILLE\u00a0"} {
		var lowered string
		if err := pool.QueryRow(t.Context(), `SELECT lower($1::text COLLATE pg_catalog.pg_c_utf8)`, value).Scan(&lowered); err != nil || lowered != strings.ToLower(value) {
			t.Fatalf("Unicode simple lowercase parity %q: %q %v", value, lowered, err)
		}
	}
	s := NewStore(pool)
	d := testDataset()
	d.Theaters[0].City = "Évreux"
	d.Theaters[1].City = "Evreux"
	d.Theaters[2].City = "\u2003LILLE\u00a0"
	d.Theaters[0].Name = "\u2003Écran\u00a0"
	d.Theaters[1].Name = "écran"
	d.Theaters[2].Name = "Écran"
	d.Showtimes[2].Language = schedule.LanguageVFSME
	d.Showtimes[2].ProviderVersion = "VF_SME"
	for i := range d.Showtimes {
		d.Showtimes[i].Movie.Title = "\u2003Égalité\u00a0"
		d.Showtimes[i].Movie.Genres = []string{"\u2003Émotion\u00a0", "émotion", "DRAME", "drame"}
	}
	historyPublish(t, s, d, cgrTestDataset())
	search, err := s.HistoryOptions(t.Context(), schedule.HistoryOptionsQuery{Kind: "theater", Q: "Écran"})
	if err != nil || len(search.Items) != 3 {
		t.Fatalf("Unicode option search: %+v %v", search, err)
	}
	source, err := NewPostgresSource(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(source, ServiceOptions{Now: func() time.Time { return time.Date(2026, 8, 15, 7, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []schedule.StatisticsQuery{{Date: "2026-08-15"}, {Date: "2026-08-15", Genre: "émotion"}, {Date: "2026-08-15", Language: "VF_SME"}, {Date: "2026-08-15", Language: "unknown"}} {
		upcoming, err := service.Statistics(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		history := historyGet(t, s, q)
		a, _ := json.Marshal(upcoming)
		b, _ := json.Marshal(history)
		var am, bm map[string]any
		if err := json.Unmarshal(a, &am); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &bm); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"options", "totals", "top_movies", "heatmap", "versions", "formats", "genres", "runtimes", "local", "concentration"} {
			if !reflect.DeepEqual(am[key], bm[key]) {
				t.Fatalf("SQL/Go parity %s query=%+v\nGo=%s\nSQL=%s", key, q, mustJSON(am[key]), mustJSON(bm[key]))
			}
		}
	}
	// Both repeated DST hours aggregate into the same Paris hour/service weekday.
	historyExec(t, pool, `UPDATE screening_history_showtimes SET service_date='2026-10-25',start_time=CASE WHEN provider_showing_id='100' THEN '2026-10-25 00:30Z'::timestamptz ELSE '2026-10-25 01:30Z'::timestamptz END,end_time=CASE WHEN provider_showing_id='100' THEN '2026-10-25 02:10Z'::timestamptz ELSE '2026-10-25 03:10Z'::timestamptz END WHERE provider='ugc' AND provider_showing_id IN ('100','104')`)
	dst := historyGet(t, s, schedule.StatisticsQuery{Date: "2026-10-25"})
	if dst.Totals.Showtimes != 2 || dst.Heatmap[6*24+2].ShowtimeCount != 2 {
		t.Fatal("DST/service day", dst.Totals)
	}
}

func mustJSON(value any) string { b, _ := json.Marshal(value); return string(b) }
