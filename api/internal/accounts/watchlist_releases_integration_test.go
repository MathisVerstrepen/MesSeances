package accounts

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/tmdb"
)

type watchlistReleaseFunc func(context.Context, int64) (tmdb.ReleaseEvidence, error)

func (f watchlistReleaseFunc) FrenchReleaseEvidence(ctx context.Context, id int64) (tmdb.ReleaseEvidence, error) {
	return f(ctx, id)
}

func releaseEvidence(t *testing.T, date string) tmdb.ReleaseEvidence {
	t.Helper()
	rows := []tmdb.FrenchReleaseRow{{Type: 4, Date: "1998-01-01"}}
	if date != "" {
		rows = append(rows, tmdb.FrenchReleaseRow{Type: 3, Date: date}, tmdb.FrenchReleaseRow{Type: 2, Date: "2026-10-01"})
	}
	e, err := tmdb.NormalizeFrenchReleases(rows)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func releaseSQL(t *testing.T, f *lifecycleFixture, query string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func releaseSeed(t *testing.T, f *lifecycleFixture, count int) SessionResult {
	t.Helper()
	one := f.complete(t, "releases@example.com", "release_owner")
	releaseSQL(t, f, `INSERT INTO public_movies(id,identity_anchor_tmdb_id,confirmed_tmdb_id,title,runtime_minutes,release_date) OVERRIDING SYSTEM VALUE SELECT n,n,n,'Film',0,'1998-01-01' FROM generate_series(1,$1::int)n`, count)
	releaseSQL(t, f, `INSERT INTO account_watchlist_items(account_id,public_movie_id) SELECT a.id,p.id FROM accounts a CROSS JOIN public_movies p WHERE a.email='releases@example.com'`)
	return one
}

func releaseService(t *testing.T, f *lifecycleFixture, provider WatchlistReleaseProvider) *Service {
	t.Helper()
	s, err := NewService(NewPostgresStore(f.pool), ServiceOptions{Now: f.now, Hasher: f.hasher, Origin: "https://messeances.fr", AddressHMACKey: f.service.hmacKey, WatchlistReleaseProvider: provider})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWatchlistReleaseProjectionIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	one := releaseSeed(t, f, 1)
	ctx := t.Context()
	now := f.now()
	releaseSQL(t, f, `INSERT INTO public_movie_metadata_overrides(public_movie_id,release_date_overridden,release_date) VALUES(1,true,'2001-01-01')`)
	read := func(want string) {
		t.Helper()
		view, err := f.service.Watchlist(ctx, one.Cookie.Token)
		if err != nil || len(view.Items) != 1 || view.Revision != "0" || view.Items[0].FrenchReleaseDate != want || view.Items[0].ReleaseDate != "2001-01-01" {
			t.Fatalf("projection %+v %v want %s", view, err, want)
		}
	}
	read("") // Neither the general metadata nor its override is French evidence.
	releaseSQL(t, f, `INSERT INTO tmdb_upcoming_movies(tmdb_id,public_movie_id,french_release_date,active,verified_at,decision) VALUES(1,1,'1998-10-14',false,$1,'excluded')`, now)
	read("1998-10-14") // Inactive, excluded, old and unassessed positives count.
	releaseSQL(t, f, `INSERT INTO tmdb_french_release_cache(tmdb_id,verified_at,retry_after,attempt_revision) VALUES(1,$1,$1,1)`, now.Add(time.Second))
	read("") // Newer negative does not coalesce into the older positive.
	releaseSQL(t, f, `UPDATE tmdb_french_release_cache SET verified_at=$1`, now)
	read("1998-10-14") // Upcoming wins exact ties.
	releaseSQL(t, f, `UPDATE tmdb_french_release_cache SET french_release_date='1998-10-15',verified_at=$1`, now.Add(time.Second))
	read("1998-10-15")
	releaseSQL(t, f, `UPDATE tmdb_upcoming_movies SET french_release_date=NULL,verified_at=$1`, now.Add(2*time.Second))
	read("1998-10-15") // Unassessed null is not a negative observation.
	releaseSQL(t, f, `UPDATE tmdb_upcoming_movies SET assessed_at=verified_at`)
	read("")
	releaseSQL(t, f, `UPDATE tmdb_french_release_cache SET french_release_date=NULL,verified_at=NULL`)
	read("")
	releaseSQL(t, f, `UPDATE tmdb_upcoming_movies SET french_release_date='1998-10-14'`)
	// A new confirmed identity must not use either the old cache or anchor ID.
	releaseSQL(t, f, `UPDATE public_movies SET confirmed_tmdb_id=2 WHERE id=1`)
	read("")
	releaseSQL(t, f, `UPDATE public_movies SET confirmed_tmdb_id=NULL WHERE id=1`)
	read("")
	ids, err := f.service.dueWatchlistReleases(ctx)
	if err != nil || len(ids) != 0 {
		t.Fatal("unconfirmed identity selected", ids, err)
	}
}

func TestWatchlistReleaseCanonicalResolutionIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	one := releaseSeed(t, f, 2)
	f.complete(t, "second@example.com", "release_second")
	releaseSQL(t, f, `INSERT INTO account_watchlist_items(account_id,public_movie_id) SELECT id,2 FROM accounts WHERE email='second@example.com'`)
	// Leave the tombstone's old confirmed ID deliberately: joins must resolve it.
	releaseSQL(t, f, `UPDATE public_movies SET redirect_to_id=1 WHERE id=2`)
	var ids []int64
	f.service.watchlistReleaseProvider = watchlistReleaseFunc(func(_ context.Context, id int64) (tmdb.ReleaseEvidence, error) {
		ids = append(ids, id)
		return releaseEvidence(t, "1998-10-14"), nil
	})
	r, err := f.service.SweepWatchlistReleases(t.Context())
	if err != nil || r.Verified != 1 || !reflect.DeepEqual(ids, []int64{1}) {
		t.Fatal(r, err, ids)
	}
	view, err := f.service.Watchlist(t.Context(), one.Cookie.Token)
	if err != nil || len(view.Items) != 1 || view.Items[0].FrenchReleaseDate != "1998-10-14" {
		t.Fatal(view, err)
	}
	releaseSQL(t, f, `UPDATE public_movies SET redirect_to_id=NULL WHERE id=2`)
	view, err = f.service.Watchlist(t.Context(), one.Cookie.Token)
	if err != nil || len(view.Items) != 2 {
		t.Fatal(view, err)
	}
	for _, item := range view.Items {
		if item.Slug == "film-2" && item.FrenchReleaseDate != "" {
			t.Fatal("split inherited old identity evidence")
		}
	}
	releaseSQL(t, f, `UPDATE public_movies SET redirect_to_id=CASE id WHEN 1 THEN 2 ELSE 1 END`)
	due, err := f.service.dueWatchlistReleases(t.Context())
	if err != nil || len(due) != 0 {
		t.Fatal("cycle not terminated", due, err)
	}
}

func TestWatchlistReleaseAcquisitionFreshnessAndPersistenceIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	one := releaseSeed(t, f, 2)
	f.service.watchlistReleaseProvider = watchlistReleaseFunc(func(_ context.Context, id int64) (tmdb.ReleaseEvidence, error) {
		if id == 1 {
			return releaseEvidence(t, "1998-10-14"), nil
		}
		return releaseEvidence(t, ""), nil
	})
	before, err := f.service.Watchlist(t.Context(), one.Cookie.Token)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.service.SweepWatchlistReleases(t.Context())
	if err != nil || r.Attempted != 2 || r.Verified != 2 {
		t.Fatal(r, err)
	}
	after, err := f.service.Watchlist(t.Context(), one.Cookie.Token)
	if err != nil || before.Revision != after.Revision || !before.Items[0].AddedAt.Equal(after.Items[0].AddedAt) {
		t.Fatal("membership changed", after, err)
	}
	// A fresh service without credentials still reads retained evidence.
	restarted := releaseService(t, f, nil)
	view, err := restarted.Watchlist(t.Context(), one.Cookie.Token)
	if err != nil || view.Items[0].FrenchReleaseDate != "1998-10-14" || view.Items[1].FrenchReleaseDate != "" {
		t.Fatal(view, err)
	}
	var enrichment, upcoming, states int
	if err = f.pool.QueryRow(t.Context(), `SELECT (SELECT version FROM movie_enrichment_state),(SELECT count(*) FROM tmdb_upcoming_movies),(SELECT count(*) FROM account_watchlist_state)`).Scan(&enrichment, &upcoming, &states); err != nil || enrichment != 0 || upcoming != 0 || states != 0 {
		t.Fatal("publication or membership mutated", err)
	}
	f.advance(7*24*time.Hour - time.Nanosecond)
	assertDue := func(want []int64) {
		t.Helper()
		got, err := f.service.dueWatchlistReleases(t.Context())
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("due", got, want, err)
		}
	}
	assertDue([]int64{})
	f.advance(time.Nanosecond)
	assertDue([]int64{2})
	f.advance(23 * 24 * time.Hour)
	assertDue([]int64{1, 2})
	releaseSQL(t, f, `DELETE FROM accounts`)
	assertDue([]int64{})
	var retained int
	if err = f.pool.QueryRow(t.Context(), `SELECT count(*) FROM tmdb_french_release_cache`).Scan(&retained); err != nil || retained != 2 {
		t.Fatal("cache did not survive deletion", err)
	}
}

