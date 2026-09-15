package schedulepg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"messeances/api/internal/database"
	"messeances/api/internal/schedule"
)

func newHistoryPool(t *testing.T, tracers ...pgx.QueryTracer) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if strings.TrimSpace(url) == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	schema := "history_test_" + hex.EncodeToString(nonce)
	ctx := t.Context()
	bootstrap, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal("history bootstrap connection failed")
	}
	t.Cleanup(func() { _ = bootstrap.Close(context.Background()) })
	if _, err = bootstrap.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !strings.HasPrefix(schema, "history_test_") || len(schema) != 29 {
			t.Error("unsafe history cleanup rejected")
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := bootstrap.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal("history pool configuration failed")
	}
	config.ConnConfig.RuntimeParams["search_path"] = pgx.Identifier{schema}.Sanitize()
	config.MaxConns = 6
	if len(tracers) > 0 {
		config.ConnConfig.Tracer = tracers[0]
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	assert := func() {
		var got string
		if err := pool.QueryRow(context.Background(), `SELECT current_schema()`).Scan(&got); err != nil || got != schema {
			t.Fatal("history schema assertion", err)
		}
	}
	assert()
	t.Cleanup(assert)
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func historyExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatal(err)
	}
}
func historyCount(t *testing.T, pool *pgxpool.Pool, sql string, want int) {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), sql).Scan(&n); err != nil || n != want {
		t.Fatalf("count=%d want=%d err=%v", n, want, err)
	}
}
func historyPublish(t *testing.T, s *Store, datasets ...Dataset) {
	t.Helper()
	if _, err := s.Replace(t.Context(), datasets); err != nil {
		t.Fatal(err)
	}
}
func historyGet(t *testing.T, s *Store, q schedule.StatisticsQuery) schedule.HistoryStatistics {
	t.Helper()
	r, err := s.HistoryStatistics(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestHistoryRetentionIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	r := historyGet(t, s, schedule.StatisticsQuery{})
	if r.Range != nil || r.Coverage.CollectionStartedAt != nil || r.Totals.Showtimes != 0 {
		t.Fatal("nonempty start", r)
	}
	historyPublish(t, s, testDataset(), kinepolisTestDataset())
	// Emulate migration's empty history on an existing two-provider schedule.
	historyExec(t, pool, `DELETE FROM screening_history_showtimes;DELETE FROM screening_history_theaters;DELETE FROM screening_history_providers`)
	historyPublish(t, s, testDataset())
	historyCount(t, pool, `SELECT count(*) FROM screening_history_showtimes`, 5)
	historyCount(t, pool, `SELECT count(*) FROM screening_history_providers WHERE provider='kinepolis'`, 0)
	historyPublish(t, s, kinepolisTestDataset())
	var first, last, kine time.Time
	if err := pool.QueryRow(t.Context(), `SELECT first_seen_at,last_seen_at FROM screening_history_showtimes WHERE provider='ugc' AND provider_showing_id='100'`).Scan(&first, &last); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT last_publication_at FROM screening_history_providers WHERE provider='kinepolis'`).Scan(&kine); err != nil {
		t.Fatal(err)
	}
	data := testDataset()
	data.GeneratedAt = data.GeneratedAt.Add(-time.Hour)
	data.Showtimes[0].StartTime = data.Showtimes[0].StartTime.Add(time.Hour)
	data.Showtimes[0].EndTime = data.Showtimes[0].EndTime.Add(time.Hour)
	data.Showtimes[0].Movie = data.Showtimes[2].Movie
	data.Showtimes[0].Language = LanguageVF
	data.Showtimes[0].Format = FormatIMAX
	data.Showtimes[0].Room = "Corrected"
	data.Showtimes[0].BookingURL = "https://www.ugc.fr/reservationSeances.html?id=%31%30%30"
	data.Theaters[0].Name = "Renamed"
	data.Theaters[0].City = "Évreux"
	historyPublish(t, s, data)
	var ok bool
	if err := pool.QueryRow(t.Context(), `SELECT first_seen_at=$1 AND last_seen_at>$2 AND movie_provider_id='201' AND language='VF' AND format='IMAX' AND room='Corrected' AND source_generated_at=$3 FROM screening_history_showtimes WHERE provider='ugc' AND provider_showing_id='100'`, first, last, data.GeneratedAt).Scan(&ok); err != nil || !ok {
		t.Fatal("correction receipt", err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT last_publication_at=$1 FROM screening_history_providers WHERE provider='kinepolis'`, kine).Scan(&ok); err != nil || !ok {
		t.Fatal("copied provider reobserved", err)
	}
	historyPublish(t, s, data)
	historyCount(t, pool, `SELECT count(*) FROM screening_history_showtimes`, 6)
	// One identity reused in another theater and date is new; two IDs at one instant stay distinct.
	one := testDataset()
	one.Showtimes = one.Showtimes[:1]
	one.Showtimes[0].TheaterID = "ugc-26"
	historyPublish(t, s, one)
	one.Showtimes[0].ProviderShowingID = "900"
	one.Showtimes[0].ID = "ugc-showing-900"
	one.Showtimes[0].BookingURL = "https://www.ugc.fr/reservationSeances.html?id=900"
	historyPublish(t, s, one)
	one.Showtimes[0].ProviderShowingID = "100"
	one.Showtimes[0].ID = "ugc-showing-100"
	one.Showtimes[0].BookingURL = "https://www.ugc.fr/reservationSeances.html?id=100"
	one.Showtimes[0].ServiceDate = "2026-08-16"
	one.Showtimes[0].StartTime = one.Showtimes[0].StartTime.AddDate(0, 0, 1)
	one.Showtimes[0].EndTime = one.Showtimes[0].EndTime.AddDate(0, 0, 1)
	one.Window = Window{From: "2026-08-16", Through: "2026-08-16"}
	for i := range one.Theaters {
		one.Theaters[i].AvailableDates = []string{"2026-08-16"}
	}
	historyPublish(t, s, one)
	empty := testDataset()
	empty.Showtimes = nil
	empty.Theaters = nil
	for range 3 {
		historyPublish(t, s, empty)
	}
	historyCount(t, pool, `SELECT count(*) FROM movies WHERE provider='ugc'`, 0)
	historyCount(t, pool, `SELECT count(*) FROM screening_history_showtimes`, 9)
	historyCount(t, pool, `SELECT count(*) FROM screening_history_theaters`, 4)
	historyPublish(t, s, testDataset())
	historyCount(t, pool, `SELECT count(*) FROM screening_history_showtimes`, 9)
	r = historyGet(t, s, schedule.StatisticsQuery{})
	if r.Totals.Showtimes != 9 || r.Range.From != "2026-08-15" || r.Range.Through != "2026-08-16" {
		t.Fatal("retained totals", r.Totals, r.Range)
	}
	for _, sql := range []string{
		`UPDATE screening_history_showtimes SET last_seen_at=first_seen_at-interval '1 second'`,
		`UPDATE screening_history_showtimes SET last_generation=0`,
		`UPDATE screening_history_showtimes SET movie_provider_id='99999'`,
		`UPDATE screening_history_showtimes SET theater_id='ugc-99999'`,
		`UPDATE screening_history_theaters SET passes=ARRAY['UGC_ILLIMITE','UGC_ILLIMITE']`,
		`INSERT INTO screening_history_showtimes SELECT * FROM screening_history_showtimes`,
		`DELETE FROM public_movie_sources WHERE source_provider='ugc'`,
	} {
		if _, err := pool.Exec(t.Context(), sql); err == nil {
			t.Fatalf("invalid history accepted: %s", sql)
		}
	}
}

func TestHistoryRollbackIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	historyExec(t, pool, `CREATE FUNCTION reject_history() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture failure'; END $$; CREATE TRIGGER reject_history BEFORE INSERT OR UPDATE ON screening_history_providers FOR EACH ROW EXECUTE FUNCTION reject_history()`)
	if _, err := s.Replace(t.Context(), []Dataset{testDataset()}); err == nil {
		t.Fatal("initial publication succeeded")
	}
	historyCount(t, pool, `SELECT count(*) FROM screening_history_showtimes`, 0)
	historyCount(t, pool, `SELECT count(*) FROM public_movie_sources`, 0)
	historyCount(t, pool, `SELECT count(*) FROM schedule_snapshot`, 0)
	historyExec(t, pool, `DROP TRIGGER reject_history ON screening_history_providers`)
	historyPublish(t, s, testDataset())
	historyPublish(t, s, testDataset())
	var before string
	fingerprint := `SELECT jsonb_build_array((SELECT jsonb_agg(s ORDER BY version) FROM schedule_snapshot s),(SELECT jsonb_agg(s ORDER BY generation_id,id) FROM showtimes s),(SELECT jsonb_agg(s ORDER BY source_provider,source_movie_id) FROM public_movie_sources s),(SELECT jsonb_agg(s ORDER BY provider,provider_showing_id) FROM screening_history_showtimes s),(SELECT jsonb_agg(s ORDER BY provider) FROM screening_history_providers s))::text`
	if err := pool.QueryRow(t.Context(), fingerprint).Scan(&before); err != nil {
		t.Fatal(err)
	}
	historyExec(t, pool, `CREATE TRIGGER reject_history BEFORE INSERT OR UPDATE ON screening_history_providers FOR EACH ROW EXECUTE FUNCTION reject_history()`)
	data := testDataset()
	data.Showtimes[0].Movie.ProviderID = "999"
	data.Showtimes[0].Movie.Slug = "ugc-film-999"
	data.Showtimes[0].Movie.Title = "New rollback movie"
	if _, err := s.Replace(t.Context(), []Dataset{data}); err == nil {
		t.Fatal("late publication succeeded")
	}
	var after string
	if err := pool.QueryRow(t.Context(), fingerprint).Scan(&after); err != nil || after != before {
		t.Fatal("publication/prune/reconcile/history rollback", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Replace(ctx, []Dataset{data}); err == nil {
		t.Fatal("canceled publication succeeded")
	}
}

func assertHistorySums(t *testing.T, r schedule.HistoryStatistics) {
	t.Helper()
	sum := 0
	for _, v := range r.Heatmap {
		sum += v.ShowtimeCount
	}
	if len(r.Heatmap) != 168 || sum != r.Totals.Showtimes {
		t.Fatal("heatmap totals", sum, r.Totals)
	}
	for _, buckets := range [][]schedule.StatisticsCountBucket{r.Versions, r.Formats} {
		sum = 0
		for _, b := range buckets {
			sum += b.Count
		}
		if sum != r.Totals.Showtimes {
			t.Fatal("bucket totals")
		}
	}
	sum = 0
	for _, b := range r.Runtimes {
		sum += b.Count
	}
	if len(r.Runtimes) != 4 || sum != r.Totals.Movies {
		t.Fatal("runtime partition")
	}
	if r.Concentration.TopShowtimeCount+r.Concentration.OtherShowtimeCount != r.Totals.Showtimes {
		t.Fatal("concentration totals")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"booking_url", "provider_showing_id", "reservationSeances", "source_movie_id"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("raw evidence exposed", secret)
		}
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	var walk func(any, string)
	walk = func(v any, path string) {
		switch v := v.(type) {
		case map[string]any:
			for k, x := range v {
				if x == nil && k != "range" && k != "recorded_window" && k != "collection_started_at" && k != "last_publication_at" {
					t.Fatal("unexpected null", path+k)
				}
				walk(x, path+k+".")
			}
		case []any:
			for _, x := range v {
				walk(x, path)
			}
		}
	}
	walk(value, "")
}

func TestHistoryStatisticsIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	empty := testDataset()
	empty.Theaters = nil
	empty.Showtimes = nil
	historyPublish(t, s, empty)
	r := historyGet(t, s, schedule.StatisticsQuery{})
	assertHistorySums(t, r)
	if r.Coverage.CollectionStartedAt == nil || r.Range != nil || len(r.Coverage.Providers) != 1 {
		t.Fatal("successful empty coverage")
	}
	r = historyGet(t, s, schedule.StatisticsQuery{Date: "0001-01-01", DateTo: "9999-12-31"})
	if r.Range.From != "0001-01-01" {
		t.Fatal("explicit empty range")
	}
	data := testDataset()
	for i := range data.Showtimes {
		data.Showtimes[i].Movie.Genres = []string{"\u2003Drame\u00a0", "drame", "Émotion"}
	}
	historyPublish(t, s, data, kinepolisTestDataset(), cgrTestDataset())
	r = historyGet(t, s, schedule.StatisticsQuery{})
	assertHistorySums(t, r)
	if r.Totals.Showtimes != 7 || r.Totals.Movies != 6 {
		t.Fatal("all-recorded totals", r.Totals)
	}
	for _, tc := range []struct {
		q schedule.StatisticsQuery
		n int
	}{
		{schedule.StatisticsQuery{City: []string{"lille", "lyon", "lille"}}, 4},
		{schedule.StatisticsQuery{Theater: []string{"ugc-25", "ugc-99"}, Language: "VF"}, 2},
		{schedule.StatisticsQuery{City: []string{"lyon"}, Theater: []string{"ugc-25"}}, 0},
		{schedule.StatisticsQuery{Chain: "ugc", Pass: "UGC_ILLIMITE", Genre: " DRAME "}, 5},
		{schedule.StatisticsQuery{Language: "unknown"}, 1},
		{schedule.StatisticsQuery{Genre: "unknown"}, 1},
		{schedule.StatisticsQuery{Format: "IMAX"}, 1},
		{schedule.StatisticsQuery{Pass: "unknown"}, 0},
		{schedule.StatisticsQuery{City: []string{"not-a-city"}}, 0},
		{schedule.StatisticsQuery{Date: "1900-01-01", DateTo: "2100-12-31"}, 7},
		{schedule.StatisticsQuery{Date: "2026-08-16"}, 0},
	} {
		filtered := historyGet(t, s, tc.q)
		assertHistorySums(t, filtered)
		if filtered.Totals.Showtimes != tc.n {
			t.Fatalf("filter=%+v totals=%+v want=%d", tc.q, filtered.Totals, tc.n)
		}
		if !reflect.DeepEqual(filtered.Options, r.Options) || !reflect.DeepEqual(filtered.Coverage, r.Coverage) {
			t.Fatal("filters changed inventory/coverage")
		}
	}
	if r.Heatmap[(6-1)*24].ShowtimeCount != 1 {
		t.Fatal("after-midnight service weekday")
	}
	for _, q := range []schedule.HistoryOptionsQuery{{Kind: "theater", Q: "LILLE"}, {Kind: "city", Selected: []string{"lyon", "lille", "lyon", "missing"}, Q: "not-found"}, {Kind: "genre", Q: "%"}, {Kind: "genre", Q: "_"}} {
		options, err := s.HistoryOptions(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		if q.Q == "not-found" && (len(options.Items) != 0 || len(options.Selected) != 2 || options.Selected[0].Value != "lyon") {
			t.Fatal("selection lookup", options)
		}
		if (q.Q == "%" || q.Q == "_") && len(options.Items) != 0 {
			t.Fatal("wildcard expanded")
		}
		if q.Q == "LILLE" && len(options.Items) != 2 {
			t.Fatal("theater city search", options)
		}
	}
}

func TestHistoryBudgetsIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan error, 2)
	for range 2 {
		go func() {
			done <- s.historyRead(t.Context(), func(ctx context.Context, tx pgx.Tx) error {
				entered <- struct{}{}
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
	}
	<-entered
	<-entered
	if _, err := s.HistoryStatistics(t.Context(), schedule.StatisticsQuery{}); !errors.Is(err, schedule.ErrHistoryBusy) {
		t.Fatal("statistics admission", err)
	}
	if _, err := s.HistoryOptions(t.Context(), schedule.HistoryOptionsQuery{Kind: "city"}); !errors.Is(err, schedule.ErrHistoryBusy) {
		t.Fatal("shared options admission", err)
	}
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	err := s.historyRead(t.Context(), func(ctx context.Context, tx pgx.Tx) error { _, err := tx.Exec(ctx, `SELECT pg_sleep(5)`); return err })
	if !errors.Is(err, schedule.ErrHistoryQueryTimeout) || time.Since(start) > 3*time.Second {
		t.Fatal("statement deadline", err, time.Since(start))
	}
	connections := make([]*pgxpool.Conn, 0, 6)
	for range 6 {
		c, err := pool.Acquire(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, c)
	}
	start = time.Now()
	_, err = s.HistoryOptions(t.Context(), schedule.HistoryOptionsQuery{Kind: "city"})
	for _, c := range connections {
		c.Release()
	}
	if !errors.Is(err, schedule.ErrHistoryQueryTimeout) || time.Since(start) > 4*time.Second {
		t.Fatal("pool-inclusive deadline", err, time.Since(start))
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.HistoryStatistics(ctx, schedule.StatisticsQuery{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation", err)
	}
	err = s.historyRead(t.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var isolation, readonly string
		if err := tx.QueryRow(ctx, `SELECT current_setting('transaction_isolation'),current_setting('transaction_read_only')`).Scan(&isolation, &readonly); err != nil {
			return err
		}
		if isolation != "repeatable read" || readonly != "on" {
			return fmt.Errorf("transaction mode %s/%s", isolation, readonly)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
