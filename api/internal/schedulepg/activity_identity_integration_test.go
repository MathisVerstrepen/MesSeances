package schedulepg

import (
	"context"
	"messeances/api/internal/enrichment"
	"testing"
	"time"
)

func TestActivityCanonicalCertificateIntegration(t *testing.T) {
	for _, contaminated := range []bool{false, true} {
		name := "compatible"
		if contaminated {
			name = "positive_in_gap"
		}
		t.Run(name, func(t *testing.T) {
			pool := newHistoryPool(t)
			s := NewStore(pool)
			d := testDataset()
			d.Theaters = d.Theaters[:1]
			d.Showtimes = []ShowtimeRecord{d.Showtimes[0], d.Showtimes[2]}
			historyPublish(t, s, d)
			historyExec(t, pool, `UPDATE cinema_activity_episodes SET detected_at='2026-08-15 12:00Z'`)
			historyExec(t, pool, `UPDATE cinema_activity_episode_sources SET first_seen_at='2026-08-15 12:00Z',last_seen_at='2026-08-15 12:00Z'`)
			if contaminated {
				historyExec(t, pool, `UPDATE screening_history_showtimes SET service_date='2026-08-20' WHERE movie_provider_id='201'`)
				historyExec(t, pool, `UPDATE cinema_activity_episodes SET observed_through='2026-08-20' WHERE anchor_source_movie_id='201'`)
				historyExec(t, pool, `UPDATE cinema_activity_episode_sources SET observed_through='2026-08-20' WHERE source_movie_id='201'`)
			}
			historyExec(t, pool, `INSERT INTO cinema_activity_coverage(provider,generation,theater_id,service_date,status,basis,source_generated_at,detected_at)
 SELECT 'ugc',100,'ugc-25','2026-08-15'::date+n,'complete','date_response',(('2026-08-15'::date+n)+time '12:00') AT TIME ZONE 'Europe/Paris',(('2026-08-15'::date+n)+time '12:01') AT TIME ZONE 'Europe/Paris' FROM generate_series(1,28) n`)
			var id int64
			if err := pool.QueryRow(t.Context(), `SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='200'`).Scan(&id); err != nil {
				t.Fatal(err)
			}
			next := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
			tx, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if err := observeActivityGroup(t.Context(), tx, 101, next.Add(10*time.Hour), []activitySource{{theater: "ugc-25", provider: "ugc", id: "200", movieID: id, from: next, through: next, eligible: &next}}, false); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			before := activityRead(t, s, 20, "")
			if len(before.Items) != 1 {
				t.Fatal("seed certificate", before)
			}
			historyOriginalLanguage(t, pool, "200", 88101, "fr", d.GeneratedAt)
			historyOriginalLanguage(t, pool, "201", 88101, "fr", d.GeneratedAt)
			after := activityRead(t, s, 20, "")
			if !contaminated && (len(after.Items) != 1 || after.Items[0].EventID != before.Items[0].EventID || after.Items[0].FirstScreeningDate != before.Items[0].FirstScreeningDate) {
				t.Fatal("compatible return coalesced", after)
			}
			if contaminated && len(after.Items) != 0 {
				t.Fatal("new positive lineage did not invalidate return", after)
			}
			if activityCount(t, pool, "cinema_activity_episodes") != 3 {
				t.Fatal("identity rewrote/deleted episodes")
			}
		})
	}
}

func TestActivityLocalMergeUnmergeIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	historyPublish(t, s, activityDataset(""))
	a, b := activityDataset("200"), activityDataset("201")
	b.Showtimes[0].StartTime = b.Showtimes[0].StartTime.Add(time.Hour)
	b.Showtimes[0].EndTime = b.Showtimes[0].EndTime.Add(time.Hour)
	a.Showtimes = append(a.Showtimes, b.Showtimes...)
	historyPublish(t, s, a)
	store := enrichment.NewPostgresStore(pool)
	members := []enrichment.LocalMovieSource{{SourceProvider: "ugc", SourceMovieID: "200"}, {SourceProvider: "ugc", SourceMovieID: "201"}}
	group, err := store.MergeLocalMovies(t.Context(), members, members[0])
	if err != nil {
		t.Fatal(err)
	}
	merged := activityRead(t, s, 20, "")
	if len(merged.Items) != 1 {
		t.Fatal("local merge left duplicates", merged)
	}
	if err := store.UnmergeLocalMovie(t.Context(), group.ID); err != nil {
		t.Fatal(err)
	}
	unmerged := activityRead(t, s, 20, "")
	if len(unmerged.Items) != 1 || unmerged.Items[0].EventID != merged.Items[0].EventID {
		t.Fatal("unmerge resurrected event", unmerged)
	}
}

func TestActivityDetachedSourceReturnIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	historyPublish(t, s, activityDataset(""))
	d := testDataset()
	d.Theaters = d.Theaters[:1]
	d.Showtimes = []ShowtimeRecord{d.Showtimes[0], d.Showtimes[2]}
	historyPublish(t, s, d)
	store := enrichment.NewPostgresStore(pool)
	members := []enrichment.LocalMovieSource{{SourceProvider: "ugc", SourceMovieID: "200"}, {SourceProvider: "ugc", SourceMovieID: "201"}}
	group, err := store.MergeLocalMovies(t.Context(), members, members[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UnmergeLocalMovie(t.Context(), group.ID); err != nil {
		t.Fatal(err)
	}
	if activityCount(t, pool, "cinema_activity_episodes") != 2 {
		t.Fatal("split modified episode identities")
	}
	historyExec(t, pool, `UPDATE cinema_activity_episode_sources SET first_seen_at='2026-08-15 12:00Z',last_seen_at='2026-08-15 12:00Z'`)
	historyExec(t, pool, `INSERT INTO cinema_activity_coverage(provider,generation,theater_id,service_date,status,basis,source_generated_at,detected_at)
 SELECT 'ugc',100,'ugc-25','2026-08-15'::date+n,'complete','date_response',(('2026-08-15'::date+n)+time '12:00') AT TIME ZONE 'Europe/Paris',(('2026-08-15'::date+n)+time '12:01') AT TIME ZONE 'Europe/Paris' FROM generate_series(1,28) n`)
	var id int64
	if err := pool.QueryRow(t.Context(), `SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='201'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	next := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := observeActivityGroup(t.Context(), tx, 101, next.Add(10*time.Hour), []activitySource{{theater: "ugc-25", provider: "ugc", id: "201", movieID: id, from: next, through: next, eligible: &next}}, false); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	result := activityRead(t, s, 20, "")
	if len(result.Items) != 1 || result.Items[0].Type != "return_to_program" || result.Items[0].PreviousProgramEndDate == nil || *result.Items[0].PreviousProgramEndDate != "2026-08-15" {
		t.Fatal("detached familiarity did not allow fresh proven return", result)
	}
	if activityCount(t, pool, "cinema_activity_episodes") != 3 {
		t.Fatal("split resurrected episode")
	}
}