func TestWatchlistReleaseErrorsAndBackoffIntegration(t *testing.T) {
	for _, mode := range []string{"404", "stop", "server", "timeout", "invalid", "mismatch", "negative"} {
		t.Run(mode, func(t *testing.T) {
			f := newLifecycleFixture(t)
			releaseSeed(t, f, 2)
			old := f.now().Add(-31 * 24 * time.Hour)
			releaseSQL(t, f, `INSERT INTO tmdb_french_release_cache(tmdb_id,french_release_date,verified_at,retry_after,attempt_revision) VALUES(1,'1998-10-14',$1,$1,1)`, old)
			// Candidate 1 first, before unknown 2, by giving 2 a newer old observation.
			releaseSQL(t, f, `INSERT INTO tmdb_french_release_cache(tmdb_id,verified_at,retry_after,attempt_revision) VALUES(2,$1,$1,1)`, old.Add(time.Hour))
			calls := 0
			f.service.watchlistReleaseProvider = watchlistReleaseFunc(func(_ context.Context, _ int64) (tmdb.ReleaseEvidence, error) {
				calls++
				switch mode {
				case "404":
					return tmdb.ReleaseEvidence{}, tmdb.ErrNotFound
				case "stop":
					return tmdb.ReleaseEvidence{}, tmdb.ErrStop
				case "server":
					return tmdb.ReleaseEvidence{}, errors.New("secret upstream response")
				case "timeout":
					return tmdb.ReleaseEvidence{}, context.DeadlineExceeded
				case "invalid":
					return tmdb.ReleaseEvidence{Rows: []tmdb.FrenchReleaseRow{{Type: 3, Date: "1998-02-31"}}}, nil
				case "mismatch":
					return tmdb.ReleaseEvidence{FrenchReleaseDate: "1998-10-14"}, nil
				default:
					return releaseEvidence(t, ""), nil
				}
			})
			r, err := f.service.SweepWatchlistReleases(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			wantCalls := 2
			if mode == "stop" {
				wantCalls = 1
			}
			if r.Attempted != wantCalls || calls != wantCalls {
				t.Fatal(r, calls)
			}
			var date *string
			var verified, retry time.Time
			var revision int64
			if err = f.pool.QueryRow(t.Context(), `SELECT french_release_date::text,verified_at,retry_after,attempt_revision FROM tmdb_french_release_cache WHERE tmdb_id=1`).Scan(&date, &verified, &retry, &revision); err != nil {
				t.Fatal(err)
			}
			ttl := 15 * time.Minute
			if mode == "404" {
				ttl = 24 * time.Hour
			}
			if mode == "negative" {
				ttl = 7 * 24 * time.Hour
				if date != nil || !verified.Equal(f.now()) {
					t.Fatal("negative not retained")
				}
			} else if date == nil || *date != "1998-10-14" || !verified.Equal(old) {
				t.Fatal("error erased evidence")
			}
			if !retry.Equal(f.now().Add(ttl)) || revision != 2 {
				t.Fatal("backoff/revision", retry, revision)
			}
			if _, err = f.service.SweepWatchlistReleases(t.Context()); err != nil || calls != wantCalls {
				t.Fatal("backoff bypassed", err, calls)
			}
			if mode == "stop" {
				f.advance(15 * time.Minute)
				if _, err = f.service.SweepWatchlistReleases(t.Context()); err != nil || calls != 2 {
					t.Fatal("pause did not expire", calls, err)
				}
			}
		})
	}
}

func TestWatchlistReleaseBackfillBoundAndGlobalQuotaIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	releaseSeed(t, f, 1000)
	f.complete(t, "extra@example.com", "release_extra")
	releaseSQL(t, f, `INSERT INTO public_movies(id,identity_anchor_tmdb_id,confirmed_tmdb_id,title,runtime_minutes) OVERRIDING SYSTEM VALUE SELECT n,n,n,'Extra',0 FROM generate_series(1001,1010)n`)
	releaseSQL(t, f, `INSERT INTO account_watchlist_items(account_id,public_movie_id) SELECT a.id,p.id FROM accounts a CROSS JOIN public_movies p WHERE a.email='extra@example.com' AND p.id>=1001`)
	seen := map[int64]bool{}
	provider := watchlistReleaseFunc(func(_ context.Context, id int64) (tmdb.ReleaseEvidence, error) {
		if seen[id] {
			t.Errorf("duplicate %d", id)
		}
		seen[id] = true
		return releaseEvidence(t, "1998-10-14"), nil
	})
	f.service.watchlistReleaseProvider = provider
	other := releaseService(t, f, provider)
	for cycle := 0; cycle < 101; cycle++ {
		service := f.service
		if cycle%2 == 1 {
			service = other
		}
		r, err := service.SweepWatchlistReleases(t.Context())
		if err != nil || r.Attempted != 10 || r.Verified != 10 {
			t.Fatal("bounded progression", cycle, r, err)
		}
		if cycle == 5 {
			r, err = other.SweepWatchlistReleases(t.Context())
			var limited *RateLimitError
			if !errors.As(err, &limited) || r.Attempted != 0 || len(seen) != 60 {
				t.Fatal("global quota bypassed", r, err)
			}
		}
		if cycle >= 5 {
			f.advance(time.Minute)
		}
	}
	if len(seen) != 1010 {
		t.Fatal("global truncation", len(seen))
	}
	r, err := other.SweepWatchlistReleases(t.Context())
	if err != nil || r.Attempted != 0 {
		t.Fatal("fresh evidence retried", r, err)
	}
}

