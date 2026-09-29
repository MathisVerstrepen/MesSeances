package accounts

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/schedule"
	"messeances/api/internal/schedulepg"
	"messeances/api/internal/tmdb"
)

type watchlistFakeProvider struct {
	search  func(context.Context, string) ([]tmdb.Candidate, error)
	details func(context.Context, int64) (tmdb.Details, error)
}

func (p watchlistFakeProvider) Search(ctx context.Context, q string) ([]tmdb.Candidate, error) {
	return p.search(ctx, q)
}
func (p watchlistFakeProvider) Details(ctx context.Context, id int64) (tmdb.Details, error) {
	return p.details(ctx, id)
}

func watchlistDetails(id int64) tmdb.Details {
	adult := false
	return tmdb.Details{ID: id, Adult: &adult, Title: "Imported film", OriginalTitle: "Original", Runtime: 0, ReleaseDate: "1999-01-01", Genres: []string{}}
}

func watchlistSeedMovie(t *testing.T, f *lifecycleFixture, id int64, title string) string {
	t.Helper()
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO public_movies(id,identity_anchor_tmdb_id,confirmed_tmdb_id,title,runtime_minutes) OVERRIDING SYSTEM VALUE VALUES($1,$1,$1,$2,0)`, id, title); err != nil {
		t.Fatal(err)
	}
	return "film-" + strconv.FormatInt(id, 10)
}

func TestWatchlistCRUDCanonicalAndCASIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	one := f.complete(t, "watchlist@example.com", "watchlist_one")
	two := f.complete(t, "other@example.com", "watchlist_two")
	ctx := t.Context()
	first := watchlistSeedMovie(t, f, 10, "Same title")
	second := watchlistSeedMovie(t, f, 20, "Same title")
	view, err := f.service.Watchlist(ctx, one.Cookie.Token)
	if err != nil || view.Revision != "0" || view.Items == nil || len(view.Items) != 0 || view.ExternalSearchAvailable {
		t.Fatalf("initial %+v %v", view, err)
	}
	var count int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM account_watchlist_state`).Scan(&count); err != nil || count != 0 {
		t.Fatal("read initialized state")
	}
	if _, err = f.service.SaveWatchlist(ctx, two.Cookie.Token, "watchlist_one", "0", first, true); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("cross-owner accepted")
	}
	if _, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "watchlist_one", "0", "film-999", true); !errors.Is(err, ErrMovieNotFound) {
		t.Fatal("unknown movie accepted")
	}
	view, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "watchlist_one", "0", first, false)
	if err != nil || view.Revision != "0" {
		t.Fatal("empty remove initialized state")
	}
	view, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "watchlist_one", "0", first, true)
	if err != nil || view.Revision != "1" || len(view.Items) != 1 {
		t.Fatalf("add %+v %v", view, err)
	}
	firstAdded := view.Items[0].AddedAt
	view, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "watchlist_one", "1", first, true)
	if err != nil || view.Revision != "1" {
		t.Fatal("no-op changed revision")
	}
	if _, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "watchlist_one", "0", first, true); !errors.Is(err, ErrWatchlistChanged) {
		t.Fatal("stale no-op accepted")
	}
	f.advance(time.Minute)
	view, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "watchlist_one", "1", second, true)
	if err != nil || view.Revision != "2" || len(view.Items) != 2 || view.Items[0].Slug != second {
		t.Fatal("homonyms conflated or chronology lost")
	}
	// Simulate a canonical merge. Private durable IDs are untouched by catalog writers.
	if _, err = f.pool.Exec(ctx, `UPDATE public_movies SET redirect_to_id=10,confirmed_tmdb_id=NULL WHERE id=20`); err != nil {
		t.Fatal(err)
	}
	view, err = f.service.Watchlist(ctx, one.Cookie.Token)
	if err != nil || len(view.Items) != 1 || view.Items[0].Slug != first || !view.Items[0].AddedAt.Equal(firstAdded) || view.Revision != "2" {
		t.Fatalf("merge %+v %v", view, err)
	}
	view, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "watchlist_one", "2", second, false)
	if err != nil || view.Revision != "3" || len(view.Items) != 0 {
		t.Fatal("redirect removal did not clear all durable rows")
	}
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM account_watchlist_items`).Scan(&count); err != nil || count != 0 {
		t.Fatal("merge removal left hidden membership")
	}
	// Save via old canonical slug, then split. Saved durable target follows only its own ID.
	if _, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "watchlist_one", "3", second, true); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE public_movies SET redirect_to_id=NULL,confirmed_tmdb_id=20 WHERE id=20`); err != nil {
		t.Fatal(err)
	}
	view, err = f.service.Watchlist(ctx, one.Cookie.Token)
	if err != nil || len(view.Items) != 1 || view.Items[0].Slug != first {
		t.Fatal("split fanned out membership")
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind) VALUES('tmdb-film-10',10,'tmdb')`); err != nil {
		t.Fatal(err)
	}
	view, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "watchlist_one", "4", "tmdb-film-10", true)
	if err != nil || view.Revision != "4" || view.Items[0].Slug != first {
		t.Fatalf("alias %+v %v", view, err)
	}
	// Two first-time sessions share the same account compare-and-swap lock.
	other, err := f.service.Login(ctx, "other@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	outcomes := make(chan error, 2)
	for i, raw := range []string{two.Cookie.Token, other.Cookie.Token} {
		go func() {
			<-start
			_, err := f.service.SaveWatchlist(ctx, raw, "watchlist_two", "0", []string{first, second}[i], true)
			outcomes <- err
		}()
	}
	close(start)
	var successes, conflicts int
	for range 2 {
		err := <-outcomes
		if err == nil {
			successes++
		} else if errors.Is(err, ErrWatchlistChanged) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal("first-write CAS lost")
	}
}

func TestWatchlistAuthorizationAndBoundsIntegration(t *testing.T) {
	const username = "watchlist_bounds"
	f := newLifecycleFixture(t)
	ctx := t.Context()
	slug := watchlistSeedMovie(t, f, 1, "Film")
	assertDenied := func(raw string, want error) {
		t.Helper()
		if _, err := f.service.Watchlist(ctx, raw); !errors.Is(err, want) {
			t.Fatalf("read %v want %v", err, want)
		}
		if _, err := f.service.SaveWatchlist(ctx, raw, username, "0", slug, true); !errors.Is(err, want) {
			t.Fatalf("write %v want %v", err, want)
		}
		if _, err := f.service.SaveWatchlistSort(ctx, raw, username, "0", "title_asc"); !errors.Is(err, want) {
			t.Fatalf("sort %v want %v", err, want)
		}
		for _, operation := range []func() error{
			func() error {
				_, err := f.service.CreateWatchlistTag(ctx, raw, username, "0", "Tag", "neutral")
				return err
			},
			func() error {
				_, err := f.service.UpdateWatchlistTag(ctx, raw, username, "0", "1", "Tag", "red")
				return err
			},
			func() error { _, err := f.service.DeleteWatchlistTag(ctx, raw, username, "0", "1"); return err },
			func() error {
				_, err := f.service.AssignWatchlistTag(ctx, raw, username, "0", slug, "1", true)
				return err
			},
		} {
			if err := operation(); !errors.Is(err, want) {
				t.Fatalf("tag authorization %v want %v", err, want)
			}
		}
		if _, err := f.service.SearchWatchlist(ctx, raw, username, "Film"); !errors.Is(err, want) {
			t.Fatalf("search %v want %v", err, want)
		}
		if _, err := f.service.ImportWatchlist(ctx, raw, username, "0", "1"); !errors.Is(err, want) {
			t.Fatalf("import %v want %v", err, want)
		}
	}
	assertDenied("", ErrUnauthorized)
	pending := f.pending(t, "pending@example.com")
	assertDenied(pending.Cookie.Token, ErrPending)
	one := f.complete(t, "owner@example.com", username)
	if err := f.service.Logout(ctx, one.Cookie.Token, true); err != nil {
		t.Fatal(err)
	}
	assertDenied(one.Cookie.Token, ErrUnauthorized)
	one, err := f.service.Login(ctx, "owner@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	// Fill with stored durable rows, not materialized canonical count.
	if _, err = f.pool.Exec(ctx, `INSERT INTO public_movies(id,identity_anchor_tmdb_id,confirmed_tmdb_id,title,runtime_minutes) OVERRIDING SYSTEM VALUE SELECT n,n,n,'Film',0 FROM generate_series(2,1001)n`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO account_watchlist_items(account_id,public_movie_id) SELECT a.id,p.id FROM accounts a CROSS JOIN public_movies p WHERE a.email='owner@example.com' AND p.id<=1000`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, username, "0", "film-1001", true); !errors.Is(err, ErrWatchlistLimit) {
		t.Fatal("stored row bound not enforced")
	}
	if _, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, username, "0", slug, true); err != nil {
		t.Fatal("full watchlist no-op failed")
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO account_watchlist_state(account_id,revision) SELECT id,$1 FROM accounts WHERE email='owner@example.com'`, maxTheaterPreferenceRevision); err != nil {
		t.Fatal(err)
	}
	max := strconv.FormatInt(maxTheaterPreferenceRevision, 10)
	if _, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, username, max, slug, false); !errors.Is(err, ErrWatchlistUnavailable) {
		t.Fatal("revision overflow accepted")
	}
	if _, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, username, max, slug, true); err != nil {
		t.Fatal("maximum revision no-op failed")
	}
	if _, err = f.service.SaveWatchlistSort(ctx, one.Cookie.Token, username, max, "added_desc"); err != nil {
		t.Fatal("maximum revision sort no-op failed", err)
	}
	if _, err = f.service.SaveWatchlistSort(ctx, one.Cookie.Token, username, max, "title_asc"); !errors.Is(err, ErrWatchlistUnavailable) {
		t.Fatal("sort revision overflow accepted", err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE account_watchlist_state SET revision=1`); err != nil {
		t.Fatal(err)
	}
	view, err := f.service.SaveWatchlistSort(ctx, one.Cookie.Token, username, "1", "title_asc")
	if err != nil || view.Revision != "2" || view.SortOrder != "title_asc" || len(view.Items) != 1000 {
		t.Fatal("sort at capacity failed", err)
	}
	f.advance(SessionIdleLifetime)
	assertDenied(one.Cookie.Token, ErrUnauthorized)
}

