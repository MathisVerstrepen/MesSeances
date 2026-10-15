package accounts

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"messeances/api/internal/tmdb"
)

func watchedRows(t *testing.T, f *lifecycleFixture, table string) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestWatchedCRUDCanonicalPreferencesAndCASIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "watched@example.com", "watched_one")
	two := f.complete(t, "other@example.com", "watched_two")
	a := watchlistSeedMovie(t, f, 10, "Same title")
	b := watchlistSeedMovie(t, f, 20, "Same title")
	c := watchlistSeedMovie(t, f, 30, "Third")
	view, err := f.service.Watched(ctx, one.Cookie.Token)
	if err != nil || view.Revision != "0" || view.Items == nil || len(view.Items) != 0 || view.Preferences != defaultWatchedPreferences() {
		t.Fatalf("defaults %+v %v", view, err)
	}
	if _, err = f.service.SaveWatchedPreferences(ctx, one.Cookie.Token, "watched_one", "0", view.Preferences); err != nil {
		t.Fatal(err)
	}
	result, err := f.service.SaveWatched(ctx, one.Cookie.Token, "watched_one", "0", a, "999", false)
	if err != nil || result.Watched.Revision != "0" || result.Watchlist != nil || watchedRows(t, f, "account_watched_state") != 0 {
		t.Fatal("read/default/no-op initialized state", err)
	}
	if _, err = f.service.SaveWatched(ctx, two.Cookie.Token, "watched_one", "0", a, "0", true); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("cross-owner write accepted", err)
	}
	if _, err = f.service.SaveWatchedPreferences(ctx, two.Cookie.Token, "watched_one", "0", view.Preferences); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("cross-owner preferences accepted", err)
	}
	if _, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_one", "0", "film-999", "0", true); !errors.Is(err, ErrMovieNotFound) {
		t.Fatal("unknown movie accepted", err)
	}
	result, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_one", "0", a, "999", true)
	if err != nil || result.Watched.Revision != "1" || len(result.Watched.Items) != 1 || result.Watchlist != nil {
		t.Fatal("add failed", err)
	}
	first := result.Watched.Items[0].WatchedAt
	result, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_one", "1", a, "0", true)
	if err != nil || result.Watched.Revision != "1" || !result.Watched.Items[0].WatchedAt.Equal(first) {
		t.Fatal("no-op changed state", err)
	}
	if _, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_one", "0", "film-999", "0", true); !errors.Is(err, ErrWatchedChanged) {
		t.Fatal("revision did not precede lookup", err)
	}
	p := defaultWatchedPreferences()
	p.DefaultVisibility, p.SearchVisibility, p.HomeVisibility = "show", "hide", "show"
	view, err = f.service.SaveWatchedPreferences(ctx, one.Cookie.Token, "watched_one", "1", p)
	if err != nil || view.Revision != "2" || view.Preferences != p || !view.Items[0].WatchedAt.Equal(first) {
		t.Fatal("preferences corrupted membership", err)
	}
	f.advance(time.Minute)
	result, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_one", "2", b, "0", true)
	if err != nil || result.Watched.Revision != "3" || len(result.Watched.Items) != 2 || result.Watched.Items[0].Slug != b || result.Watched.Preferences != p {
		t.Fatal("homonym/order/preferences", err)
	}
	result, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_one", "3", c, "0", true)
	if err != nil || result.Watched.Items[0].Slug != b || result.Watched.Items[1].Slug != c {
		t.Fatal("slug tie order", err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE public_movies SET redirect_to_id=10,confirmed_tmdb_id=NULL WHERE id=20;
 INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind) VALUES('tmdb-film-20',20,'tmdb');
 INSERT INTO public_movie_metadata_overrides(public_movie_id,title_overridden,title) VALUES(10,true,'Effective title')`); err != nil {
		t.Fatal(err)
	}
	f.service.store = NewPostgresStore(f.pool)
	view, err = f.service.Watched(ctx, one.Cookie.Token)
	if err != nil || view.Revision != "4" || len(view.Items) != 2 || view.Items[1].Slug != a || !view.Items[1].WatchedAt.Equal(first) || view.Items[1].Title != "Effective title" || watchedRows(t, f, "account_watched_items") != 3 {
		t.Fatalf("canonical reload %+v %v", view, err)
	}
	result, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_one", "4", "tmdb-film-20", "0", false)
	if err != nil || result.Watched.Revision != "5" || len(result.Watched.Items) != 1 || watchedRows(t, f, "account_watched_items") != 1 {
		t.Fatal("canonical removal left hidden rows", err)
	}
	if _, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_one", "5", b, "0", true); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE public_movies SET redirect_to_id=NULL,confirmed_tmdb_id=20 WHERE id=20`); err != nil {
		t.Fatal(err)
	}
	view, err = f.service.Watched(ctx, one.Cookie.Token)
	if err != nil || len(view.Items) != 2 || view.Items[0].Slug != a {
		t.Fatal("split fanned out durable membership", err)
	}
	for i, slug := range []string{a, c} {
		result, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_one", strconv.Itoa(6+i), slug, "0", false)
		if err != nil {
			t.Fatal(err)
		}
	}
	if result.Watched.Revision != "8" || len(result.Watched.Items) != 0 || result.Watched.Preferences != p || watchedRows(t, f, "account_watched_state") != 1 {
		t.Fatal("last removal reset state")
	}
	other, err := f.service.Watched(ctx, two.Cookie.Token)
	if err != nil || other.Revision != "0" || len(other.Items) != 0 {
		t.Fatal("owner read leaked", err)
	}
	// Membership and preferences compete on the same first-write CAS.
	start := make(chan struct{})
	out := make(chan error, 2)
	go func() {
		<-start
		_, e := f.service.SaveWatched(ctx, two.Cookie.Token, "watched_two", "0", a, "0", true)
		out <- e
	}()
	go func() {
		<-start
		_, e := f.service.SaveWatchedPreferences(ctx, two.Cookie.Token, "watched_two", "0", p)
		out <- e
	}()
	close(start)
	var successes, conflicts int
	for range 2 {
		switch e := <-out; {
		case e == nil:
			successes++
		case errors.Is(e, ErrWatchedChanged):
			conflicts++
		default:
			t.Fatal(e)
		}
	}
	other, err = f.service.Watched(ctx, two.Cookie.Token)
	if err != nil || successes != 1 || conflicts != 1 || other.Revision != "1" {
		t.Fatal("shared first-write CAS lost", err)
	}
}

