package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"messeances/api/internal/schedule"
	"messeances/api/internal/schedulepg"
)

func followedActivityExec(t *testing.T, f *lifecycleFixture, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func seedFollowedActivity(t *testing.T, f *lifecycleFixture) {
	t.Helper()
	for _, id := range []int64{1, 2, 3} {
		watchlistSeedMovie(t, f, id, "Film "+strconv.FormatInt(id, 10))
	}
	followedActivityExec(t, f, `INSERT INTO public_movie_sources(source_provider,source_movie_id,public_movie_id,source_slug,title,runtime_minutes)
 VALUES('ugc','101',1,'ugc-film-101','Old source',90),('ugc','102',2,'ugc-film-102','Second source',90)`)
	followedActivityExec(t, f, `UPDATE public_movies SET redirect_to_id=2 WHERE id=1`)
	followedActivityExec(t, f, `INSERT INTO public_movie_metadata_overrides(public_movie_id,title,title_overridden,poster_url_overridden) VALUES(2,'Current override',true,true)`)
	followedActivityExec(t, f, `INSERT INTO screening_history_theaters(id,provider_id,slug,provider,name,address,city,postal_code,city_slug,city_name,passes,last_observed_at)
 SELECT 'ugc-'||n,n::text,'ugc-'||n,'ugc','Retained '||n,'Address','Paris','75001','paris','Paris','{}',CURRENT_TIMESTAMP FROM generate_series(1,3) n`)
	followedActivityExec(t, f, `INSERT INTO cinema_activity_state SELECT id,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP FROM screening_history_theaters WHERE id IN ('ugc-1','ugc-2')`)
	followedActivityExec(t, f, `INSERT INTO schedule_snapshot(singleton,version,schema_version,provider,scope,generated_at,timezone,window_from,window_through)
 VALUES(true,1,1,'ugc','all_cinemas',CURRENT_TIMESTAMP,'Europe/Paris',CURRENT_DATE,CURRENT_DATE+13)`)
	followedActivityExec(t, f, `INSERT INTO theaters(generation_id,id,provider_id,slug,provider,name,address,city,postal_code)
 SELECT 1,'ugc-'||n,n::text,'ugc-'||n,'ugc','Active '||n,'Address','Lille','59000' FROM generate_series(1,3) n WHERE n<>2`)
	followedActivityExec(t, f, `INSERT INTO theater_dates(generation_id,theater_id,service_date) SELECT 1,id,CURRENT_DATE+1 FROM theaters`)
	followedActivityExec(t, f, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'ugc','101','ugc-film-101','Scheduled',90)`)
	followedActivityExec(t, f, `INSERT INTO showtimes(generation_id,id,provider_showing_id,provider,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url)
 SELECT 1,'ugc-showing-'||n,n::text,'ugc',CURRENT_DATE+1,'ugc-'||n,'101',CURRENT_TIMESTAMP+interval '1 day',CURRENT_TIMESTAMP+interval '1 day 90 minutes','VF','VF','2D','','https://example.com' FROM generate_series(1,3) n WHERE n<>2`)
	// Deliberately equal microsecond timestamps with globally numeric ID ordering.
	followedActivityExec(t, f, `INSERT INTO cinema_activity_episodes(id,theater_id,anchor_provider,anchor_source_movie_id,kind,detected_at,first_screening_date,observed_from,observed_through,detecting_generation)
 OVERRIDING SYSTEM VALUE VALUES
 (1,'ugc-1','ugc','101','baseline','2026-10-01T12:00:00Z','2026-10-03','2026-10-03','2026-10-03',1),
 (2,'ugc-1','ugc','101','added_to_program','2026-10-02T12:00:00.123455Z','2026-10-03','2026-10-03','2026-10-03',1),
 (3,'ugc-2','ugc','101','added_to_program','2026-10-02T12:00:00.123456Z','2026-10-03','2026-10-03','2026-10-03',1),
 (5,'ugc-1','ugc','101','added_to_program','2026-10-02T12:00:00.123457Z','2026-10-03','2026-10-03','2026-10-03',1),
 (6,'ugc-1','ugc','101','added_to_program','2026-10-02T12:00:00.123458Z','2026-10-03','2026-10-03','2026-10-03',1),
 (7,'ugc-3','ugc','101','added_to_program','2026-10-02T12:00:00.123459Z','2026-10-03','2026-10-03','2026-10-03',1)`)
	followedActivityExec(t, f, `INSERT INTO cinema_activity_episodes(id,theater_id,anchor_provider,anchor_source_movie_id,kind,detected_at,first_screening_date,previous_program_end_date,observed_from,observed_through,break_from,break_through,detecting_generation)
 OVERRIDING SYSTEM VALUE VALUES(4,'ugc-1','ugc','101','return_to_program','2026-10-02T12:00:00.123456Z','2026-10-03','2026-09-01','2026-10-03','2026-10-03','2026-09-02','2026-10-02',1)`)
	followedActivityExec(t, f, `UPDATE cinema_activity_episodes SET superseded_by_id=2 WHERE id=6`)
	followedActivityExec(t, f, `INSERT INTO cinema_activity_episode_sources SELECT e.theater_id,e.id,s.source_provider,s.source_movie_id,e.observed_from,e.observed_through,e.detected_at,e.detected_at FROM cinema_activity_episodes e CROSS JOIN public_movie_sources s`)
}

func TestFollowedActivityMergedJournalIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	seedFollowedActivity(t, f)
	a := f.complete(t, "feed@example.com", "feed_owner")
	ctx := t.Context()
	read := func(cursor string, limit int) FollowedActivityView {
		t.Helper()
		v, err := f.service.FollowedActivity(ctx, a.Cookie.Token, FollowedActivityQuery{Cursor: cursor, Limit: limit})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, id := range []string{"ugc-2", "ugc-1", "unknown"} {
		current, err := f.service.TheaterFollows(ctx, a.Cookie.Token)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.service.SaveTheaterFollow(ctx, a.Cookie.Token, "feed_owner", current.Revision, id, true); err != nil {
			t.Fatal(err)
		}
	}
	v := read("", 20)
	if v.Username != "feed_owner" || v.FollowsRevision != "3" || v.FollowedTheaterCount != 3 || v.Coverage.InitializedTheaterCount != 2 || v.Coverage.Completeness != "partial" || v.Coverage.Bootstrap != "baseline" || v.Coverage.ReturnMinimumBreakDays != 28 || v.Timezone != schedule.Timezone || v.GeneratedAt.IsZero() || v.NextCursor != nil {
		t.Fatalf("metadata %+v", v)
	}
	ids := []string{}
	for _, e := range v.Items {
		ids = append(ids, e.EventID)
		if e.Movie.Slug != "film-2" || e.Movie.Title != "Current override" || e.Movie.PosterURL != nil {
			t.Fatal("canonical override lost", e)
		}
	}
	if !reflect.DeepEqual(ids, []string{"5", "4", "3", "2"}) {
		t.Fatal("merged ordering/duplicates/baseline/supersession", ids)
	}
	if v.Items[1].PreviousProgramEndDate == nil || *v.Items[1].PreviousProgramEndDate != "2026-09-01" || v.Items[0].PreviousProgramEndDate != nil {
		t.Fatal("return dates")
	}
	for _, e := range v.Items {
		if e.Theater.ID == "ugc-2" {
			if e.Theater.Name != "Retained 2" || e.Theater.City != "Paris" || e.HasUpcomingShowtimes || e.NextShowtimeDate != nil {
				t.Fatal("absent live theater or cross-theater availability", e)
			}
		} else if e.Theater.Name != "Active 1" || e.Theater.City != "Lille" || e.Theater.Slug != "ugc-1" || e.Theater.Provider != "ugc" || !e.HasUpcomingShowtimes || e.NextShowtimeDate == nil {
			t.Fatal("active attribution/upcoming", e)
		}
	}
	public, err := schedulepg.NewStore(f.pool).TheaterActivity(ctx, schedule.TheaterActivityQuery{Slug: "ugc-1"})
	if err != nil {
		t.Fatal(err)
	}
	expectedPublic := []schedule.ActivityEvent{v.Items[0].ActivityEvent, v.Items[1].ActivityEvent, v.Items[3].ActivityEvent}
	if len(public.Items) != len(expectedPublic) {
		t.Fatalf("public projection event count differs: public=%d followed=%d", len(public.Items), len(expectedPublic))
	}
	for i, event := range public.Items {
		expected := expectedPublic[i]
		if sameFollowedActivityProjection(event, expected) {
			continue
		}
		actualJSON, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		expectedJSON, err := json.Marshal(expected)
		if err != nil {
			t.Fatal(err)
		}
		t.Fatalf("public projection differs at index %d: public=%s followed=%s; public locations detected_at=%s movie.updated_at=%s; followed locations detected_at=%s movie.updated_at=%s", i, actualJSON, expectedJSON, event.DetectedAt.Location(), event.Movie.UpdatedAt.Location(), expected.DetectedAt.Location(), expected.Movie.UpdatedAt.Location())
	}
	first := read("", 2)
	if len(first.Items) != 2 || first.NextCursor == nil {
		t.Fatal("first page")
	}
	p, err := f.service.decodeFollowedActivityCursor(*first.NextCursor, accountIDForEmail(t, f, "feed@example.com"))
	if err != nil || p.LastID != "4" || p.UpperID != "5" || p.LastDetectedAt != "2026-10-02T12:00:00.123456Z" {
		t.Fatal("wrong continuation anchor", p, err)
	}
	// A newer committed receipt appears only after refreshing the upper-ID walk.
	followedActivityExec(t, f, `INSERT INTO cinema_activity_episodes(id,theater_id,anchor_provider,anchor_source_movie_id,kind,detected_at,first_screening_date,observed_from,observed_through,detecting_generation)
 OVERRIDING SYSTEM VALUE VALUES(8,'ugc-1','ugc','101','added_to_program','2026-10-02T12:00:01Z','2026-10-03','2026-10-03','2026-10-03',2)`)
	last := read(*first.NextCursor, 2)
	if len(last.Items) != 2 || last.Items[0].EventID != "3" || last.Items[1].EventID != "2" || last.NextCursor != nil {
		t.Fatal("numeric tie/upper bound/last page", last)
	}
	if read("", 2).Items[0].EventID != "8" {
		t.Fatal("refresh lost new publication")
	}
	// Current identity remapping changes projection, never number or order of episodes.
	followedActivityExec(t, f, `UPDATE public_movie_sources SET public_movie_id=3 WHERE source_movie_id='101'`)
	after := read("", 20)
	if len(after.Items) != 5 {
		t.Fatal("split fabricated/lost episode")
	}
	for _, e := range after.Items {
		if e.Movie.Slug != "film-3" {
			t.Fatal("split not reflected")
		}
	}
	followedActivityExec(t, f, `UPDATE public_movie_sources SET public_movie_id=1 WHERE source_movie_id='101'`)
	if read("", 20).Items[0].Movie.Slug != "film-2" {
		t.Fatal("merge not reflected")
	}
	// Supersession legitimately removes a remaining row from an existing walk.
	followedActivityExec(t, f, `UPDATE cinema_activity_episodes SET superseded_by_id=1 WHERE id=2`)
	last = read(*first.NextCursor, 2)
	if len(last.Items) != 1 || last.Items[0].EventID != "3" || last.NextCursor != nil {
		t.Fatal("superseded continuation reappeared")
	}
}

func accountIDForEmail(t *testing.T, f *lifecycleFixture, email string) int64 {
	t.Helper()
	var id int64
	if err := f.pool.QueryRow(t.Context(), `SELECT id FROM accounts WHERE email=$1`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestFollowedActivityEmptyCoverageAndCursorFenceIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	a := f.complete(t, "empty@example.com", "feed_empty")
	read := func() FollowedActivityView {
		t.Helper()
		v, err := f.service.FollowedActivity(t.Context(), a.Cookie.Token, FollowedActivityQuery{})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// Empty membership has no dependency on any journal table.
	followedActivityExec(t, f, `ALTER TABLE cinema_activity_episodes RENAME TO fixture_hidden_episodes; ALTER TABLE cinema_activity_state RENAME TO fixture_hidden_state`)
	v := read()
	if v.FollowsRevision != "0" || v.FollowedTheaterCount != 0 || v.Items == nil || len(v.Items) != 0 || v.Coverage.Completeness != "unknown" || v.Coverage.InitializedTheaterCount != 0 || v.NextCursor != nil || v.GeneratedAt.IsZero() {
		t.Fatal("empty snapshot", v)
	}
	followedActivityExec(t, f, `ALTER TABLE fixture_hidden_episodes RENAME TO cinema_activity_episodes; ALTER TABLE fixture_hidden_state RENAME TO cinema_activity_state`)
	if _, err := f.service.SaveTheaterFollow(t.Context(), a.Cookie.Token, "feed_empty", "0", "unknown", true); err != nil {
		t.Fatal(err)
	}
	v = read()
	if v.FollowedTheaterCount != 1 || v.Coverage.InitializedTheaterCount != 0 || v.Coverage.Completeness != "unknown" || len(v.Items) != 0 || v.NextCursor != nil {
		t.Fatal("unknown inventory widened feed")
	}
	seedFollowedActivity(t, f)
	if _, err := f.service.SaveTheaterFollow(t.Context(), a.Cookie.Token, "feed_empty", "1", "ugc-1", true); err != nil {
		t.Fatal(err)
	}
	first, err := f.service.FollowedActivity(t.Context(), a.Cookie.Token, FollowedActivityQuery{Limit: 1})
	if err != nil || first.NextCursor == nil {
		t.Fatal("cursor fixture", err)
	}
	foreign := f.complete(t, "foreign@example.com", "feed_foreign")
	if _, err = f.service.FollowedActivity(t.Context(), foreign.Cookie.Token, FollowedActivityQuery{Cursor: *first.NextCursor}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("foreign cursor accepted")
	}
	if _, err = f.service.SaveTheaterFollow(t.Context(), a.Cookie.Token, "feed_empty", "2", "ugc-1", false); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.SaveTheaterFollow(t.Context(), a.Cookie.Token, "feed_empty", "3", "ugc-1", true); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.FollowedActivity(t.Context(), a.Cookie.Token, FollowedActivityQuery{Cursor: *first.NextCursor}); !errors.Is(err, ErrTheaterFollowsChanged) {
		t.Fatal("change-back revision reused cursor")
	}
	if _, err = f.service.SaveTheaterFollow(t.Context(), a.Cookie.Token, "feed_empty", "4", "ugc-1", false); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.SaveTheaterFollow(t.Context(), a.Cookie.Token, "feed_empty", "5", "unknown", false); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.FollowedActivity(t.Context(), a.Cookie.Token, FollowedActivityQuery{Cursor: *first.NextCursor}); !errors.Is(err, ErrTheaterFollowsChanged) {
		t.Fatal("empty shortcut bypassed revision fence")
	}
	if _, err = f.service.FollowedActivity(t.Context(), a.Cookie.Token, FollowedActivityQuery{Cursor: "malformed"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("empty shortcut bypassed cursor validation")
	}
	if read().Items == nil {
		t.Fatal("empty array became null")
	}
}

func TestFollowedActivityDeadlineAndAuthorizationIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	a := f.complete(t, "deadline@example.com", "feed_deadline")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.service.FollowedActivity(ctx, a.Cookie.Token, FollowedActivityQuery{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("cancelled read accepted")
	}
	connections := []*pgxpool.Conn{}
	for range f.pool.Config().MaxConns {
		c, err := f.pool.Acquire(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, c)
	}
	ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
	v, err := f.service.FollowedActivity(ctx, a.Cookie.Token, FollowedActivityQuery{})
	cancel()
	for _, c := range connections {
		c.Release()
	}
	if !errors.Is(err, ErrUnavailable) || v.Username != "" || v.Items != nil || len(f.service.followedActivityGate) != 0 {
		t.Fatal("pool wait escaped deadline/gate", err)
	}
	for _, raw := range []string{"", "invalid-token"} {
		if _, err := f.service.FollowedActivity(t.Context(), raw, FollowedActivityQuery{}); !errors.Is(err, ErrUnauthorized) {
			t.Fatal("unauthorized feed accepted", err)
		}
	}
	f.register(t, "pending-feed@example.com", testPassword)
	pending, err := f.service.Login(t.Context(), "pending-feed@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{pending.Cookie.Token, f.pending(t, "username-feed@example.com").Cookie.Token} {
		if _, err := f.service.FollowedActivity(t.Context(), raw, FollowedActivityQuery{}); !errors.Is(err, ErrPending) {
			t.Fatal("pending feed accepted", err)
		}
	}
	if err := f.service.Logout(t.Context(), a.Cookie.Token, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.FollowedActivity(t.Context(), a.Cookie.Token, FollowedActivityQuery{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revoked feed accepted")
	}
}

func TestFollowedActivityAccountLockRaceIntegration(t *testing.T) {
	requireFollowDatabase(t)
	for _, operation := range []string{"change follows", "revoke", "delete"} {
		t.Run(operation, func(t *testing.T) {
			f := newLifecycleFixture(t)
			seedFollowedActivity(t, f)
			a := f.complete(t, "race-feed@example.com", "feed_race")
			if _, err := f.service.SaveTheaterFollow(t.Context(), a.Cookie.Token, "feed_race", "0", "ugc-1", true); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			tx, err := f.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			var id int64
			if err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE email='race-feed@example.com' FOR UPDATE`).Scan(&id); err != nil {
				t.Fatal(err)
			}
			type outcome struct {
				view FollowedActivityView
				err  error
			}
			done := make(chan outcome, 1)
			go func() {
				v, err := f.service.FollowedActivity(ctx, a.Cookie.Token, FollowedActivityQuery{})
				done <- outcome{v, err}
			}()
			switch operation {
			case "change follows":
				_, err = tx.Exec(ctx, `UPDATE account_theater_follows SET theater_ids=ARRAY['ugc-2'],revision=2 WHERE account_id=$1`, id)
			case "revoke":
				err = revoke(ctx, tx, &account{id: id, revision: 1})
			case "delete":
				_, err = tx.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, id)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			out := <-done
			if operation == "change follows" {
				if out.err != nil || out.view.FollowsRevision != "2" || len(out.view.Items) != 1 || out.view.Items[0].Theater.ID != "ugc-2" {
					t.Fatal("read stale membership after account wait", out)
				}
			} else if !errors.Is(out.err, ErrUnauthorized) || out.view.Items != nil {
				t.Fatal("read after revocation/deletion leaked data", out)
			}
		})
	}
}