func TestWatchlistSearchLiteralOverrideDedupIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	one := f.complete(t, "search@example.com", "search_owner")
	for id := int64(1); id <= 22; id++ {
		watchlistSeedMovie(t, f, id, fmt.Sprintf("Film %02d", id))
	}
	watchlistSeedMovie(t, f, 99, "Unrelated known movie")
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO public_movie_metadata_overrides(public_movie_id,title_overridden,title) VALUES(1,true,'Literal %_ movie')`); err != nil {
		t.Fatal(err)
	}
	f.service.watchlistProvider = watchlistFakeProvider{search: func(_ context.Context, q string) ([]tmdb.Candidate, error) {
		if q != "Film" && q != "%_" {
			t.Error("query not trimmed")
		}
		return []tmdb.Candidate{{ID: 99, Title: "Known outside local page"}, {ID: 100, Title: "Film 02"}, {ID: 100, Title: "Duplicate"}}, nil
	}}
	view, err := f.service.SearchWatchlist(t.Context(), one.Cookie.Token, "search_owner", "  Film  ")
	if err != nil || len(view.Catalog) != 20 || !view.CatalogHasMore || len(view.External) != 1 || view.External[0].TMDBID != "100" || view.ExternalStatus != "ready" {
		t.Fatalf("search %+v %v", view, err)
	}
	view, err = f.service.SearchWatchlist(t.Context(), one.Cookie.Token, "search_owner", "%_")
	if err != nil || len(view.Catalog) != 1 || view.Catalog[0].Slug != "film-1" {
		t.Fatal("wildcards not literal or override ignored")
	}
	f.service.watchlistProvider = watchlistFakeProvider{search: func(context.Context, string) ([]tmdb.Candidate, error) {
		return nil, errors.New("secret provider body")
	}}
	view, err = f.service.SearchWatchlist(t.Context(), one.Cookie.Token, "search_owner", "Film")
	if err != nil || view.ExternalStatus != "unavailable" || len(view.Catalog) != 20 || view.External == nil {
		t.Fatal("provider failure discarded local results")
	}
	f.service.watchlistProvider = nil
	view, err = f.service.SearchWatchlist(t.Context(), one.Cookie.Token, "search_owner", "Film")
	if err != nil || view.ExternalStatus != "disabled" {
		t.Fatal("disabled external search failed local search")
	}
}

func watchlistPublicService(t *testing.T, f *lifecycleFixture) *schedule.Service {
	t.Helper()
	source, err := schedule.NewPostgresSource(t.Context(), schedulepg.NewStore(f.pool))
	if err != nil {
		t.Fatal(err)
	}
	service, err := schedule.NewService(source, schedule.ServiceOptions{Now: f.now})
	if err != nil {
		t.Fatal(err)
	}
	f.service.watchlistRefresh = service.RefreshPublishedMovie
	return service
}

func TestWatchlistImportAtomicPersistentPublicCatalogIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	one := f.complete(t, "import@example.com", "import_owner")
	public := watchlistPublicService(t, f)
	otherReplicaSource, err := schedule.NewPostgresSource(t.Context(), schedulepg.NewStore(f.pool))
	if err != nil {
		t.Fatal(err)
	}
	otherReplica, err := schedule.NewService(otherReplicaSource, schedule.ServiceOptions{Now: f.now})
	if err != nil {
		t.Fatal(err)
	}
	f.service.watchlistProvider = watchlistFakeProvider{details: func(_ context.Context, id int64) (tmdb.Details, error) { return watchlistDetails(id), nil }}
	result, err := f.service.ImportWatchlist(t.Context(), one.Cookie.Token, "import_owner", "0", "42")
	if err != nil || result.Watchlist.Revision != "1" || len(result.Watchlist.Items) != 1 || result.MovieSlug != result.Watchlist.Items[0].Slug {
		t.Fatalf("import %+v %v", result, err)
	}
	if !public.HasCatalog() || public.HasSnapshot() {
		t.Fatal("import invented schedule availability")
	}
	if _, err = public.UpcomingMovies(schedule.UpcomingMoviesQuery{}); err == nil {
		t.Fatal("import invented upcoming completion")
	}
	if err = otherReplica.RefreshMovie(t.Context(), result.MovieSlug); err != nil {
		t.Fatal("cold replica did not refresh missing committed movie")
	}
	detail, err := otherReplica.MovieShowtimes(schedule.MovieShowtimesQuery{Slug: result.MovieSlug, Date: "2026-09-22"})
	if err != nil || detail.CurrentlyScreened || detail.Theaters == nil || len(detail.Theaters) != 0 || detail.Movie.RuntimeMinutes != 0 {
		t.Fatalf("public detail %+v %v", detail, err)
	}
	result, err = f.service.ImportWatchlist(t.Context(), one.Cookie.Token, "import_owner", "1", "42")
	if err != nil || result.Watchlist.Revision != "1" {
		t.Fatal("reimport not idempotent membership")
	}
	if _, err = f.service.SaveWatchlist(t.Context(), one.Cookie.Token, "import_owner", "1", result.MovieSlug, false); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(t.Context(), `DELETE FROM accounts WHERE email='import@example.com'`); err != nil {
		t.Fatal(err)
	}
	var imports, items, state, movies, upcoming int
	if err = f.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM tmdb_catalog_imports),(SELECT count(*) FROM account_watchlist_items),(SELECT count(*) FROM account_watchlist_state),(SELECT count(*) FROM public_movies WHERE redirect_to_id IS NULL),(SELECT count(*) FROM tmdb_upcoming_movies)`).Scan(&imports, &items, &state, &movies, &upcoming); err != nil || imports != 1 || items != 0 || state != 0 || movies != 1 || upcoming != 0 {
		t.Fatal("private deletion deleted public evidence or leaked account state")
	}
}