func TestWatchedAuthorizationBoundsAndExhaustionIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	slug := watchlistSeedMovie(t, f, 1, "Film")
	deny := func(raw string, want error) {
		t.Helper()
		if _, err := f.service.Watched(ctx, raw); !errors.Is(err, want) {
			t.Fatal("read authorization", err)
		}
		if _, err := f.service.SaveWatched(ctx, raw, "watched_bounds", "0", slug, "0", true); !errors.Is(err, want) {
			t.Fatal("write authorization", err)
		}
		if _, err := f.service.SaveWatchedPreferences(ctx, raw, "watched_bounds", "0", defaultWatchedPreferences()); !errors.Is(err, want) {
			t.Fatal("preference authorization", err)
		}
	}
	deny("", ErrUnauthorized)
	pending := f.pending(t, "pending@example.com")
	deny(pending.Cookie.Token, ErrPending)
	one := f.complete(t, "owner@example.com", "watched_bounds")
	if err := f.service.Logout(ctx, one.Cookie.Token, true); err != nil {
		t.Fatal(err)
	}
	deny(one.Cookie.Token, ErrUnauthorized)
	one, err := f.service.Login(ctx, "owner@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO public_movies(id,identity_anchor_tmdb_id,confirmed_tmdb_id,title,runtime_minutes) OVERRIDING SYSTEM VALUE SELECT n,n,n,'Film',0 FROM generate_series(2,1001)n;
 INSERT INTO account_watched_items(account_id,public_movie_id) SELECT a.id,p.id FROM accounts a CROSS JOIN public_movies p WHERE a.email='owner@example.com' AND p.id<=1000`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_bounds", "0", "film-1001", "0", true); !errors.Is(err, ErrWatchedLimit) {
		t.Fatal("stored row bound", err)
	}
	if _, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_bounds", "0", slug, "0", true); err != nil {
		t.Fatal("full no-op", err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO account_watched_state(account_id,revision) SELECT id,$1 FROM accounts WHERE email='owner@example.com'`, maxTheaterPreferenceRevision); err != nil {
		t.Fatal(err)
	}
	max := strconv.FormatInt(maxTheaterPreferenceRevision, 10)
	if _, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_bounds", max, slug, "0", false); !errors.Is(err, ErrWatchedUnavailable) {
		t.Fatal("overflow", err)
	}
	if _, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_bounds", max, slug, "0", true); err != nil {
		t.Fatal("maximum no-op", err)
	}
	if _, err = f.service.SaveWatchedPreferences(ctx, one.Cookie.Token, "watched_bounds", max, defaultWatchedPreferences()); err != nil {
		t.Fatal("maximum preference no-op", err)
	}
	p := defaultWatchedPreferences()
	p.DefaultVisibility = "show"
	if _, err = f.service.SaveWatchedPreferences(ctx, one.Cookie.Token, "watched_bounds", max, p); !errors.Is(err, ErrWatchedUnavailable) {
		t.Fatal("preference overflow", err)
	}
	if watchedRows(t, f, "account_watched_items") != 1000 {
		t.Fatal("overflow changed membership")
	}
	view, err := f.service.Watched(ctx, one.Cookie.Token)
	if err != nil || view.Revision != max || view.Preferences != defaultWatchedPreferences() {
		t.Fatal("overflow changed state", err)
	}
	f.advance(SessionIdleLifetime)
	deny(one.Cookie.Token, ErrUnauthorized)
}

func TestWatchedAtomicWatchlistRemovalIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "atomic@example.com", "watched_atomic")
	a := watchlistSeedMovie(t, f, 10, "A")
	b := watchlistSeedMovie(t, f, 20, "B")
	c := watchlistSeedMovie(t, f, 30, "C")
	list, err := f.service.SaveWatchlist(ctx, one.Cookie.Token, "watched_atomic", "0", a, true)
	if err != nil {
		t.Fatal(err)
	}
	list, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "watched_atomic", list.Revision, b, true)
	if err != nil {
		t.Fatal(err)
	}
	list, err = f.service.CreateWatchlistTag(ctx, one.Cookie.Token, "watched_atomic", list.Revision, "Keep", "violet")
	if err != nil {
		t.Fatal(err)
	}
	tag := list.Tags[0].ID
	for _, slug := range []string{a, b} {
		list, err = f.service.AssignWatchlistTag(ctx, one.Cookie.Token, "watched_atomic", list.Revision, slug, tag, true)
		if err != nil {
			t.Fatal(err)
		}
	}
	list, err = f.service.SaveWatchlistPreferences(ctx, one.Cookie.Token, "watched_atomic", list.Revision, "tags", tag)
	if err != nil {
		t.Fatal(err)
	}
	list, err = f.service.SaveWatchlistSort(ctx, one.Cookie.Token, "watched_atomic", list.Revision, "title_desc")
	if err != nil {
		t.Fatal(err)
	}
	before := list
	beforeRevision, err := theaterRevision(before.Revision)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.service.SaveWatched(ctx, one.Cookie.Token, "watched_atomic", "0", a, "0", true)
	if err != nil || result.Watchlist != nil {
		t.Fatal("off should ignore well-formed stale watchlist revision", err)
	}
	p := defaultWatchedPreferences()
	p.AutoRemoveWatchlist = true
	view, err := f.service.SaveWatchedPreferences(ctx, one.Cookie.Token, "watched_atomic", "1", p)
	if err != nil {
		t.Fatal(err)
	}
	list, err = f.service.Watchlist(ctx, one.Cookie.Token)
	if err != nil || !reflect.DeepEqual(list, before) {
		t.Fatal("toggle swept watchlist", err)
	}
	if _, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_atomic", view.Revision, a, "0", true); !errors.Is(err, ErrWatchlistChanged) {
		t.Fatal("stale no-op not fenced", err)
	}
	result, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_atomic", view.Revision, a, list.Revision, true)
	if err != nil || result.Watched.Revision != "2" || result.Watchlist == nil || !reflect.DeepEqual(*result.Watchlist, before) {
		t.Fatal("repeated watched swept or missing full snapshot", err)
	}
	result, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_atomic", "2", a, "0", false)
	if err != nil || result.Watchlist != nil {
		t.Fatal("unwatch touched watchlist", err)
	}
	result, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_atomic", "3", a, list.Revision, true)
	if err != nil || result.Watched.Revision != "4" || result.Watchlist == nil {
		t.Fatal("atomic add", err)
	}
	list = *result.Watchlist
	if list.Revision != strconv.FormatInt(beforeRevision+1, 10) || len(list.Items) != 1 || list.Items[0].Slug != b || !reflect.DeepEqual(list.Tags, before.Tags) || list.ViewMode != before.ViewMode || !reflect.DeepEqual(list.FilterTagID, before.FilterTagID) || list.SortOrder != before.SortOrder || !reflect.DeepEqual(list.Items[0], before.Items[1]) || watchedRows(t, f, "account_watchlist_item_tags") != 1 {
		t.Fatalf("collateral watchlist mutation: %+v before %+v", list, before)
	}
	committed, err := f.service.Watched(ctx, one.Cookie.Token)
	if err != nil || !reflect.DeepEqual(committed, result.Watched) {
		t.Fatal("uncommitted watched snapshot", err)
	}
	saved, err := f.service.Watchlist(ctx, one.Cookie.Token)
	if err != nil || !reflect.DeepEqual(saved, list) {
		t.Fatal("uncommitted watchlist snapshot", err)
	}
	// No saved membership: full snapshot but no watchlist revision advance.
	result, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_atomic", "4", c, list.Revision, true)
	if err != nil || result.Watched.Revision != "5" || result.Watchlist == nil || !reflect.DeepEqual(*result.Watchlist, list) {
		t.Fatal("unsaved transition advanced watchlist", err)
	}
	// Explicit re-add remains allowed; a repeated watched save must not sweep it.
	list, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "watched_atomic", list.Revision, a, true)
	if err != nil {
		t.Fatal(err)
	}
	result, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_atomic", "5", a, list.Revision, true)
	if err != nil || result.Watchlist == nil || !reflect.DeepEqual(*result.Watchlist, list) {
		t.Fatal("later explicit save removed", err)
	}
	// Both revision exhaustion directions roll back both resources.
	max := strconv.FormatInt(maxTheaterPreferenceRevision, 10)
	for _, resource := range []string{"account_watchlist_state", "account_watched_state"} {
		if _, err = f.pool.Exec(ctx, `UPDATE account_watchlist_state SET revision=10; UPDATE account_watched_state SET revision=10`); err != nil {
			t.Fatal(err)
		}
		if _, err = f.pool.Exec(ctx, "UPDATE "+resource+" SET revision=$1", maxTheaterPreferenceRevision); err != nil {
			t.Fatal(err)
		}
		wr, lr := "10", "10"
		want := ErrWatchlistUnavailable
		if resource == "account_watchlist_state" {
			lr = max
		} else {
			wr = max
			want = ErrWatchedUnavailable
		}
		if _, err = f.service.SaveWatched(ctx, one.Cookie.Token, "watched_atomic", wr, b, lr, true); !errors.Is(err, want) {
			t.Fatal("atomic overflow", err)
		}
		w, e := f.service.Watched(ctx, one.Cookie.Token)
		if e != nil || w.Revision != wr || len(w.Items) != 2 {
			t.Fatal("watched rollback", e)
		}
		l, e := f.service.Watchlist(ctx, one.Cookie.Token)
		if e != nil || l.Revision != lr || len(l.Items) != 2 || watchedRows(t, f, "account_watchlist_item_tags") != 1 {
			t.Fatal("watchlist rollback", e)
		}
	}
}

