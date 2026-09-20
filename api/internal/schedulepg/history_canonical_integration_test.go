package schedulepg

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"messeances/api/internal/enrichment"
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
	for _, id := range []int64{a, b} {
		film := historyGet(t, s, schedule.StatisticsQuery{Film: fmt.Sprintf("film-%d", id)})
		assertHistorySums(t, film)
		if film.Totals != (schedule.StatisticsTotals{Showtimes: 3, Movies: 1, Cities: 3, Theaters: 3}) || film.TopMovies.ByShowtimes[0].Slug != fmt.Sprintf("film-%d", a) {
			t.Fatal("retained redirected sources excluded by film", film.Totals, film.TopMovies)
		}
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
		args := []any{"", "", []string{}, []string{}, "", "", "", "", "", nil}
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
	historyOriginalLanguage(t, pool, "200", 4200, "fr", d.GeneratedAt)
	d.Showtimes[0].Language = schedule.LanguageVF
	d.Showtimes[0].ProviderVersion = "VF"
	historyPublish(t, s, d)
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
	for _, q := range []schedule.StatisticsQuery{{Date: "2026-08-15"}, {Date: "2026-08-15", Genre: "émotion"}, {Date: "2026-08-15", Language: "VF_SME"}, {Date: "2026-08-15", Language: "unknown"}, {Date: "2026-08-15", Language: "VOF"}, {Date: "2026-08-15", Language: "VF"}} {
		upcoming, err := service.Statistics(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		history := historyGet(t, s, q)
		if (q.Language == "VOF" || q.Language == "VF") && history.Totals.Showtimes == 0 {
			t.Fatal("version parity must exercise nonempty results", q)
		}
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

func historyOriginalLanguage(t *testing.T, pool *pgxpool.Pool, source string, tmdbID int64, language string, now time.Time) {
	t.Helper()
	match := enrichment.Match{SourceProvider: enrichment.SourceUGC, SourceMovieID: source, MetadataProvider: enrichment.ProviderTMDB, Status: enrichment.StatusMatched, MetadataMovieID: tmdbID, Score: 1, NormalizedSourceTitle: "fixture", SourceRuntimeMinutes: 100, Candidates: []enrichment.Candidate{{ID: tmdbID, Title: "Fixture", Runtime: 100, Score: 1}}, EvaluatedAt: now, RetryAfter: now.Add(30 * 24 * time.Hour)}
	metadata := enrichment.Metadata{Provider: enrichment.ProviderTMDB, ProviderMovieID: tmdbID, Locale: enrichment.LocaleFrench, ProviderTitle: "Fixture", LocalizedTitle: "Fixture", OriginalLanguage: language, RuntimeMinutes: 100, Genres: []string{}, FetchedAt: now, RefreshAfter: now.Add(30 * 24 * time.Hour)}
	if err := enrichment.NewPostgresStore(pool).Publish(t.Context(), match, metadata); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryStatisticsVOFIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	// Exercise raw normalization without inserting query-only VOF or invalid UGC
	// empty versions into constrained storage. Silent providers can store empties.
	for _, language := range []string{"", "VOF"} {
		var got string
		if err := pool.QueryRow(t.Context(), `SELECT `+historyEffectiveLanguageSQL+` FROM (VALUES($1::text)) h(language) CROSS JOIN (VALUES('fr'::text)) s(original_language)`, language).Scan(&got); err != nil || got != "unknown" {
			t.Fatalf("raw language=%q normalized=%q err=%v", language, got, err)
		}
	}
	s := NewStore(pool)
	d := testDataset()
	seed := d.Showtimes[0]
	d.Theaters = d.Theaters[:1]
	d.Showtimes = nil
	for i, tc := range []struct {
		movie    string
		language schedule.Language
	}{
		{"200", schedule.LanguageVF}, {"201", schedule.LanguageVF}, {"202", schedule.LanguageVF}, {"203", schedule.LanguageVF},
		{"200", schedule.LanguageVFSME}, {"200", schedule.LanguageVFSTF},
		{"200", schedule.LanguageVO}, {"200", schedule.LanguageVOSTFR},
		{"200", "SPANISH"}, {"200", "INVENTED"}, {"201", schedule.LanguageVO}, {"202", schedule.LanguageVOSTFR},
	} {
		showing := seed
		showing.ID, showing.ProviderShowingID = fmt.Sprintf("ugc-showing-%d", 900+i), fmt.Sprint(900+i)
		showing.BookingURL = "https://www.ugc.fr/reservationSeances.html?id=" + showing.ProviderShowingID
		showing.Movie.ProviderID, showing.Movie.Slug = tc.movie, "ugc-film-"+tc.movie
		showing.Language, showing.ProviderVersion = tc.language, string(tc.language)
		d.Showtimes = append(d.Showtimes, showing)
	}
	historyPublish(t, s, d)
	for i, language := range []string{"fr", "en", "", "fr"} {
		historyOriginalLanguage(t, pool, fmt.Sprint(200+i), int64(4200+i), language, d.GeneratedAt)
	}
	var french, target, redirected int64
	for source, id := range map[string]*int64{"200": &french, "201": &target, "203": &redirected} {
		if err := pool.QueryRow(t.Context(), `SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id=$1`, source).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	historyExec(t, pool, `UPDATE public_movies SET redirect_to_id=$1 WHERE id=$2`, target, redirected)
	historyExec(t, pool, `UPDATE public_movie_sources SET public_movie_id=$1 WHERE public_movie_id=$2`, target, redirected)
	historyExec(t, pool, `UPDATE movie_slug_aliases SET public_movie_id=$1 WHERE public_movie_id=$2`, target, redirected)
	// Conflicting source caches must not override canonical French, English or NULL.
	historyExec(t, pool, `UPDATE movie_metadata_cache SET original_language=CASE WHEN provider_movie_id=4200 THEN 'en' ELSE 'fr' END WHERE provider_movie_id BETWEEN 4200 AND 4203`)
	storedRows := func() string {
		var rows string
		if err := pool.QueryRow(t.Context(), `SELECT jsonb_agg(to_jsonb(h) ORDER BY provider,provider_showing_id)::text FROM screening_history_showtimes h`).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	before := storedRows()
	var baseline schedule.HistoryStatistics
	check := func(vof, vf int, parity bool) {
		t.Helper()
		all := historyGet(t, s, schedule.StatisticsQuery{})
		assertHistorySums(t, all)
		counts := map[string]int{"VF": vf, "VF_SME": 1, "VFSTF": 1, "VO": 2, "VOSTFR": 2, "unknown": 2}
		if vof > 0 {
			counts["VOF"] = vof
		}
		if all.Totals.Showtimes != 12 || vf+vof != 4 || len(all.Versions) != len(counts) || slices.Contains(all.Options.Languages, "VOF") != (vof > 0) {
			t.Fatal("partition/options", all.Totals, all.Versions, all.Options.Languages)
		}
		for _, bucket := range all.Versions {
			label := bucket.Value
			if label == "unknown" {
				label = "Non renseigné"
			}
			if bucket.Count != counts[bucket.Value] || bucket.Label != label {
				t.Fatal("bucket", bucket)
			}
		}
		if baseline.Totals.Showtimes == 0 {
			baseline = all
		} else {
			fields := historyAggregateFields(t, all)
			for key, expected := range historyAggregateFields(t, baseline) {
				if key != "versions" && key != "options" && !reflect.DeepEqual(fields[key], expected) {
					t.Fatal("metadata changed unrelated aggregate", key)
				}
			}
		}
		var service *schedule.Service
		if parity {
			source, err := NewPostgresSource(t.Context(), s)
			if err != nil {
				t.Fatal(err)
			}
			service, err = NewService(source, ServiceOptions{Now: func() time.Time { return time.Date(2026, 8, 15, 7, 0, 0, 0, time.UTC) }})
			if err != nil {
				t.Fatal(err)
			}
		}
		queries := []schedule.StatisticsQuery{{}, {Language: "VOF", Date: "2026-09-01"}, {Language: "VOF", Film: "absent"}, {Language: "VOF", City: []string{"absent"}}, {Language: "VOF", Film: fmt.Sprintf("film-%d", redirected)}}
		for _, language := range []string{"VOF", "VF", "VF_SME", "VFSTF", "VO", "VOSTFR", "unknown"} {
			queries = append(queries, schedule.StatisticsQuery{Language: language})
		}
		for _, query := range queries {
			got := historyGet(t, s, query)
			assertHistorySums(t, got)
			if !reflect.DeepEqual(got.Options, all.Options) {
				t.Fatal("filters changed global options", query)
			}
			if query.Language != "" && query.Date == "" && query.Film == "" && len(query.City) == 0 && got.Totals.Showtimes != counts[query.Language] {
				t.Fatal("exact version filter", query, got.Totals)
			}
			if (query.Date != "" || query.Film == "absent" || len(query.City) > 0) && got.Totals.Showtimes != 0 {
				t.Fatal("empty intersection matched", query, got.Totals)
			}
			if parity {
				upcoming, err := service.Statistics(t.Context(), query)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(historyAggregateFields(t, upcoming), historyAggregateFields(t, got)) {
					t.Fatalf("SQL/Go VOF parity query=%+v\nGo=%s\nSQL=%s", query, mustJSON(upcoming), mustJSON(got))
				}
			}
		}
		if storedRows() != before {
			t.Fatal("statistics rewrote retained screenings")
		}
	}
	check(1, 3, true)
	historyExec(t, pool, `UPDATE public_movies SET original_language='fr' WHERE id=$1`, target)
	check(3, 1, true)
	historyExec(t, pool, `UPDATE public_movies SET original_language=NULL WHERE id=$1`, target)
	check(1, 3, true)
	// Retained screenings survive live-generation pruning and reclassify on reads.
	empty := d
	empty.Theaters, empty.Showtimes = nil, nil
	for range 3 {
		historyPublish(t, s, empty)
	}
	historyCount(t, pool, `SELECT count(*) FROM showtimes`, 0)
	// History also resolves still-redirected associations after live rows are gone.
	// Fixture publications reconcile metadata from caches; restore the conflicting
	// current metadata before testing read-time classification of retained rows.
	historyExec(t, pool, `UPDATE public_movies SET original_language=CASE WHEN id IN ($1,$2) THEN 'fr' ELSE NULL END`, french, redirected)
	historyExec(t, pool, `UPDATE public_movie_sources SET public_movie_id=$1 WHERE source_provider='ugc' AND source_movie_id='203'`, redirected)
	check(1, 3, false)
	historyExec(t, pool, `UPDATE public_movies SET original_language='fr' WHERE id=$1`, target)
	check(3, 1, false)
	historyExec(t, pool, `UPDATE public_movies SET original_language=NULL WHERE id=$1`, target)
	check(1, 3, false)
	historyExec(t, pool, `UPDATE public_movies SET original_language='en' WHERE id=$1`, french)
	check(0, 4, false)
	historyExec(t, pool, `UPDATE public_movies SET original_language='fr' WHERE id=$1`, french)
	check(1, 3, false)
	// A later source reassignment uses the new canonical language, not history-time metadata.
	historyExec(t, pool, `UPDATE public_movie_sources SET public_movie_id=$1 WHERE source_provider='ugc' AND source_movie_id='203'`, french)
	baseline = schedule.HistoryStatistics{}
	check(2, 2, false)
}

func historyAggregateFields(t *testing.T, value any) map[string]any {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal([]byte(mustJSON(value)), &fields); err != nil {
		t.Fatal(err)
	}
	for key := range fields {
		if !slices.Contains([]string{"options", "totals", "top_movies", "heatmap", "versions", "formats", "genres", "runtimes", "local", "concentration"}, key) {
			delete(fields, key)
		}
	}
	return fields
}

func mustJSON(value any) string { b, _ := json.Marshal(value); return string(b) }

func TestHistoryFilmIdentityIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	historyPublish(t, s, testDataset(), kinepolisTestDataset())
	var a, b, empty int64
	for provider, target := range map[string]*int64{"ugc": &a, "kinepolis": &b} {
		if err := pool.QueryRow(t.Context(), `SELECT public_movie_id FROM public_movie_sources WHERE source_provider=$1 AND source_movie_id IN ('200','HO200')`, provider).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	// Model a completed merge, retaining the old public ID as a redirect.
	historyExec(t, pool, `UPDATE public_movies SET redirect_to_id=$1 WHERE id=$2`, a, b)
	historyExec(t, pool, `UPDATE public_movie_sources SET public_movie_id=$1 WHERE public_movie_id=$2`, a, b)
	historyExec(t, pool, `UPDATE movie_slug_aliases SET public_movie_id=$1 WHERE public_movie_id=$2`, a, b)
	historyExec(t, pool, `UPDATE public_movies SET genres=ARRAY['Drame','Action'] WHERE id=$1`, a)
	if err := pool.QueryRow(t.Context(), `INSERT INTO public_movies(identity_anchor_provider,identity_anchor_source_movie_id,title,runtime_minutes) VALUES('ugc','999999','No sessions',100) RETURNING id`).Scan(&empty); err != nil {
		t.Fatal(err)
	}
	alias := "É &+/?#,alias"
	for _, slug := range []string{alias, "film-01"} {
		historyExec(t, pool, `INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind) VALUES($1,$2,'local')`, slug, a)
	}
	source, err := NewPostgresSource(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(source, ServiceOptions{Now: func() time.Time { return time.Date(2026, 8, 15, 7, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	film := fmt.Sprintf("film-%d", a)
	redirect := fmt.Sprintf("film-%d", b)
	base := historyGet(t, s, schedule.StatisticsQuery{})
	for _, tc := range []struct {
		query schedule.StatisticsQuery
		count int
	}{
		{schedule.StatisticsQuery{Film: film}, 3},
		{schedule.StatisticsQuery{Film: redirect}, 3},
		{schedule.StatisticsQuery{Film: " " + film + " "}, 3},
		{schedule.StatisticsQuery{Film: alias}, 3},
		{schedule.StatisticsQuery{Film: "film-01"}, 3},
		{schedule.StatisticsQuery{Film: "ugc-film-200"}, 3},
		{schedule.StatisticsQuery{Film: "kinepolis-film-HO200"}, 3},
		{schedule.StatisticsQuery{Film: fmt.Sprintf("film-%d", empty)}, 0},
		{schedule.StatisticsQuery{Film: "film-9223372036854775807"}, 0},
		{schedule.StatisticsQuery{Film: "film-9223372036854775808"}, 0},
		{schedule.StatisticsQuery{Film: "film-" + strings.Repeat("9", 195)}, 0},
		{schedule.StatisticsQuery{Film: "film-0"}, 0},
		{schedule.StatisticsQuery{Film: "film-+1"}, 0},
		{schedule.StatisticsQuery{Film: strings.ToUpper(film)}, 0},
		{schedule.StatisticsQuery{Film: "Film A"}, 0},
		{schedule.StatisticsQuery{Film: "200"}, 0},
		{schedule.StatisticsQuery{Film: "missing' OR true--"}, 0},
		{schedule.StatisticsQuery{Film: film + "," + redirect}, 0},
		{schedule.StatisticsQuery{Film: film, City: []string{"lille"}}, 1},
		{schedule.StatisticsQuery{Film: film, Theater: []string{"ugc-26"}}, 1},
		{schedule.StatisticsQuery{Film: film, Chain: "kinepolis"}, 1},
		{schedule.StatisticsQuery{Film: film, Language: "VOSTFR"}, 2},
		{schedule.StatisticsQuery{Film: film, Format: "IMAX"}, 1},
		{schedule.StatisticsQuery{Film: film, Genre: "drame"}, 3},
		{schedule.StatisticsQuery{Film: film, Pass: "UGC_ILLIMITE"}, 2},
		{schedule.StatisticsQuery{Film: film, City: []string{"lyon"}}, 0},
		{schedule.StatisticsQuery{Film: film, Date: "2026-08-16"}, 0},
		{schedule.StatisticsQuery{Film: redirect, City: []string{"lille"}, Theater: []string{"ugc-25"}, Chain: "ugc", Language: "VOSTFR", Format: "2D", Genre: "action", Pass: "UGC_ILLIMITE"}, 1},
	} {
		t.Run(fmt.Sprintf("%+v", tc.query), func(t *testing.T) {
			got := historyGet(t, s, tc.query)
			assertHistorySums(t, got)
			if got.Totals.Showtimes != tc.count || tc.count > 0 && (got.Totals.Movies != 1 || got.TopMovies.ByShowtimes[0].Slug != film) {
				t.Fatal(got.Totals, got.TopMovies)
			}
			if !reflect.DeepEqual(got.Options, base.Options) || !reflect.DeepEqual(got.Coverage, base.Coverage) || !reflect.DeepEqual(got.Limits, base.Limits) || tc.query.Date == "" && !reflect.DeepEqual(got.Range, base.Range) {
				t.Fatal("film changed inventory, coverage, limits or all-history range")
			}
			q := tc.query
			if q.Date == "" {
				q.Date = "2026-08-15"
			}
			upcoming, err := service.Statistics(t.Context(), q)
			if err != nil {
				t.Fatal(err)
			}
			var snapshot, history map[string]any
			if err := json.Unmarshal([]byte(mustJSON(upcoming)), &snapshot); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(mustJSON(got)), &history); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"options", "totals", "top_movies", "heatmap", "versions", "formats", "genres", "runtimes", "local", "concentration"} {
				if !reflect.DeepEqual(snapshot[key], history[key]) {
					t.Fatalf("SQL/Go film parity %s: Go=%s SQL=%s", key, mustJSON(snapshot[key]), mustJSON(history[key]))
				}
			}
		})
	}
	// Aliases pointing at redirect rows are ineligible, not recursively resolved.
	historyExec(t, pool, `INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind) VALUES('ineligible',$1,'local')`, b)
	if got := historyGet(t, s, schedule.StatisticsQuery{Film: "ineligible"}); got.Totals != (schedule.StatisticsTotals{}) {
		t.Fatal("ineligible alias matched", got.Totals)
	}
	// Identity selection and aggregation share the same repeatable-read snapshot.
	err = s.historyRead(t.Context(), func(ctx context.Context, tx pgx.Tx) error {
		before, err := historyFilmID(ctx, tx, alias)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, `UPDATE movie_slug_aliases SET public_movie_id=$1 WHERE slug=$2`, empty, alias); err != nil {
			return err
		}
		after, err := historyFilmID(ctx, tx, alias)
		if err != nil {
			return err
		}
		if before == nil || after == nil || *before != a || *after != a {
			return fmt.Errorf("film resolution changed within history transaction")
		}
		var raw []byte
		if err := tx.QueryRow(ctx, historyStatisticsSQL, "", "", []string{}, []string{}, "", "", "", "", "", after).Scan(&raw); err != nil {
			return err
		}
		var got schedule.HistoryStatistics
		if err := decodeHistoryJSON(raw, &got); err != nil {
			return err
		}
		if got.Totals.Showtimes != 3 {
			return fmt.Errorf("film aggregate changed within history transaction")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := historyGet(t, s, schedule.StatisticsQuery{Film: alias}); got.Totals.Showtimes != 0 {
		t.Fatal("next request did not see retargeted alias")
	}
}