func TestWatchlistImportEligibilityAndRollbackIntegration(t *testing.T) {
	for _, mode := range []string{"adult", "unknown", "missing", "invalid metadata", "stale", "db failure", "refresh failure"} {
		t.Run(mode, func(t *testing.T) {
			f := newLifecycleFixture(t)
			one := f.complete(t, "failure@example.com", "failure_owner")
			f.service.watchlistRefresh = func(context.Context, string) error { return nil }
			f.service.watchlistProvider = watchlistFakeProvider{details: func(_ context.Context, id int64) (tmdb.Details, error) {
				d := watchlistDetails(id)
				switch mode {
				case "adult":
					*d.Adult = true
				case "unknown":
					d.Adult = nil
				case "missing":
					return d, tmdb.ErrNotFound
				case "invalid metadata":
					d.ReleaseDate = "not-a-date"
				}
				return d, nil
			}}
			expected := "0"
			want := enrichment.ErrMovieNotImportable
			switch mode {
			case "stale":
				expected = "1"
				want = ErrWatchlistChanged
			case "db failure":
				want = ErrWatchlistUnavailable
				if _, err := f.pool.Exec(t.Context(), `ALTER TABLE account_watchlist_items ADD CONSTRAINT fixture_reject_membership CHECK(false)`); err != nil {
					t.Fatal(err)
				}
			case "refresh failure":
				want = ErrWatchlistUnavailable
				f.service.watchlistRefresh = func(context.Context, string) error { return errors.New("refresh unavailable") }
			}
			result, err := f.service.ImportWatchlist(t.Context(), one.Cookie.Token, "failure_owner", expected, "42")
			if !errors.Is(err, want) || result.MovieSlug != "" || result.Watchlist.Items != nil {
				t.Fatalf("unsafe result %+v %v want %v", result, err, want)
			}
			var imports, metadata, movies, items int
			if err = f.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM tmdb_catalog_imports),(SELECT count(*) FROM movie_metadata_cache),(SELECT count(*) FROM public_movies),(SELECT count(*) FROM account_watchlist_items)`).Scan(&imports, &metadata, &movies, &items); err != nil {
				t.Fatal(err)
			}
			wantCount := 0
			if mode == "refresh failure" {
				wantCount = 1
			}
			if imports != wantCount || metadata != wantCount || movies != wantCount || items != wantCount {
				t.Fatalf("atomicity imports=%d metadata=%d movies=%d items=%d", imports, metadata, movies, items)
			}
		})
	}
}

func TestWatchlistImportReauthorizationAndConcurrencyIntegration(t *testing.T) {
	for _, mode := range []string{"logout", "revision", "sort", "tag", "concurrent import"} {
		t.Run(mode, func(t *testing.T) {
			f := newLifecycleFixture(t)
			one := f.complete(t, "race@example.com", "race_owner")
			f.service.watchlistRefresh = func(context.Context, string) error { return nil }
			started := make(chan struct{}, 2)
			resume := make(chan struct{})
			f.service.watchlistProvider = watchlistFakeProvider{details: func(ctx context.Context, id int64) (tmdb.Details, error) {
				started <- struct{}{}
				select {
				case <-resume:
					return watchlistDetails(id), nil
				case <-ctx.Done():
					return tmdb.Details{}, ctx.Err()
				}
			}}
			results := make(chan error, 2)
			go func() {
				_, err := f.service.ImportWatchlist(t.Context(), one.Cookie.Token, "race_owner", "0", "42")
				results <- err
			}()
			<-started
			switch mode {
			case "logout":
				if err := f.service.Logout(t.Context(), one.Cookie.Token, true); err != nil {
					t.Fatal(err)
				}
			case "revision":
				slug := watchlistSeedMovie(t, f, 99, "Local")
				if _, err := f.service.SaveWatchlist(t.Context(), one.Cookie.Token, "race_owner", "0", slug, true); err != nil {
					t.Fatal(err)
				}
			case "sort":
				other, err := f.service.Login(t.Context(), "race@example.com", testPassword, "")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.service.SaveWatchlistSort(t.Context(), other.Cookie.Token, "race_owner", "0", "title_asc"); err != nil {
					t.Fatal(err)
				}
			case "tag":
				if _, err := f.service.CreateWatchlistTag(t.Context(), one.Cookie.Token, "race_owner", "0", "Tag", "neutral"); err != nil {
					t.Fatal(err)
				}
			case "concurrent import":
				two := f.complete(t, "other@example.com", "other_owner")
				go func() {
					_, err := f.service.ImportWatchlist(t.Context(), two.Cookie.Token, "other_owner", "0", "42")
					results <- err
				}()
				<-started
			}
			close(resume)
			err := <-results
			var want error
			if mode == "logout" {
				want = ErrUnauthorized
			}
			if mode == "revision" || mode == "sort" || mode == "tag" {
				want = ErrWatchlistChanged
			}
			if !errors.Is(err, want) {
				t.Fatalf("race %v want %v", err, want)
			}
			var count int
			if mode == "concurrent import" {
				if err = <-results; err != nil {
					t.Fatal(err)
				}
				if err = f.pool.QueryRow(t.Context(), `SELECT count(DISTINCT public_movie_id) FROM account_watchlist_items`).Scan(&count); err != nil || count != 1 {
					t.Fatal("imports did not converge")
				}
			} else if err = f.pool.QueryRow(t.Context(), `SELECT count(*) FROM tmdb_catalog_imports`).Scan(&count); err != nil || count != 0 {
				t.Fatal("revoked import published metadata")
			}
			if mode == "sort" || mode == "tag" {
				if err = f.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM account_watchlist_items)+(SELECT count(*) FROM movie_metadata_cache)+(SELECT count(*) FROM public_movies)`).Scan(&count); err != nil || count != 0 {
					t.Fatal("stale import changed membership or catalog", err)
				}
			}
		})
	}
}

func TestWatchlistSortPersistenceIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "sort@example.com", "sort_owner")
	two := f.complete(t, "other@example.com", "other_owner")
	view, err := f.service.Watchlist(ctx, one.Cookie.Token)
	if err != nil || view.SortOrder != "added_desc" || view.Revision != "0" {
		t.Fatal("missing default", err)
	}
	view, err = f.service.SaveWatchlistSort(ctx, one.Cookie.Token, "sort_owner", "0", "added_desc")
	if err != nil || view.Revision != "0" || view.SortOrder != "added_desc" {
		t.Fatal("default no-op failed", err)
	}
	var count int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM account_watchlist_state`).Scan(&count); err != nil || count != 0 {
		t.Fatal("default initialized state", err)
	}
	if _, err = f.service.SaveWatchlistSort(ctx, two.Cookie.Token, "sort_owner", "0", "title_asc"); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("wrong owner accepted", err)
	}
	for i, order := range []string{"added_asc", "title_asc", "title_desc", "release_desc", "release_asc", "added_desc"} {
		view, err = f.service.SaveWatchlistSort(ctx, one.Cookie.Token, "sort_owner", strconv.Itoa(i), order)
		if err != nil || view.SortOrder != order || view.Revision != strconv.Itoa(i+1) || len(view.Items) != 0 {
			t.Fatalf("sort %s: %+v %v", order, view, err)
		}
		unchanged, err := f.service.SaveWatchlistSort(ctx, one.Cookie.Token, "sort_owner", view.Revision, order)
		if err != nil || !reflect.DeepEqual(unchanged, view) {
			t.Fatal("sort no-op changed snapshot", err)
		}
		if _, err = f.service.SaveWatchlistSort(ctx, one.Cookie.Token, "sort_owner", strconv.Itoa(i), order); !errors.Is(err, ErrWatchlistChanged) {
			t.Fatal("stale sort no-op accepted", err)
		}
	}
	other, err := f.service.Watchlist(ctx, two.Cookie.Token)
	if err != nil || other.SortOrder != "added_desc" || other.Revision != "0" {
		t.Fatal("preference crossed account boundary", err)
	}
	view, err = f.service.SaveWatchlistSort(ctx, one.Cookie.Token, "sort_owner", "6", "title_asc")
	if err != nil {
		t.Fatal(err)
	}
	first := watchlistSeedMovie(t, f, 10, "Alpha")
	second := watchlistSeedMovie(t, f, 20, "Zulu")
	for _, slug := range []string{first, second} {
		f.advance(time.Minute)
		view, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "sort_owner", view.Revision, slug, true)
		if err != nil || view.SortOrder != "title_asc" {
			t.Fatal("membership reset preference", err)
		}
	}
	if view.Items[0].Slug != second {
		t.Fatal("server personalized item ordering")
	}
	items := view.Items
	// Include nonempty release evidence and publication clocks in side-effect checks.
	if _, err = f.pool.Exec(ctx, `INSERT INTO tmdb_french_release_cache(tmdb_id,french_release_date,verified_at,retry_after,attempt_revision) VALUES(10,'1998-10-14',now(),now(),1)`); err != nil {
		t.Fatal(err)
	}
	unchangedState := func() string {
		t.Helper()
		var value string
		if err := f.pool.QueryRow(ctx, `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(i) ORDER BY account_id,public_movie_id) FROM account_watchlist_items i),
 (SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM accounts a),
 (SELECT jsonb_agg(to_jsonb(c) ORDER BY tmdb_id) FROM tmdb_french_release_cache c),
 (SELECT jsonb_agg(to_jsonb(e)) FROM movie_enrichment_state e),
 (SELECT jsonb_agg(to_jsonb(s)) FROM schedule_snapshot s),
 (SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM public_movies p))::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := unchangedState()
	view, err = f.service.SaveWatchlistSort(ctx, one.Cookie.Token, "sort_owner", view.Revision, "release_asc")
	if err != nil || unchangedState() != before || !view.Items[0].AddedAt.Equal(items[0].AddedAt) {
		t.Fatal("sort changed unrelated state", err)
	}
	for _, slug := range []string{first, second, first} {
		view, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "sort_owner", view.Revision, slug, false)
		if err != nil || view.SortOrder != "release_asc" {
			t.Fatal("last removal reset sort", err)
		}
	}
	view, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "sort_owner", view.Revision, first, true)
	if err != nil || view.SortOrder != "release_asc" {
		t.Fatal("re-add reset sort", err)
	}
	f.service.watchlistProvider = watchlistFakeProvider{details: func(_ context.Context, id int64) (tmdb.Details, error) { return watchlistDetails(id), nil }}
	f.service.watchlistRefresh = func(context.Context, string) error { return nil }
	result, err := f.service.ImportWatchlist(ctx, one.Cookie.Token, "sort_owner", view.Revision, "42")
	if err != nil || result.Watchlist.SortOrder != "release_asc" || len(result.Watchlist.Items) != 2 {
		t.Fatal("import reset sort", err)
	}
	if _, err = f.pool.Exec(ctx, `DELETE FROM accounts WHERE email='sort@example.com'`); err != nil {
		t.Fatal(err)
	}
	if err = f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM account_watchlist_state)+(SELECT count(*) FROM account_watchlist_items)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("private state survived deletion", err)
	}
}