func TestWatchlistReleaseClaimsLockFreeAndStaleFinalizationIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	one := releaseSeed(t, f, 1)
	started, resume := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	provider := watchlistReleaseFunc(func(ctx context.Context, _ int64) (tmdb.ReleaseEvidence, error) {
		calls.Add(1)
		close(started)
		select {
		case <-resume:
			return releaseEvidence(t, "1998-10-14"), nil
		case <-ctx.Done():
			return tmdb.ReleaseEvidence{}, ctx.Err()
		}
	})
	f.service.watchlistReleaseProvider = provider
	other := releaseService(t, f, provider)
	// Use a single pooled connection for the worker: any retained connection
	// would prevent both the read and the other replica's claim from completing.
	cfg := f.pool.Config()
	cfg.MaxConns = 1
	single, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer single.Close()
	f.service.store = NewPostgresStore(single)
	done := make(chan error, 1)
	go func() { _, err := f.service.SweepWatchlistReleases(t.Context()); done <- err }()
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err = f.service.Watchlist(ctx, one.Cookie.Token); err != nil {
		t.Fatal("read blocked by provider", err)
	}
	if _, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "release_owner", "0", "film-1", false); err != nil {
		t.Fatal("membership/catalog lock held", err)
	}
	if r, err := other.SweepWatchlistReleases(ctx); err != nil || r.Attempted != 0 {
		t.Fatal("duplicate worker fetched", r, err)
	}
	close(resume)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate fetch")
	}
	view, err := f.service.Watchlist(t.Context(), one.Cookie.Token)
	if err != nil || len(view.Items) != 0 {
		t.Fatal("removed membership resurrected")
	}
	// Claim without finalizing, then reconstruct service: reservation survives.
	releaseSQL(t, f, `INSERT INTO account_watchlist_items(account_id,public_movie_id) SELECT id,1 FROM accounts`)
	f.advance(31 * 24 * time.Hour)
	rev, err := other.claimWatchlistRelease(t.Context(), 1)
	if err != nil || rev != 2 {
		t.Fatal(rev, err)
	}
	restarted := releaseService(t, f, nil)
	if ids, err := restarted.dueWatchlistReleases(t.Context()); err != nil || len(ids) != 0 {
		t.Fatal("reservation lost on restart", ids, err)
	}
	f.advance(15 * time.Minute)
	newRev, err := restarted.claimWatchlistRelease(t.Context(), 1)
	if err != nil || newRev != 3 {
		t.Fatal(newRev, err)
	}
	if err = restarted.finishWatchlistRelease(t.Context(), 1, newRev, releaseEvidence(t, ""), nil); err != nil {
		t.Fatal(err)
	}
	if err = other.finishWatchlistRelease(t.Context(), 1, rev, releaseEvidence(t, "2000-01-01"), nil); err != nil {
		t.Fatal(err)
	}
	var date *string
	if err = f.pool.QueryRow(t.Context(), `SELECT french_release_date::text FROM tmdb_french_release_cache WHERE tmdb_id=1`).Scan(&date); err != nil || date != nil {
		t.Fatal("stale completion overwrote negative", date, err)
	}
}

