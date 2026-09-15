package schedulepg

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"messeances/api/internal/schedule"
)

type historyQueryCounter struct{ count atomic.Int32 }

func (c *historyQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.count.Add(1)
	return ctx
}
func (c *historyQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// Deliberately synthetic growth evidence, not a production-latency claim.
func TestHistoryPerformanceIntegration(t *testing.T) {
	counter := &historyQueryCounter{}
	pool := newHistoryPool(t, counter)
	s := NewStore(pool)
	historyExec(t, pool, `INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes,genres)
 SELECT 'ugc',n::text,'Film '||lpad(n::text,4,'0'),CASE WHEN n%4=0 THEN 0 WHEN n%4=1 THEN 89 WHEN n%4=2 THEN 120 ELSE 121 END,ARRAY['Genre '||lpad(n::text,4,'0'),'Commun'] FROM generate_series(1000,1255) n;
 INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes,genres)
 SELECT 'ugc',identity_anchor_source_movie_id,id,'ugc-film-'||identity_anchor_source_movie_id,title,runtime_minutes,genres FROM public_movies;
 INSERT INTO screening_history_theaters(id,provider,provider_id,slug,name,address,city,postal_code,city_slug,city_name,passes,last_observed_at)
 SELECT 'ugc-'||n,'ugc',n::text,'ugc-'||n,'Cinéma '||lpad(n::text,4,'0'),'Fixture address','Ville '||n,'59000','ville-'||n,'Ville '||n,ARRAY['UGC_ILLIMITE'],now() FROM generate_series(1000,1127) n;
 INSERT INTO screening_history_providers(provider,collection_started_at,last_publication_at,last_generation,source_generated_at) VALUES('ugc',now(),now(),1,now());
 INSERT INTO screening_history_showtimes(id,provider,provider_showing_id,theater_id,movie_provider_id,service_date,start_time,end_time,first_part_duration_minutes,language,provider_version,format,room,booking_url,first_seen_at,last_seen_at,source_generated_at,last_generation)
 SELECT 'ugc-showing-'||n,'ugc',n::text,'ugc-'||(1000+n%128),(1000+n%256)::text,'2010-01-01'::date+n%8000,
 '2010-01-01 18:00Z'::timestamptz+(n%8000)*interval '1 day','2010-01-01 20:00Z'::timestamptz+(n%8000)*interval '1 day',0,
 CASE WHEN n%2=0 THEN 'VF' ELSE 'VOSTFR' END,CASE WHEN n%2=0 THEN 'VF' ELSE 'VOSTFR' END,'2D','1','https://www.ugc.fr/reservationSeances.html?id='||n,now(),now(),now(),1 FROM generate_series(1,105000) n;
 ANALYZE screening_history_showtimes; ANALYZE screening_history_theaters; ANALYZE public_movies; ANALYZE public_movie_sources;`)
	historyCount(t, pool, `SELECT count(*) FROM screening_history_showtimes`, 105000)
	cases := []struct {
		name string
		q    schedule.StatisticsQuery
	}{
		{"all-recorded", schedule.StatisticsQuery{}},
		{"selected-outside-initial", schedule.StatisticsQuery{Theater: []string{"ugc-1127"}, City: []string{"ville-1127"}, Genre: "genre 1255"}},
		{"single-day", schedule.StatisticsQuery{Date: "2018-01-01"}},
		{"long-period", schedule.StatisticsQuery{Date: "2010-01-01", DateTo: "2035-12-31", Language: "VF", Pass: "UGC_ILLIMITE"}},
		{"contradictory", schedule.StatisticsQuery{City: []string{"ville-1000"}, Theater: []string{"ugc-1001"}}},
	}
	for _, tc := range cases {
		counter.count.Store(0)
		start := time.Now()
		r := historyGet(t, s, tc.q)
		elapsed := time.Since(start)
		queries := counter.count.Load()
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("synthetic rows=105000 case=%s elapsed=%s queries=%d response_bytes=%d totals=%+v", tc.name, elapsed, queries, len(raw), r.Totals)
		if queries > 10 || elapsed >= 3*time.Second || len(raw) > 250000 {
			t.Fatal("history cost boundary", queries, elapsed, len(raw))
		}
		assertHistorySums(t, r)
		if tc.name == "all-recorded" {
			if r.Totals.Showtimes != 105000 || r.Totals.Movies != 256 || r.Totals.Cities != 128 || r.Totals.Theaters != 128 {
				t.Fatal("full totals truncated", r.Totals)
			}
			if len(r.Genres) != 100 || len(r.Local.Cities) != 100 || len(r.Local.Theaters) != 100 || len(r.Options.Genres) != 100 || len(r.Options.Cities) != 100 || !r.Limits.Genres || !r.Limits.Local.Cities || !r.Limits.Local.Theaters || !r.Limits.Options.Genres || !r.Limits.Options.Cities || !r.Limits.Options.Theaters {
				t.Fatal("limit contract", r.Limits)
			}
			if len(r.TopMovies.ByShowtimes) != 10 || r.Concentration.OtherShowtimeCount <= 0 {
				t.Fatal("top10 truncation")
			}
			// The first 40 theaters have one extra screening. Within both tied
			// groups the display-name order must survive weighted aggregation.
			for _, boundary := range []struct{ position, id int }{{0, 1001}, {39, 1040}, {40, 1000}, {99, 1099}} {
				if r.Local.Theaters[boundary.position].ID != fmt.Sprintf("ugc-%d", boundary.id) || r.Local.Cities[boundary.position].Slug != fmt.Sprintf("ville-%d", boundary.id) {
					t.Fatal("top100 tied order", boundary, r.Local)
				}
			}
			for i, movie := range r.TopMovies.ByShowtimes {
				if movie.Title != fmt.Sprintf("Film %04d", 1001+i) || movie.ShowtimeCount != 411 || movie.TheaterCount != 1 {
					t.Fatal("top10 weighted order", i, movie)
				}
			}
		}
		if tc.name == "selected-outside-initial" {
			if len(r.Options.Theaters) != 101 || len(r.Options.Cities) != 101 || len(r.Options.Genres) != 101 || !r.Limits.Options.Theaters || r.Totals.Showtimes == 0 {
				t.Fatal("selected option union", len(r.Options.Theaters), len(r.Options.Cities), len(r.Options.Genres), r.Totals)
			}
		}
	}
	for _, q := range []schedule.HistoryOptionsQuery{{Kind: "city"}, {Kind: "theater", Q: "1127", Selected: []string{"ugc-1127", "ugc-1000", "unknown"}}, {Kind: "genre", Q: "genre", Selected: []string{"genre 1255"}}} {
		counter.count.Store(0)
		start := time.Now()
		r, err := s.HistoryOptions(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("synthetic options kind=%s elapsed=%s queries=%d items=%d selected=%d has_more=%t", q.Kind, time.Since(start), counter.count.Load(), len(r.Items), len(r.Selected), r.HasMore)
		if counter.count.Load() > 10 || len(r.Items) > 100 || len(r.Selected) > 50 {
			t.Fatal("option cost boundary")
		}
		if q.Kind == "city" && (!r.HasMore || len(r.Items) != 100) {
			t.Fatal("city overflow")
		}
		if q.Kind == "theater" && (len(r.Items) != 1 || len(r.Selected) != 2 || r.Selected[0].Value != "ugc-1127") {
			t.Fatal("selected lookup", r)
		}
	}
	for _, tc := range []struct {
		name, sql string
		args      []any
	}{
		{"all-aggregate", historyStatisticsSQL, []any{"", "", []string{}, []string{}, "", "", "", "", ""}},
		{"selective-date", historyStatisticsSQL, []any{"2018-01-01", "2018-01-01", []string{}, []string{}, "", "", "", "", ""}},
		{"option-search", historyOptionsSQL, []any{"theater", "1127", []string{"ugc-1127"}}},
	} {
		rows, err := pool.Query(t.Context(), `EXPLAIN (ANALYZE,BUFFERS,FORMAT TEXT) `+tc.sql, tc.args...)
		if err != nil {
			t.Fatal(err)
		}
		t.Log("EXPLAIN", tc.name)
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			t.Log(line)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}