func TestWatchlistSortConcurrentCASIntegration(t *testing.T) {
	for _, membership := range []bool{false, true} {
		t.Run(fmt.Sprintf("membership=%t", membership), func(t *testing.T) {
			f := newLifecycleFixture(t)
			ctx := t.Context()
			one := f.complete(t, "race@example.com", "sort_race")
			two, err := f.service.Login(ctx, "race@example.com", testPassword, "")
			if err != nil {
				t.Fatal(err)
			}
			slug := watchlistSeedMovie(t, f, 1, "Film")
			start, results := make(chan struct{}), make(chan error, 2)
			go func() {
				<-start
				_, err := f.service.SaveWatchlistSort(ctx, one.Cookie.Token, "sort_race", "0", "title_asc")
				results <- err
			}()
			go func() {
				<-start
				var err error
				if membership {
					_, err = f.service.SaveWatchlist(ctx, two.Cookie.Token, "sort_race", "0", slug, true)
				} else {
					_, err = f.service.SaveWatchlistSort(ctx, two.Cookie.Token, "sort_race", "0", "title_desc")
				}
				results <- err
			}()
			close(start)
			var successes, conflicts int
			for range 2 {
				err := <-results
				if err == nil {
					successes++
				} else if errors.Is(err, ErrWatchlistChanged) {
					conflicts++
				} else {
					t.Fatal(err)
				}
			}
			view, err := f.service.Watchlist(ctx, one.Cookie.Token)
			if successes != 1 || conflicts != 1 || err != nil || view.Revision != "1" {
				t.Fatal("shared compare-and-swap lost", err)
			}
		})
	}
}

func TestWatchlistSortSharesAccountQuotaIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "quota@example.com", "sort_quota")
	two, err := f.service.Login(ctx, "quota@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	slug := watchlistSeedMovie(t, f, 1, "Film")
	if _, err = f.service.SaveWatchlist(ctx, one.Cookie.Token, "sort_quota", "0", slug, true); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE account_rate_limits SET count=120 WHERE purpose='watchlist_write'`); err != nil {
		t.Fatal(err)
	}
	_, err = f.service.SaveWatchlistSort(ctx, two.Cookie.Token, "sort_quota", "1", "title_asc")
	var rate *RateLimitError
	if !errors.As(err, &rate) || rate.RetryAfter <= 0 {
		t.Fatal("sort bypassed shared account quota", err)
	}
	view, err := f.service.Watchlist(ctx, one.Cookie.Token)
	if err != nil || view.Revision != "1" || view.SortOrder != "added_desc" || len(view.Items) != 1 {
		t.Fatal("rejected sort changed state", err)
	}
}

func TestWatchlistSearchRevocationDuringProviderIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	one := f.complete(t, "revoked@example.com", "revoked_owner")
	started, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.service.watchlistProvider = watchlistFakeProvider{search: func(ctx context.Context, _ string) ([]tmdb.Candidate, error) {
		once.Do(func() { close(started) })
		select {
		case <-resume:
			return []tmdb.Candidate{{ID: 42, Title: "Film"}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	result := make(chan error, 1)
	go func() {
		view, err := f.service.SearchWatchlist(t.Context(), one.Cookie.Token, "revoked_owner", "Film")
		if view.Username != "" {
			result <- errors.New("private response survived logout")
			return
		}
		result <- err
	}()
	<-started
	if err := f.service.Logout(t.Context(), one.Cookie.Token, true); err != nil {
		t.Fatal(err)
	}
	close(resume)
	if err := <-result; !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
}
