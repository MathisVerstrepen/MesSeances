package accounts

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"messeances/api/internal/tmdb"
)

func assertWatchlistPreferences(t *testing.T, view WatchlistView, mode, filter string) {
	t.Helper()
	if view.ViewMode != mode || (filter == "" && view.FilterTagID != nil) || (filter != "" && (view.FilterTagID == nil || *view.FilterTagID != filter)) {
		t.Fatal("snapshot lost committed preferences")
	}
}

func TestWatchlistPreferencesPersistenceIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "preferences@example.com", "prefs_owner")
	two := f.complete(t, "other@example.com", "other_owner")
	raw := one.Cookie.Token
	view, err := f.service.Watchlist(ctx, raw)
	if err != nil || view.Revision != "0" {
		t.Fatal("initial read", err)
	}
	assertWatchlistPreferences(t, view, "list", "")
	view, err = f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", "0", "list", "")
	if err != nil || view.Revision != "0" {
		t.Fatal("default no-op", err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM account_watchlist_state`).Scan(&count); err != nil || count != 0 {
		t.Fatal("read/no-op initialized state", err)
	}
	view, err = f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", "0", "tags", "")
	if err != nil || view.Revision != "1" || len(view.Items) != 0 || view.SortOrder != "added_desc" {
		t.Fatal("empty-list initialization", err)
	}
	assertWatchlistPreferences(t, view, "tags", "")
	view, err = f.service.CreateWatchlistTag(ctx, raw, "prefs_owner", view.Revision, "Selected", "blue")
	if err != nil {
		t.Fatal(err)
	}
	id := view.Tags[0].ID
	other, err := f.service.CreateWatchlistTag(ctx, two.Cookie.Token, "other_owner", "0", "Other", "rose")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{other.Tags[0].ID, "9223372036854775807"} {
		if _, err := f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", "0", "list", target); !errors.Is(err, ErrWatchlistChanged) {
			t.Fatal("lookup preceded revision", err)
		}
		if _, err := f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", view.Revision, "list", target); !errors.Is(err, ErrWatchlistTagNotFound) {
			t.Fatal("foreign/absent tag not indistinguishable", err)
		}
	}
	if _, err := f.service.SaveWatchlistPreferences(ctx, two.Cookie.Token, "prefs_owner", other.Revision, "tags", id); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("owner guard", err)
	}
	persisted, err := f.service.Watchlist(ctx, raw)
	if err != nil || !reflect.DeepEqual(persisted, view) {
		t.Fatal("rejected write partially changed pair", err)
	}
	// Both fields, one field, selected empty tag and returning to defaults.
	for _, pair := range [][2]string{{"list", id}, {"tags", id}, {"tags", ""}, {"list", ""}, {"tags", id}} {
		before := view
		unrelated := watchlistTagUnrelatedState(t, f)
		view, err = f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", before.Revision, pair[0], pair[1])
		revision, _ := strconv.Atoi(before.Revision)
		if err != nil || view.Revision != strconv.Itoa(revision+1) || unrelated != watchlistTagUnrelatedState(t, f) || !reflect.DeepEqual(view.Items, before.Items) || !reflect.DeepEqual(view.Tags, before.Tags) || view.SortOrder != before.SortOrder {
			t.Fatal("pair commit changed unrelated data or wrong revision", err)
		}
		assertWatchlistPreferences(t, view, pair[0], pair[1])
		if _, err := f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", before.Revision, pair[0], pair[1]); !errors.Is(err, ErrWatchlistChanged) {
			t.Fatal("stale no-op accepted", err)
		}
		unchanged, err := f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", view.Revision, pair[0], pair[1])
		if err != nil || !reflect.DeepEqual(unchanged, view) {
			t.Fatal("no-op changed state", err)
		}
	}
	// Every ordinary write must keep the pair and return the entire watchlist.
	slug := watchlistSeedMovie(t, f, 10, "Film")
	check := func(next WatchlistView, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		assertWatchlistPreferences(t, next, "tags", id)
		view = next
	}
	check(f.service.SaveWatchlist(ctx, raw, "prefs_owner", view.Revision, slug, true))
	check(f.service.SaveWatchlistSort(ctx, raw, "prefs_owner", view.Revision, "release_asc"))
	check(f.service.AssignWatchlistTag(ctx, raw, "prefs_owner", view.Revision, slug, id, true))
	check(f.service.UpdateWatchlistTag(ctx, raw, "prefs_owner", view.Revision, id, "Renamed", "rose"))
	check(f.service.CreateWatchlistTag(ctx, raw, "prefs_owner", view.Revision, "Unselected", "neutral"))
	check(f.service.DeleteWatchlistTag(ctx, raw, "prefs_owner", view.Revision, view.Tags[1].ID))
	check(f.service.AssignWatchlistTag(ctx, raw, "prefs_owner", view.Revision, slug, id, false))
	check(f.service.SaveWatchlist(ctx, raw, "prefs_owner", view.Revision, slug, false))
	check(f.service.SaveWatchlist(ctx, raw, "prefs_owner", view.Revision, slug, true))
	// Unassigned local/imported films must remain present despite the saved filter.
	f.service.watchlistProvider = watchlistFakeProvider{details: func(_ context.Context, id int64) (tmdb.Details, error) { return watchlistDetails(id), nil }}
	f.service.watchlistRefresh = func(context.Context, string) error { return nil }
	result, err := f.service.ImportWatchlist(ctx, raw, "prefs_owner", view.Revision, "42")
	check(result.Watchlist, err)
	if len(view.Items) != 2 || view.SortOrder != "release_asc" {
		t.Fatal("filter truncated import snapshot or reset sort")
	}
	login, err := f.service.Login(ctx, "preferences@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := NewService(NewPostgresStore(f.pool), ServiceOptions{Now: f.now, Hasher: f.hasher, Origin: "https://messeances.fr", AddressHMACKey: []byte(strings.Repeat("h", 32)), WatchlistProvider: f.service.watchlistProvider})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err = fresh.Watchlist(ctx, login.Cookie.Token)
	if err != nil || !reflect.DeepEqual(persisted, view) {
		t.Fatal("fresh service/session lost preferences", err)
	}
	isolated, err := fresh.Watchlist(ctx, two.Cookie.Token)
	if err != nil {
		t.Fatal(err)
	}
	assertWatchlistPreferences(t, isolated, "list", "")
	// The import's final reread, not its pre-refresh snapshot, is authoritative.
	f.service.watchlistRefresh = func(ctx context.Context, _ string) error {
		current, err := fresh.Watchlist(ctx, login.Cookie.Token)
		if err != nil {
			return err
		}
		_, err = fresh.SaveWatchlistPreferences(ctx, login.Cookie.Token, "prefs_owner", current.Revision, "list", id)
		return err
	}
	result, err = f.service.ImportWatchlist(ctx, raw, "prefs_owner", view.Revision, "43")
	if err != nil || len(result.Watchlist.Items) != 3 {
		t.Fatal("import final reread", err)
	}
	assertWatchlistPreferences(t, result.Watchlist, "list", id)
}

func TestWatchlistPreferencesDeletionAndRevisionLimitIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "preferences@example.com", "prefs_owner")
	raw := one.Cookie.Token
	slug := watchlistSeedMovie(t, f, 10, "Film")
	view, err := f.service.CreateWatchlistTag(ctx, raw, "prefs_owner", "0", "Selected", "blue")
	if err != nil {
		t.Fatal(err)
	}
	id := view.Tags[0].ID
	for _, operation := range []func() (WatchlistView, error){
		func() (WatchlistView, error) {
			return f.service.SaveWatchlist(ctx, raw, "prefs_owner", view.Revision, slug, true)
		},
		func() (WatchlistView, error) {
			return f.service.AssignWatchlistTag(ctx, raw, "prefs_owner", view.Revision, slug, id, true)
		},
		func() (WatchlistView, error) {
			return f.service.SaveWatchlistSort(ctx, raw, "prefs_owner", view.Revision, "title_asc")
		},
		func() (WatchlistView, error) {
			return f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", view.Revision, "tags", id)
		},
	} {
		view, err = operation()
		if err != nil {
			t.Fatal(err)
		}
	}
	releaseSQL(t, f, `UPDATE account_watchlist_state SET revision=9007199254740991`)
	max := strconv.FormatInt(maxTheaterPreferenceRevision, 10)
	view, err = f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", max, "tags", id)
	if err != nil || view.Revision != max {
		t.Fatal("max no-op", err)
	}
	for _, operation := range []func() (WatchlistView, error){
		func() (WatchlistView, error) {
			return f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", max, "list", "")
		},
		func() (WatchlistView, error) { return f.service.DeleteWatchlistTag(ctx, raw, "prefs_owner", max, id) },
	} {
		if _, err := operation(); !errors.Is(err, ErrWatchlistUnavailable) {
			t.Fatal("exhausted revision changed state", err)
		}
		persisted, err := f.service.Watchlist(ctx, raw)
		if err != nil || !reflect.DeepEqual(persisted, view) {
			t.Fatal("max revision failed to roll back pair/tag/assignment", err)
		}
	}
	releaseSQL(t, f, `UPDATE account_watchlist_state SET revision=10`)
	view, err = f.service.DeleteWatchlistTag(ctx, raw, "prefs_owner", "10", id)
	if err != nil || view.Revision != "11" || view.SortOrder != "title_asc" || len(view.Items) != 1 || len(view.Items[0].TagIDs) != 0 || len(view.Tags) != 0 {
		t.Fatal("selected deletion snapshot", err)
	}
	assertWatchlistPreferences(t, view, "tags", "")
	if _, err := f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", "10", "tags", id); !errors.Is(err, ErrWatchlistChanged) {
		t.Fatal("deleted filter resurrection", err)
	}
	releaseSQL(t, f, `UPDATE account_rate_limits SET count=120 WHERE purpose='watchlist_write'`)
	var rate *RateLimitError
	if _, err := f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", view.Revision, "list", ""); !errors.As(err, &rate) {
		t.Fatal("preferences bypassed shared account write quota", err)
	}
	unchanged, err := f.service.Watchlist(ctx, raw)
	if err != nil || !reflect.DeepEqual(unchanged, view) {
		t.Fatal("quota rejection changed state", err)
	}
}

func TestWatchlistPreferencesAuthorityAndIDPrecisionIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "preferences@example.com", "prefs_owner")
	raw := one.Cookie.Token
	releaseSQL(t, f, `INSERT INTO account_watchlist_tags(account_id,id,name,name_key) OVERRIDING SYSTEM VALUE SELECT id,9223372036854775807,'Precise','precise' FROM accounts WHERE email='preferences@example.com'`)
	view, err := f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", "0", "tags", "9223372036854775807")
	if err != nil || view.Revision != "1" {
		t.Fatal("full int64 ID rejected", err)
	}
	assertWatchlistPreferences(t, view, "tags", "9223372036854775807")
	current, err := f.service.Watchlist(ctx, raw)
	if err != nil || !reflect.DeepEqual(current, view) {
		t.Fatal("read lost int64 precision", err)
	}
	if _, err := f.service.Register(ctx, "pending-preferences@example.com", testPassword); err != nil {
		t.Fatal(err)
	}
	pending, err := f.service.Login(ctx, "pending-preferences@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SaveWatchlistPreferences(ctx, pending.Cookie.Token, "prefs_owner", "1", "list", ""); !errors.Is(err, ErrPending) {
		t.Fatal("pending email authorized", err)
	}
	releaseSQL(t, f, `DELETE FROM accounts WHERE email='preferences@example.com'`)
	if _, err := f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", "1", "list", ""); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("deleted session authorized", err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM account_watchlist_state)+(SELECT count(*) FROM account_watchlist_tags)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("selected account deletion retained private state", err)
	}
}

func TestWatchlistPreferencesConcurrentCASIntegration(t *testing.T) {
	for _, mode := range []string{"preferences", "sort", "membership", "tag edit", "assignment", "selected deletion"} {
		t.Run(mode, func(t *testing.T) {
			f := newLifecycleFixture(t)
			ctx := t.Context()
			one := f.complete(t, "preferences@example.com", "prefs_owner")
			raw := one.Cookie.Token
			slug := watchlistSeedMovie(t, f, 10, "Film")
			view, err := f.service.CreateWatchlistTag(ctx, raw, "prefs_owner", "0", "Selected", "blue")
			if err != nil {
				t.Fatal(err)
			}
			id := view.Tags[0].ID
			view, err = f.service.SaveWatchlist(ctx, raw, "prefs_owner", view.Revision, slug, true)
			if err != nil {
				t.Fatal(err)
			}
			view, err = f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", view.Revision, "tags", id)
			if err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			type outcome struct {
				preferences bool
				err         error
			}
			results := make(chan outcome, 2)
			go func() {
				<-start
				_, err := f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", view.Revision, "list", id)
				results <- outcome{true, err}
			}()
			go func() {
				<-start
				var err error
				switch mode {
				case "preferences":
					_, err = f.service.SaveWatchlistPreferences(ctx, raw, "prefs_owner", view.Revision, "tags", "")
				case "sort":
					_, err = f.service.SaveWatchlistSort(ctx, raw, "prefs_owner", view.Revision, "title_asc")
				case "membership":
					_, err = f.service.SaveWatchlist(ctx, raw, "prefs_owner", view.Revision, slug, false)
				case "tag edit":
					_, err = f.service.UpdateWatchlistTag(ctx, raw, "prefs_owner", view.Revision, id, "Renamed", "rose")
				case "assignment":
					_, err = f.service.AssignWatchlistTag(ctx, raw, "prefs_owner", view.Revision, slug, id, true)
				case "selected deletion":
					_, err = f.service.DeleteWatchlistTag(ctx, raw, "prefs_owner", view.Revision, id)
				}
				results <- outcome{false, err}
			}()
			close(start)
			wins, conflicts, preferenceWon := 0, 0, false
			for range 2 {
				result := <-results
				if result.err == nil {
					wins++
					preferenceWon = result.preferences
				} else if errors.Is(result.err, ErrWatchlistChanged) {
					conflicts++
				} else {
					t.Fatal(result.err)
				}
			}
			current, err := f.service.Watchlist(ctx, raw)
			revision, _ := strconv.Atoi(view.Revision)
			if err != nil || wins != 1 || conflicts != 1 || current.Revision != strconv.Itoa(revision+1) {
				t.Fatal("shared CAS lost", err)
			}
			wantMode, wantFilter := "tags", id
			if preferenceWon {
				wantMode = "list"
			} else if mode == "preferences" || mode == "selected deletion" {
				wantFilter = ""
			}
			assertWatchlistPreferences(t, current, wantMode, wantFilter)
		})
	}
}

func TestWatchlistPreferencesFenceDelayedImportIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "preferences@example.com", "prefs_owner")
	started, resume := make(chan struct{}), make(chan struct{})
	f.service.watchlistProvider = watchlistFakeProvider{details: func(ctx context.Context, id int64) (tmdb.Details, error) {
		close(started)
		select {
		case <-resume:
			return watchlistDetails(id), nil
		case <-ctx.Done():
			return tmdb.Details{}, ctx.Err()
		}
	}}
	f.service.watchlistRefresh = func(context.Context, string) error { return nil }
	result := make(chan error, 1)
	go func() {
		_, err := f.service.ImportWatchlist(ctx, one.Cookie.Token, "prefs_owner", "0", "42")
		result <- err
	}()
	<-started
	_, saveErr := f.service.SaveWatchlistPreferences(ctx, one.Cookie.Token, "prefs_owner", "0", "tags", "")
	close(resume)
	if err := <-result; !errors.Is(err, ErrWatchlistChanged) || saveErr != nil {
		t.Fatal("preference failed to fence delayed import", saveErr, err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM account_watchlist_items)+(SELECT count(*) FROM tmdb_catalog_imports)+(SELECT count(*) FROM movie_metadata_cache)+(SELECT count(*) FROM public_movies)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("stale import published", err)
	}
}