func TestWatchlistReleaseNewSaveImportAndUpcomingAdmissionIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	one := f.complete(t, "new@example.com", "release_new")
	f.service.watchlistRefresh = func(context.Context, string) error { return nil }
	f.service.watchlistProvider = watchlistFakeProvider{details: func(_ context.Context, id int64) (tmdb.Details, error) { return watchlistDetails(id), nil }}
	if _, err := f.service.ImportWatchlist(t.Context(), one.Cookie.Token, "release_new", "0", "42"); err != nil {
		t.Fatal(err)
	}
	watchlistSeedMovie(t, f, 10, "Local")
	watchlistSeedMovie(t, f, 20, "Upcoming")
	releaseSQL(t, f, `INSERT INTO tmdb_upcoming_movies(tmdb_id,public_movie_id,french_release_date,active,verified_at) VALUES(20,20,'1998-10-14',false,$1)`, f.now())
	for i, id := range []int{10, 20} {
		if _, err := f.service.SaveWatchlist(t.Context(), one.Cookie.Token, "release_new", fmt.Sprint(i+1), fmt.Sprintf("film-%d", id), true); err != nil {
			t.Fatal(err)
		}
	}
	var ids []int64
	f.service.watchlistReleaseProvider = watchlistReleaseFunc(func(_ context.Context, id int64) (tmdb.ReleaseEvidence, error) {
		ids = append(ids, id)
		return releaseEvidence(t, "1998-10-14"), nil
	})
	r, err := f.service.SweepWatchlistReleases(t.Context())
	if err != nil || r.Verified != 2 || !reflect.DeepEqual(ids, []int64{10, 42}) {
		t.Fatal("new saves/import or upcoming freshness", r, ids, err)
	}
	view, err := f.service.Watchlist(t.Context(), one.Cookie.Token)
	if err != nil || view.Revision != "3" {
		t.Fatal(view, err)
	}
	for _, item := range view.Items {
		if item.FrenchReleaseDate != "1998-10-14" {
			t.Fatal(item)
		}
	}
}

