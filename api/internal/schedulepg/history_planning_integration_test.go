package schedulepg

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"messeances/api/internal/schedule"
)

func TestHistoryPreparedReuseIntegration(t *testing.T) {
	fixture := newHistoryPool(t)
	historyPublish(t, NewStore(fixture), testDataset(), kinepolisTestDataset())
	config := fixture.Config()
	config.MaxConns = 1 // Every execution must reuse the same prepared statements.
	config.ConnConfig.RuntimeParams["plan_cache_mode"] = "auto"
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	s := NewStore(pool)
	for _, query := range []schedule.StatisticsQuery{
		{},
		{Chain: "ugc", Language: "VF", Format: "2D", Pass: "UGC_ILLIMITE"},
		{City: []string{"lille", "lyon"}, Theater: []string{"ugc-25", "ugc-26"}},
		{Film: "ugc-film-200"},
		{Film: "unknown-film"},
	} {
		var want schedule.HistoryStatistics
		for i := range 8 {
			got := historyGet(t, s, query)
			got.GeneratedAt = time.Time{}
			if i == 0 {
				want = got
			} else if !reflect.DeepEqual(got, want) {
				t.Fatalf("prepared execution %d changed result for %+v", i+1, query)
			}
		}
	}
	for range 8 {
		if _, err := s.HistoryOptions(t.Context(), schedule.HistoryOptionsQuery{Kind: "theater"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, sql := range []string{historyStatisticsSQL, historyStatisticsOptionsSQL, historyOptionsSQL} {
		var generic, custom int64
		if err := pool.QueryRow(t.Context(), `SELECT generic_plans,custom_plans FROM pg_prepared_statements WHERE statement=$1`, sql).Scan(&generic, &custom); err != nil {
			t.Fatal(err)
		}
		if generic != 0 || custom < 8 {
			t.Fatalf("history statement reused generic=%d custom=%d", generic, custom)
		}
	}
	// SET LOCAL must not leak into unrelated pooled reads after commit or rollback.
	checkReset := func() {
		t.Helper()
		var mode string
		if err := pool.QueryRow(t.Context(), `SHOW plan_cache_mode`).Scan(&mode); err != nil || mode != "auto" {
			t.Fatalf("session planning mode=%q err=%v", mode, err)
		}
	}
	checkReset()
	wantErr := errors.New("fixture read failed")
	err = s.historyRead(t.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var mode string
		if err := tx.QueryRow(ctx, `SHOW plan_cache_mode`).Scan(&mode); err != nil {
			return err
		}
		if mode != "force_custom_plan" {
			t.Errorf("transaction planning mode=%q", mode)
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatal("read error was not preserved", err)
	}
	checkReset()
}

// Keep the prior row-level predicate as an independent equivalence oracle.
// CTE fixtures can model corrupt associations that foreign keys normally reject,
// without disabling constraints or mutating any business table.
const historyIntegrityReferenceSQL = `SELECT EXISTS (
 SELECT 1 FROM screening_history_showtimes h
 LEFT JOIN screening_history_theaters t ON (t.provider,t.id)=(h.provider,h.theater_id)
 LEFT JOIN public_movie_sources s ON (s.source_provider,s.source_movie_id)=(h.provider,h.movie_provider_id)
 LEFT JOIN public_movies p ON p.id=s.public_movie_id
 LEFT JOIN public_movies c ON c.id=coalesce(p.redirect_to_id,p.id)
 WHERE t.id IS NULL OR s.public_movie_id IS NULL OR c.id IS NULL OR c.redirect_to_id IS NOT NULL
)`

func TestHistoryIntegrityParityIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	const fixtures = `WITH screening_history_showtimes(provider,theater_id,movie_provider_id) AS (
 SELECT * FROM (VALUES ('ugc','ugc-1','1'),('ugc','ugc-1','1'),('ugc','ugc-2','2')) h WHERE NOT $1::boolean
), screening_history_theaters(provider,id) AS (
 VALUES ('ugc','ugc-1'),($2::text,$3::text)
), public_movie_sources(source_provider,source_movie_id,public_movie_id) AS (
 VALUES ('ugc','1',1::bigint),($4::text,'2',$5::bigint)
), public_movies(id,redirect_to_id) AS (
 SELECT * FROM (VALUES (1::bigint,NULL::bigint),(2::bigint,$6::bigint),(3::bigint,$7::bigint)) p(id,redirect_to_id) WHERE id<>$8::bigint
) SELECT (`
	for _, tc := range []struct {
		name   string
		args   []any
		broken bool
	}{
		{"valid duplicates", []any{false, "ugc", "ugc-2", "ugc", 2, nil, nil, 0}, false},
		{"empty history", []any{true, "", "", "", nil, nil, nil, 0}, false},
		{"missing theater", []any{false, "ugc", "", "ugc", 2, nil, nil, 0}, true},
		{"wrong theater provider", []any{false, "cgr", "ugc-2", "ugc", 2, nil, nil, 0}, true},
		{"missing source", []any{false, "ugc", "ugc-2", "", 2, nil, nil, 0}, true},
		{"wrong source provider", []any{false, "ugc", "ugc-2", "cgr", 2, nil, nil, 0}, true},
		{"null source association", []any{false, "ugc", "ugc-2", "ugc", nil, nil, nil, 0}, true},
		{"missing public movie", []any{false, "ugc", "ugc-2", "ugc", 2, nil, nil, 2}, true},
		{"valid one hop redirect", []any{false, "ugc", "ugc-2", "ugc", 2, 3, nil, 0}, false},
		{"missing redirect target", []any{false, "ugc", "ugc-2", "ugc", 2, 4, nil, 0}, true},
		{"nonflattened redirect", []any{false, "ugc", "ugc-2", "ugc", 2, 3, 1, 0}, true},
		{"self redirect", []any{false, "ugc", "ugc-2", "ugc", 2, 2, nil, 0}, true},
		{"redirect cycle", []any{false, "ugc", "ugc-2", "ugc", 2, 3, 2, 0}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before, after bool
			if err := pool.QueryRow(t.Context(), fixtures+historyIntegrityReferenceSQL+`), (`+historyIntegritySQL+`)`, tc.args...).Scan(&before, &after); err != nil {
				t.Fatal(err)
			}
			if before != tc.broken || after != tc.broken {
				t.Fatalf("integrity broken: original=%t candidate=%t want=%t", before, after, tc.broken)
			}
		})
	}
}
