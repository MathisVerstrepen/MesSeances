package schedulepg

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/publicmoviepg"
	"messeances/api/internal/schedule"
)

func activityDataset(movie string) Dataset {
	d := shiftedDataset(testDataset(), 23209) // Fixed future dates: no dependence on wall clock for announcement eligibility.
	d.Theaters = d.Theaters[:1]
	s := d.Showtimes[0]
	d.Showtimes = nil
	if movie != "" {
		s.Movie.ProviderID = movie
		s.Movie.Slug = "ugc-film-" + movie
		s.Movie.Title = "Activity " + movie
		s.ID = "ugc-showing-" + movie
		s.ProviderShowingID = movie
		s.BookingURL = "https://www.ugc.fr/reservationSeances.html?id=" + movie
		d.Showtimes = []ShowtimeRecord{s}
	}
	return d
}

func activityRead(t *testing.T, s *Store, limit int, cursor string) schedule.TheaterActivity {
	t.Helper()
	r, err := s.TheaterActivity(t.Context(), schedule.TheaterActivityQuery{Slug: "ugc-25", Limit: limit, Cursor: cursor})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func activityCount(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestActivityPublicationIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	historyPublish(t, s, activityDataset(""))
	base := activityRead(t, s, 0, "")
	if len(base.Items) != 0 || base.Coverage.Completeness != "partial" || base.Coverage.HistoryStartedAt == nil || base.Limit != 20 {
		t.Fatal(base)
	}
	historyPublish(t, s, activityDataset("200"))
	first := activityRead(t, s, 0, "")
	if len(first.Items) != 1 || first.Items[0].Type != "added_to_program" || !first.Items[0].HasUpcomingShowtimes || first.Items[0].NextShowtimeDate == nil || first.Items[0].PreviousProgramEndDate != nil {
		t.Fatal(first)
	}
	event := first.Items[0]
	d := activityDataset("200")
	d.Showtimes[0].StartTime = d.Showtimes[0].StartTime.Add(time.Hour)
	d.Showtimes[0].EndTime = d.Showtimes[0].EndTime.Add(time.Hour)
	d.Showtimes[0].Room = "new room"
	d.Showtimes[0].Movie.Title = "Edited metadata"
	historyPublish(t, s, d)
	historyPublish(t, s, d)
	current := activityRead(t, s, 0, "")
	if len(current.Items) != 1 || current.Items[0].EventID != event.EventID || current.Items[0].DetectedAt != event.DetectedAt || current.Items[0].FirstScreeningDate != event.FirstScreeningDate || current.Items[0].Movie.Title != "Edited metadata" {
		t.Fatal(current)
	}
	// Failed journal write rolls back live generation, retained rows and coverage.
	var generation int64
	if err := pool.QueryRow(t.Context(), `SELECT version FROM schedule_snapshot`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	historyExec(t, pool, `CREATE FUNCTION reject_activity_fixture() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'fixture';END$$;CREATE TRIGGER reject_activity_fixture BEFORE INSERT ON cinema_activity_episodes FOR EACH ROW EXECUTE FUNCTION reject_activity_fixture()`)
	beforeCoverage := activityCount(t, pool, "cinema_activity_coverage")
	failing := activityDataset("201")
	failing.Showtimes[0].StartTime = failing.Showtimes[0].StartTime.Add(2 * time.Hour)
	failing.Showtimes[0].EndTime = failing.Showtimes[0].EndTime.Add(2 * time.Hour)
	if _, err := s.Replace(t.Context(), []Dataset{failing}); err == nil {
		t.Fatal("failed activity write committed")
	}
	var unchanged bool
	if err := pool.QueryRow(t.Context(), `SELECT version=$1 FROM schedule_snapshot`, generation).Scan(&unchanged); err != nil || !unchanged || activityCount(t, pool, "cinema_activity_coverage") != beforeCoverage {
		t.Fatal("publication not atomic", err)
	}
	historyExec(t, pool, `DROP TRIGGER reject_activity_fixture ON cinema_activity_episodes;DROP FUNCTION reject_activity_fixture()`)
	// Omission does not advance actual cinema observation; copied providers write nothing.
	last := *current.Coverage.LastPublicationAt
	omit := activityDataset("")
	omit.Theaters = nil
	historyPublish(t, s, omit)
	omitted := activityRead(t, s, 0, "")
	if !omitted.Coverage.LastPublicationAt.Equal(last) || omitted.Items[0].HasUpcomingShowtimes || omitted.Theater.Slug != "ugc-25" {
		t.Fatal(omitted)
	}
	n := activityCount(t, pool, "cinema_activity_coverage")
	historyPublish(t, s, kinepolisTestDataset())
	if activityCount(t, pool, "cinema_activity_coverage") != n+1 {
		t.Fatal("copied provider wrote coverage")
	}
	historyPublish(t, s, d)
	if len(activityRead(t, s, 0, "").Items) != 1 {
		t.Fatal("recovery emitted event")
	}
}

func TestActivityBaselineFamiliarityAndCorrectionsIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	historyPublish(t, s, activityDataset("200"))
	// Simulate an upgrade with retained positive history but no activity rows.
	historyExec(t, pool, `DELETE FROM cinema_activity_episode_sources;DELETE FROM cinema_activity_episodes;DELETE FROM cinema_activity_coverage;DELETE FROM cinema_activity_state`)
	before := activityRead(t, s, 0, "")
	if before.Coverage.Completeness != "unknown" || before.Coverage.HistoryStartedAt != nil || before.Items == nil || len(before.Items) != 0 {
		t.Fatal(before)
	}
	historyPublish(t, s, activityDataset(""))
	historyPublish(t, s, activityDataset("200"))
	if len(activityRead(t, s, 0, "").Items) != 0 {
		t.Fatal("baseline familiarity announced")
	}
	// Same owned screening corrected to a new source remains silent.
	d := activityDataset("201")
	d.Showtimes[0].ID = "ugc-showing-200"
	d.Showtimes[0].ProviderShowingID = "200"
	d.Showtimes[0].BookingURL = "https://www.ugc.fr/reservationSeances.html?id=200"
	historyPublish(t, s, d)
	if len(activityRead(t, s, 0, "").Items) != 0 {
		t.Fatal("source correction announced")
	}
	// Existing sources do not permit a false new-film announcement after long outages.
	d = shiftedDataset(activityDataset("200"), 200)
	historyPublish(t, s, d)
	if len(activityRead(t, s, 0, "").Items) != 0 {
		t.Fatal("unproven long return announced")
	}
}

func TestActivityPaginationIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	historyPublish(t, s, activityDataset(""))
	for _, id := range []string{"200", "201", "202"} {
		d := activityDataset(id)
		d.Showtimes[0].StartTime = d.Showtimes[0].StartTime.Add(time.Duration(len(id)+int(id[2])) * time.Minute)
		d.Showtimes[0].EndTime = d.Showtimes[0].StartTime.Add(100 * time.Minute)
		historyPublish(t, s, d)
	}
	all := activityRead(t, s, 100, "")
	if len(all.Items) != 3 {
		t.Fatal(all)
	}
	page := activityRead(t, s, 1, "")
	if page.NextCursor == nil || page.Items[0].EventID != all.Items[0].EventID {
		t.Fatal(page)
	}
	d := activityDataset("203")
	d.Showtimes[0].StartTime = d.Showtimes[0].StartTime.Add(9 * time.Hour)
	d.Showtimes[0].EndTime = d.Showtimes[0].StartTime.Add(100 * time.Minute)
	historyPublish(t, s, d)
	second := activityRead(t, s, 1, *page.NextCursor)
	if len(second.Items) != 1 || second.Items[0].EventID != all.Items[1].EventID || second.NextCursor == nil {
		t.Fatal(second)
	}
	third := activityRead(t, s, 1, *second.NextCursor)
	if len(third.Items) != 1 || third.Items[0].EventID != all.Items[2].EventID || third.NextCursor != nil {
		t.Fatal(third)
	}
	_, err := s.TheaterActivity(t.Context(), schedule.TheaterActivityQuery{Slug: "ugc-26", Cursor: *page.NextCursor})
	var invalid *schedule.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatal("cross-theater cursor accepted", err)
	}
	_, err = s.TheaterActivity(t.Context(), schedule.TheaterActivityQuery{Slug: "absent"})
	var missing *schedule.NotFoundError
	if !errors.As(err, &missing) {
		t.Fatal("unknown cinema", err)
	}
	if _, err := NewStore(nil).TheaterActivity(t.Context(), schedule.TheaterActivityQuery{Slug: "ugc-25"}); !errors.Is(err, schedule.ErrHistoryUnavailable) {
		t.Fatal(err)
	}
}