func TestWatchlistReleaseConcurrentClaimsAndDeadlineIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	releaseSeed(t, f, 2)
	other := releaseService(t, f, nil)
	start := make(chan struct{})
	type claim struct {
		revision int64
		err      error
	}
	claims := make(chan claim, 2)
	for _, service := range []*Service{f.service, other} {
		go func() {
			<-start
			revision, err := service.claimWatchlistRelease(t.Context(), 1)
			claims <- claim{revision, err}
		}()
	}
	close(start)
	winners := 0
	for range 2 {
		got := <-claims
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.revision != 0 {
			winners++
		}
	}
	if winners != 1 {
		t.Fatal("claim race winners", winners)
	}
	var providerDeadline time.Time
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.service.watchlistReleaseProvider = watchlistReleaseFunc(func(ctx context.Context, id int64) (tmdb.ReleaseEvidence, error) {
		if id != 2 {
			t.Fatal("reserved identity fetched", id)
		}
		var ok bool
		providerDeadline, ok = ctx.Deadline()
		remaining := time.Until(providerDeadline)
		if !ok || remaining <= 0 || remaining > 5*time.Second {
			t.Fatal("provider not bounded", remaining)
		}
		cancel()
		<-ctx.Done()
		return tmdb.ReleaseEvidence{}, ctx.Err()
	})
	r, err := f.service.SweepWatchlistReleases(ctx)
	if err != nil || r.Attempted != 1 || r.Unavailable != 1 || providerDeadline.IsZero() {
		t.Fatal("cancellation", r, err)
	}
	var count int
	if err = f.pool.QueryRow(t.Context(), `SELECT count(*) FROM tmdb_french_release_cache WHERE verified_at IS NULL AND french_release_date IS NULL AND retry_after=$1`, f.now().Add(15*time.Minute)).Scan(&count); err != nil || count != 2 {
		t.Fatal("canceled reservations", count, err)
	}
	// Exhausted revision is never wrapped or retried; removed saves cannot claim.
	f.advance(15 * time.Minute)
	releaseSQL(t, f, `UPDATE tmdb_french_release_cache SET attempt_revision=9007199254740991 WHERE tmdb_id=1`)
	releaseSQL(t, f, `DELETE FROM account_watchlist_items WHERE public_movie_id=2`)
	if due, err := f.service.dueWatchlistReleases(t.Context()); err != nil || len(due) != 0 {
		t.Fatal(due, err)
	}
	if revision, err := f.service.claimWatchlistRelease(t.Context(), 2); err != nil || revision != 0 {
		t.Fatal("removed identity claimed", revision, err)
	}
}