func TestFollowedActivityStatementTimeoutRollbackIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	seedFollowedActivity(t, f)
	a := f.complete(t, "timeout-feed@example.com", "feed_timeout")
	if _, err := f.service.SaveTheaterFollow(t.Context(), a.Cookie.Token, "feed_timeout", "0", "ugc-1", true); err != nil {
		t.Fatal(err)
	}
	digest, _ := TokenDigest(a.Cookie.Token)
	var before, after time.Time
	if err := f.pool.QueryRow(t.Context(), `SELECT last_seen_at FROM account_sessions WHERE token_digest=$1`, digest[:]).Scan(&before); err != nil {
		t.Fatal(err)
	}
	f.advance(time.Minute)
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(t.Context(), `LOCK TABLE cinema_activity_episodes IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	v, err := f.service.FollowedActivity(t.Context(), a.Cookie.Token, FollowedActivityQuery{})
	if !errors.Is(err, ErrUnavailable) || v.Items != nil || v.Username != "" || len(f.service.followedActivityGate) != 0 {
		t.Fatal("query timeout escaped transaction/gate", err)
	}
	if err := f.pool.QueryRow(t.Context(), `SELECT last_seen_at FROM account_sessions WHERE token_digest=$1`, digest[:]).Scan(&after); err != nil || !before.Equal(after) {
		t.Fatal("failed query committed session touch", err)
	}
	if err = tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if v, err = f.service.FollowedActivity(t.Context(), a.Cookie.Token, FollowedActivityQuery{}); err != nil || len(v.Items) != 3 {
		t.Fatal("timeout left reader unusable", err)
	}
}