func TestActivityReturnProofIntegration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		days     int
		mutation string
		want     bool
	}{
		{"28_dates", 28, "", true}, {"27_dates", 27, "", false}, {"missing_day", 28, `DELETE FROM cinema_activity_coverage WHERE service_date='2026-08-20'`, false},
		{"later_unknown", 28, `INSERT INTO cinema_activity_coverage SELECT provider,generation+1,theater_id,service_date,'unknown','unproven',source_generated_at+interval '1 hour',detected_at+interval '1 hour' FROM cinema_activity_coverage WHERE service_date='2026-08-20'`, false},
		{"advance_acquisition", 28, `UPDATE cinema_activity_coverage SET source_generated_at=source_generated_at-interval '1 day' WHERE service_date='2026-08-20'`, false},
		{"late_receipt", 28, `UPDATE cinema_activity_coverage SET detected_at=detected_at+interval '1 day' WHERE service_date='2026-08-20'`, false},
		{"positive_lineage", 28, `UPDATE cinema_activity_episode_sources SET observed_through='2026-08-20'`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := newHistoryPool(t)
			s := NewStore(pool)
			d := testDataset()
			d.Theaters = d.Theaters[:1]
			d.Showtimes = d.Showtimes[:1]
			historyPublish(t, s, d)
			historyExec(t, pool, `UPDATE cinema_activity_episodes SET detected_at='2026-08-15 12:00Z'`)
			historyExec(t, pool, `UPDATE cinema_activity_episode_sources SET first_seen_at='2026-08-15 12:00Z',last_seen_at='2026-08-15 12:00Z'`)
			var movieID int64
			if err := pool.QueryRow(t.Context(), `SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='200'`).Scan(&movieID); err != nil {
				t.Fatal(err)
			}
			historyExec(t, pool, `INSERT INTO cinema_activity_coverage(provider,generation,theater_id,service_date,status,basis,source_generated_at,detected_at)
 SELECT 'ugc',100,'ugc-25','2026-08-15'::date+n,'complete','date_response',(('2026-08-15'::date+n)+time '12:00') AT TIME ZONE 'Europe/Paris',(('2026-08-15'::date+n)+time '12:01') AT TIME ZONE 'Europe/Paris' FROM generate_series(1,$1::int) n`, tc.days)
			if tc.mutation != "" {
				historyExec(t, pool, tc.mutation)
			}
			previous := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
			next := previous.AddDate(0, 0, tc.days+1)
			received := next.Add(10 * time.Hour)
			tx, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if err := observeActivityGroup(t.Context(), tx, 101, received, []activitySource{{theater: "ugc-25", provider: "ugc", id: "200", movieID: movieID, from: next, through: next, eligible: &next}}, false); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			r := activityRead(t, s, 20, "")
			if (len(r.Items) == 1) != tc.want {
				t.Fatal(tc.name, r)
			}
			if tc.want && (r.Items[0].Type != "return_to_program" || r.Items[0].PreviousProgramEndDate == nil || *r.Items[0].PreviousProgramEndDate != "2026-08-15" || r.Items[0].FirstScreeningDate != next.Format("2006-01-02")) {
				t.Fatal(r)
			}
			// Receipt replay or later passage of time never creates a second return.
			tx, err = pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := observeActivityGroup(t.Context(), tx, 102, received.AddDate(0, 0, 60), []activitySource{{theater: "ugc-25", provider: "ugc", id: "200", movieID: movieID, from: next, through: next, eligible: &next}}, false); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(activityRead(t, s, 20, "").Items) != len(r.Items) {
				t.Fatal("replay duplicated return")
			}
		})
	}
}

func TestActivityCanonicalMergeSplitIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	historyPublish(t, s, activityDataset(""))
	a, b := activityDataset("200"), activityDataset("201")
	b.Showtimes[0].StartTime = b.Showtimes[0].StartTime.Add(time.Hour)
	b.Showtimes[0].EndTime = b.Showtimes[0].EndTime.Add(time.Hour)
	historyPublish(t, s, a)
	historyPublish(t, s, b)
	before := activityRead(t, s, 20, "")
	if len(before.Items) != 2 {
		t.Fatal(before)
	}
	historyOriginalLanguage(t, pool, "200", 99901, "fr", a.GeneratedAt)
	historyOriginalLanguage(t, pool, "201", 99901, "fr", a.GeneratedAt)
	merged := activityRead(t, s, 20, "")
	if len(merged.Items) != 1 || merged.Items[0].EventID != before.Items[1].EventID || !merged.Items[0].DetectedAt.Equal(before.Items[1].DetectedAt) || activityCount(t, pool, "cinema_activity_episodes") != 2 || activityCount(t, pool, "cinema_activity_episode_sources") != 3 {
		t.Fatal("merge lost stable provenance", merged)
	}
	// Canonical edits change presentation without manufacturing announcements.
	historyExec(t, pool, `UPDATE public_movies SET title='Canonical edited',poster_url=NULL WHERE redirect_to_id IS NULL`)
	if got := activityRead(t, s, 20, ""); got.Items[0].Movie.Title != "Canonical edited" || got.Items[0].Movie.PosterURL != nil {
		t.Fatal(got)
	}
	store := enrichment.NewPostgresStore(pool)
	match, found, err := store.Match(t.Context(), enrichment.SourceUGC, "201", enrichment.ProviderTMDB)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	match.Status = enrichment.StatusUnmatched
	match.MetadataMovieID = 0
	match.Score = 0
	match.Candidates = []enrichment.Candidate{}
	if err := store.SaveDecision(t.Context(), match); err != nil {
		t.Fatal(err)
	}
	split := activityRead(t, s, 20, "")
	if len(split.Items) != 1 || split.Items[0].EventID != merged.Items[0].EventID {
		t.Fatal("split fanned out/resurrected", split)
	}
	historyPublish(t, s, b)
	if len(activityRead(t, s, 20, "").Items) != 1 {
		t.Fatal("detached familiar source reannounced")
	}
}

func TestActivityConcurrentPublicationIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	historyPublish(t, s, activityDataset(""))
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := s.Replace(t.Context(), []Dataset{activityDataset("200")}); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := activityRead(t, s, 20, ""); len(got.Items) != 1 {
		t.Fatal("serialized writers duplicated event", got)
	}
}

func TestActivityReadBoundsIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	historyPublish(t, s, activityDataset(""))
	s.historyCalls.Store(2)
	_, err := s.TheaterActivity(t.Context(), schedule.TheaterActivityQuery{Slug: "ugc-25"})
	if !errors.Is(err, schedule.ErrHistoryBusy) {
		t.Fatal("admission unbounded", err)
	}
	s.historyCalls.Store(0)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = s.TheaterActivity(ctx, schedule.TheaterActivityQuery{Slug: "ugc-25"})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	// Pool wait belongs to the same deadline as all response subqueries.
	connections := []*pgxpool.Conn{}
	for range pool.Config().MaxConns {
		conn, err := pool.Acquire(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, conn)
	}
	defer func() {
		for _, conn := range connections {
			conn.Release()
		}
	}()
	ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = s.TheaterActivity(ctx, schedule.TheaterActivityQuery{Slug: "ugc-25"})
	if !errors.Is(err, schedule.ErrHistoryQueryTimeout) || s.historyCalls.Load() != 0 {
		t.Fatal("pool acquisition escaped deadline/admission release", err)
	}
}

func TestActivityDSTProofIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	historyPublish(t, s, activityDataset("200"))
	for _, month := range []time.Month{time.March, time.October} {
		t.Run(month.String(), func(t *testing.T) {
			previous := time.Date(2026, month, 1, 0, 0, 0, 0, time.UTC)
			next := previous.AddDate(0, 0, 30)
			received := next.AddDate(0, 0, 1).Add(12 * time.Hour)
			historyExec(t, pool, `DELETE FROM cinema_activity_coverage`)
			historyExec(t, pool, `UPDATE cinema_activity_episodes SET observed_from=$1,observed_through=$1`, previous)
			historyExec(t, pool, `UPDATE cinema_activity_episode_sources SET observed_from=$1,observed_through=$1`, previous)
			historyExec(t, pool, `INSERT INTO cinema_activity_coverage(provider,generation,theater_id,service_date,status,basis,source_generated_at,detected_at)
 SELECT 'ugc',100,'ugc-25',$1::date+n,'complete','date_response',(($1::date+n)+time '12:00') AT TIME ZONE 'Europe/Paris',(($1::date+n)+time '12:01') AT TIME ZONE 'Europe/Paris' FROM generate_series(1,29) n`, previous)
			var id int64
			if err := pool.QueryRow(t.Context(), `SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='200'`).Scan(&id); err != nil {
				t.Fatal(err)
			}
			tx, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			ok, err := publicmoviepg.ActivityBreakComplete(t.Context(), tx, "ugc-25", id, previous, next, received)
			if err != nil || !ok {
				t.Fatal("calendar-day DST proof", month, ok, err)
			}
		})
	}
}

// Compile-time transaction and contract ownership checks.
var _ interface {
	TheaterActivity(context.Context, schedule.TheaterActivityQuery) (schedule.TheaterActivity, error)
} = (*Store)(nil)