func TestWatchedCompetingWatchlistAndDelayedImportIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "race@example.com", "watched_race")
	a := watchlistSeedMovie(t, f, 10, "A")
	b := watchlistSeedMovie(t, f, 20, "B")
	list, err := f.service.SaveWatchlist(ctx, one.Cookie.Token, "watched_race", "0", a, true)
	if err != nil {
		t.Fatal(err)
	}
	p := defaultWatchedPreferences()
	p.AutoRemoveWatchlist = true
	if _, err = f.service.SaveWatchedPreferences(ctx, one.Cookie.Token, "watched_race", "0", p); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	out := make(chan error, 2)
	go func() {
		<-start
		_, e := f.service.SaveWatched(ctx, one.Cookie.Token, "watched_race", "1", a, list.Revision, true)
		out <- e
	}()
	go func() {
		<-start
		_, e := f.service.SaveWatchlist(ctx, one.Cookie.Token, "watched_race", list.Revision, b, true)
		out <- e
	}()
	close(start)
	var successes, conflicts int
	for range 2 {
		e := <-out
		if e == nil {
			successes++
		} else if errors.Is(e, ErrWatchlistChanged) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	list, err = f.service.Watchlist(ctx, one.Cookie.Token)
	w, e := f.service.Watched(ctx, one.Cookie.Token)
	if err != nil || e != nil || successes != 1 || conflicts != 1 || list.Revision != "2" || (w.Revision == "2" && len(list.Items) != 0) || (w.Revision == "1" && len(list.Items) != 2) {
		t.Fatal("competing save lost CAS", err, e)
	}
	// Reset by explicit mutations, then compete a tag edit against removal.
	if len(w.Items) != 0 {
		r, e := f.service.SaveWatched(ctx, one.Cookie.Token, "watched_race", w.Revision, a, "0", false)
		if e != nil {
			t.Fatal(e)
		}
		w = r.Watched
	}
	list, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "watched_race", list.Revision, a, true)
	if err != nil {
		t.Fatal(err)
	}
	list, err = f.service.CreateWatchlistTag(ctx, one.Cookie.Token, "watched_race", list.Revision, "Tag", "red")
	if err != nil {
		t.Fatal(err)
	}
	start = make(chan struct{})
	wr, lr, tag := w.Revision, list.Revision, list.Tags[0].ID
	go func() {
		<-start
		_, e := f.service.SaveWatched(ctx, one.Cookie.Token, "watched_race", wr, a, lr, true)
		out <- e
	}()
	go func() {
		<-start
		_, e := f.service.UpdateWatchlistTag(ctx, one.Cookie.Token, "watched_race", lr, tag, "Edited", "blue")
		out <- e
	}()
	close(start)
	successes, conflicts = 0, 0
	for range 2 {
		e := <-out
		if e == nil {
			successes++
		} else if errors.Is(e, ErrWatchlistChanged) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal("tag edit lost CAS")
	}
	list, err = f.service.Watchlist(ctx, one.Cookie.Token)
	if err != nil {
		t.Fatal(err)
	}
	w, err = f.service.Watched(ctx, one.Cookie.Token)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Items) != 0 {
		r, e := f.service.SaveWatched(ctx, one.Cookie.Token, "watched_race", w.Revision, a, "0", false)
		if e != nil {
			t.Fatal(e)
		}
		w = r.Watched
	}
	list, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "watched_race", list.Revision, a, true)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	f.service.watchlistProvider = watchlistFakeProvider{details: func(context.Context, int64) (tmdb.Details, error) {
		close(entered)
		<-release
		return watchlistDetails(42), nil
	}}
	f.service.watchlistRefresh = func(context.Context, string) error { return nil }
	imported := make(chan error, 1)
	go func() {
		_, e := f.service.ImportWatchlist(ctx, one.Cookie.Token, "watched_race", list.Revision, "42")
		imported <- e
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("import did not reach provider")
	}
	r, err := f.service.SaveWatched(ctx, one.Cookie.Token, "watched_race", w.Revision, a, list.Revision, true)
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if err = <-imported; !errors.Is(err, ErrWatchlistChanged) {
		t.Fatal("delayed import ignored atomic removal", err)
	}
	committed, err := f.service.Watchlist(ctx, one.Cookie.Token)
	if err != nil || r.Watchlist == nil || !reflect.DeepEqual(committed, *r.Watchlist) {
		t.Fatal("delayed import changed membership", err)
	}
	var n int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM public_movies WHERE confirmed_tmdb_id=42`).Scan(&n); err != nil || n != 0 {
		t.Fatal("stale import published", err)
	}
}

func TestWatchedCanonicalAtomicRemovalAndPrecommitRollbackIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "rollback@example.com", "watched_rollback")
	a := watchlistSeedMovie(t, f, 10, "A")
	b := watchlistSeedMovie(t, f, 20, "B")
	list, err := f.service.SaveWatchlist(ctx, one.Cookie.Token, "watched_rollback", "0", a, true)
	if err != nil {
		t.Fatal(err)
	}
	list, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "watched_rollback", list.Revision, b, true)
	if err != nil {
		t.Fatal(err)
	}
	list, err = f.service.CreateWatchlistTag(ctx, one.Cookie.Token, "watched_rollback", list.Revision, "Keep", "green")
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{a, b} {
		list, err = f.service.AssignWatchlistTag(ctx, one.Cookie.Token, "watched_rollback", list.Revision, slug, list.Tags[0].ID, true)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.pool.Exec(ctx, `UPDATE public_movies SET redirect_to_id=10,confirmed_tmdb_id=NULL WHERE id=20`); err != nil {
		t.Fatal(err)
	}
	p := defaultWatchedPreferences()
	p.AutoRemoveWatchlist = true
	if _, err = f.service.SaveWatchedPreferences(ctx, one.Cookie.Token, "watched_rollback", "0", p); err != nil {
		t.Fatal(err)
	}
	beforeW, err := f.service.Watched(ctx, one.Cookie.Token)
	if err != nil {
		t.Fatal(err)
	}
	beforeL, err := f.service.Watchlist(ctx, one.Cookie.Token)
	if err != nil {
		t.Fatal(err)
	}
	// First force a write failure after watched insertion, then a final snapshot
	// scan failure after both writes. Neither permits a partial commit.
	for _, body := range []string{`RAISE EXCEPTION 'synthetic failure';`, `UPDATE account_watched_items SET watched_at='infinity'; RETURN OLD;`} {
		if _, err = f.pool.Exec(ctx, `CREATE FUNCTION watched_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN `+body+` END $$;
 CREATE TRIGGER watched_fail BEFORE DELETE ON account_watchlist_items FOR EACH ROW EXECUTE FUNCTION watched_fail()`); err != nil {
			t.Fatal(err)
		}
		result, e := f.service.SaveWatched(ctx, one.Cookie.Token, "watched_rollback", "1", b, list.Revision, true)
		if e == nil || result.Watchlist != nil || result.Watched.Revision != "" {
			t.Fatal("precommit failure returned success/snapshot", e)
		}
		w, e := f.service.Watched(ctx, one.Cookie.Token)
		if e != nil || !reflect.DeepEqual(w, beforeW) {
			t.Fatal("precommit watched rollback", e)
		}
		l, e := f.service.Watchlist(ctx, one.Cookie.Token)
		if e != nil || !reflect.DeepEqual(l, beforeL) || watchedRows(t, f, "account_watchlist_items") != 2 || watchedRows(t, f, "account_watchlist_item_tags") != 2 {
			t.Fatal("precommit watchlist rollback", e)
		}
		if _, err = f.pool.Exec(ctx, `DROP TRIGGER watched_fail ON account_watchlist_items; DROP FUNCTION watched_fail()`); err != nil {
			t.Fatal(err)
		}
	}
	result, err := f.service.SaveWatched(ctx, one.Cookie.Token, "watched_rollback", "1", b, list.Revision, true)
	if err != nil || result.Watched.Revision != "2" || len(result.Watched.Items) != 1 || result.Watched.Items[0].Slug != a || result.Watchlist == nil || len(result.Watchlist.Items) != 0 || len(result.Watchlist.Tags) != 1 || watchedRows(t, f, "account_watchlist_items") != 0 || watchedRows(t, f, "account_watchlist_item_tags") != 0 {
		t.Fatal("merged atomic removal", err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE public_movies SET redirect_to_id=NULL,confirmed_tmdb_id=20 WHERE id=20`); err != nil {
		t.Fatal(err)
	}
	l, err := f.service.Watchlist(ctx, one.Cookie.Token)
	if err != nil || len(l.Items) != 0 {
		t.Fatal("split restored removed watchlist rows", err)
	}
}

func TestWatchedSharesPersistedWatchlistQuotaIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "quota@example.com", "watched_quota")
	a := watchlistSeedMovie(t, f, 10, "A")
	for range 60 {
		if _, err := f.service.SaveWatched(ctx, one.Cookie.Token, "watched_quota", "0", a, "0", false); err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.SaveWatchedPreferences(ctx, one.Cookie.Token, "watched_quota", "0", defaultWatchedPreferences()); err != nil {
			t.Fatal(err)
		}
	}
	var rate *RateLimitError
	if _, err := f.service.SaveWatchlist(ctx, one.Cookie.Token, "watched_quota", "0", a, true); !errors.As(err, &rate) {
		t.Fatal("watchlist has additive quota", err)
	}
	if _, err := f.service.SaveWatched(ctx, one.Cookie.Token, "watched_quota", "0", a, "0", true); !errors.As(err, &rate) {
		t.Fatal("watched has additive quota", err)
	}
	if watchedRows(t, f, "account_watched_state") != 0 || watchedRows(t, f, "account_watched_items") != 0 || watchedRows(t, f, "account_watchlist_items") != 0 {
		t.Fatal("limited write mutated data")
	}
}
