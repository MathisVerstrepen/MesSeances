package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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
		if _, err := f.service.SaveWatchlistPreferences(ctx, raw, username, "0", "tags", ""); !errors.Is(err, want) {
			t.Fatalf("preferences %v want %v", err, want)
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
		if q != "Film" && q != "%_" && q != "literal movie" {
			t.Error("query not trimmed")
		}
		return []tmdb.Candidate{{ID: 99, Title: "Known outside local page"}, {ID: 100, Title: "Film 02"}, {ID: 100, Title: "Duplicate"}}, nil
	}}
	view, err := f.service.SearchWatchlist(t.Context(), one.Cookie.Token, "search_owner", "  Film  ")
	if err != nil || len(view.Catalog) != 20 || !view.CatalogHasMore || len(view.External) != 1 || view.External[0].TMDBID != "100" || view.ExternalStatus != "ready" {
		t.Fatalf("search %+v %v", view, err)
	}
	view, err = f.service.SearchWatchlist(t.Context(), one.Cookie.Token, "search_owner", "%_")
	if err != nil || len(view.Catalog) != 0 || view.CatalogHasMore {
		t.Fatal("punctuation-only query exposed catalog")
	}
	view, err = f.service.SearchWatchlist(t.Context(), one.Cookie.Token, "search_owner", "  literal movie  ")
	if err != nil || len(view.Catalog) != 1 || view.Catalog[0].Slug != "film-1" || view.Catalog[0].Title != "Literal %_ movie" || view.CatalogHasMore {
		t.Fatal("normalized override search failed")
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

func watchlistSeedOriginalTitle(t *testing.T, f *lifecycleFixture, id int64, title string) {
	t.Helper()
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO movie_metadata_cache(provider,provider_movie_id,locale,provider_title,localized_title,runtime_minutes,fetched_at,refresh_after)
 VALUES('tmdb',$1,'fr-FR',$2,'Localized',100,now(),now())
 ON CONFLICT(provider,provider_movie_id,locale) DO UPDATE SET provider_title=EXCLUDED.provider_title`, id, title); err != nil {
		t.Fatal(err)
	}
}

func TestWatchlistSearchSharedTitleCorpusIntegration(t *testing.T) {
	fixture, err := os.ReadFile("../../../web/tests/fixtures/movie-title-search.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name          string  `json:"name"`
		Query         string  `json:"query"`
		Title         string  `json:"title"`
		OriginalTitle *string `json:"original_title"`
		Expected      bool    `json:"expected"`
	}
	if err := json.Unmarshal(fixture, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty corpus")
	}
	f := newLifecycleFixture(t)
	one := f.complete(t, "corpus@example.com", "corpus_owner")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			// Keep each single-movie expectation isolated without copying the corpus.
			if _, err := f.pool.Exec(t.Context(), `DELETE FROM public_movies; DELETE FROM movie_metadata_cache`); err != nil {
				t.Fatal(err)
			}
			slug := watchlistSeedMovie(t, f, 42, tc.Title)
			if tc.OriginalTitle != nil {
				watchlistSeedOriginalTitle(t, f, 42, *tc.OriginalTitle)
			}
			called := false
			f.service.watchlistProvider = watchlistFakeProvider{search: func(_ context.Context, query string) ([]tmdb.Candidate, error) {
				called = true
				if query != strings.TrimSpace(tc.Query) {
					t.Fatal("provider query rewritten")
				}
				return []tmdb.Candidate{{ID: 999, Title: "External unchanged"}}, nil
			}}
			view, err := f.service.SearchWatchlist(t.Context(), one.Cookie.Token, "corpus_owner", tc.Query)
			if _, validationErr := watchlistQuery(tc.Query); validationErr != nil {
				if !errors.Is(err, ErrInvalidInput) || called || view.Username != "" {
					t.Fatal("invalid raw query accepted", err)
				}
				return
			}
			want := 0
			if tc.Expected {
				want = 1
			}
			if err != nil || !called || len(view.Catalog) != want || view.CatalogHasMore || len(view.External) != 1 || view.External[0].TMDBID != "999" || view.ExternalStatus != "ready" {
				t.Fatalf("view=%+v err=%v want=%d", view, err, want)
			}
			if want > 0 && (view.Catalog[0].Slug != slug || view.Catalog[0].Title != tc.Title) {
				t.Fatal("display or identity changed")
			}
		})
		// Keep the same account below its real per-minute quota as corpus grows.
		f.advance(time.Minute)
	}
}

func TestWatchlistSearchConfirmedOriginalAndOverridesIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	one := f.complete(t, "original@example.com", "original_owner")
	slug := watchlistSeedMovie(t, f, 42, "Base-Title")
	if _, err := f.pool.Exec(t.Context(), `UPDATE public_movies SET poster_url='https://example.com/base.jpg',release_date='2000-01-01' WHERE id=42;
 INSERT INTO public_movie_metadata_overrides(public_movie_id,title_overridden,title,poster_url_overridden,poster_url,release_date_overridden,release_date)
 VALUES(42,true,'Local-Été',true,'https://example.com/override.jpg',true,'2001-02-03')`); err != nil {
		t.Fatal(err)
	}
	assertSearch := func(query string, want bool) {
		t.Helper()
		view, err := f.service.SearchWatchlist(t.Context(), one.Cookie.Token, "original_owner", query)
		count := 0
		if want {
			count = 1
		}
		if err != nil || len(view.Catalog) != count || view.CatalogHasMore || view.ExternalStatus != "disabled" {
			t.Fatalf("query=%q view=%+v err=%v", query, view, err)
		}
		if want && view.Catalog[0] != (WatchlistMovie{Slug: slug, Title: "Local-Été", PosterURL: "https://example.com/override.jpg", ReleaseDate: "2001-02-03"}) {
			t.Fatal("summary overrides changed")
		}
	}
	assertSearch("ete local", true)
	assertSearch("base title", false)
	assertSearch("the invite", false) // Missing cache cannot supply an original title.
	watchlistSeedOriginalTitle(t, f, 42, "  The Invite  ")
	assertSearch("invité the", true)
	assertSearch("local invite", false)
	watchlistSeedOriginalTitle(t, f, 42, "The Héros Returned")
	assertSearch("returned heros", true) // Reads current durable cache, not a snapshot.
	assertSearch("the invite", false)
	watchlistSeedOriginalTitle(t, f, 42, " \t\n")
	assertSearch("heros", false)
	assertSearch("ete local", true)
	watchlistSeedOriginalTitle(t, f, 42, "The Invite")
	if _, err := f.pool.Exec(t.Context(), `UPDATE public_movies SET confirmed_tmdb_id=NULL WHERE id=42`); err != nil {
		t.Fatal(err)
	}
	assertSearch("the invite", false) // Identity anchor alone must not attach cache.
	watchlistSeedOriginalTitle(t, f, 43, "Replacement Original")
	if _, err := f.pool.Exec(t.Context(), `UPDATE public_movies SET confirmed_tmdb_id=43 WHERE id=42`); err != nil {
		t.Fatal(err)
	}
	assertSearch("the invite", false) // Old anchored cache is stale after rematching.
	assertSearch("original replacement", true)
}

func TestWatchlistSearchOrderedPostMatchLimitIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	one := f.complete(t, "ordered@example.com", "ordered_owner")
	// More than 21 nonmatches sort before all matches. No pre-match cap is valid.
	if _, err := f.pool.Exec(t.Context(), `INSERT INTO public_movies(id,identity_anchor_tmdb_id,confirmed_tmdb_id,title,runtime_minutes) OVERRIDING SYSTEM VALUE
 SELECT n,n,n,'Alpha nonmatch ' || n,0 FROM generate_series(1,30)n`); err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{3, 20, 21, 22} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			if _, err := f.pool.Exec(t.Context(), `DELETE FROM public_movies WHERE id>=1000`); err != nil {
				t.Fatal(err)
			}
			for i := count; i >= 1; i-- {
				watchlistSeedMovie(t, f, int64(1000+i), fmt.Sprintf("Zulu Étoile %02d", i))
			}
			watchlistSeedMovie(t, f, 2000, "Zulu Étoile 00")
			if _, err := f.pool.Exec(t.Context(), `UPDATE public_movies SET redirect_to_id=1001,confirmed_tmdb_id=NULL WHERE id=2000`); err != nil {
				t.Fatal(err)
			}
			f.service.watchlistProvider = watchlistFakeProvider{search: func(_ context.Context, q string) ([]tmdb.Candidate, error) {
				if q != "etoile zulu" {
					t.Fatal("provider query normalized")
				}
				return []tmdb.Candidate{{ID: 30, Title: "Known nonmatching"}, {ID: int64(1000 + count), Title: "Known outside page"}, {ID: 3000, Title: "External"}, {ID: 3000, Title: "Duplicate"}, {ID: 0}, {ID: -1}}, nil
			}}
			view, err := f.service.SearchWatchlist(t.Context(), one.Cookie.Token, "ordered_owner", "  etoile zulu  ")
			if err != nil || len(view.Catalog) != min(count, 20) || view.CatalogHasMore != (count > 20) || len(view.External) != 1 || view.External[0].TMDBID != "3000" {
				t.Fatalf("count=%d view=%+v err=%v", count, view, err)
			}
			for i, movie := range view.Catalog {
				if movie.Slug != "film-"+strconv.Itoa(1001+i) || movie.Title != fmt.Sprintf("Zulu Étoile %02d", i+1) {
					t.Fatal("SQL order or redirect omission changed")
				}
			}
		})
	}
}

func TestWatchlistSearchDurableImportOutsideSnapshotIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	one := f.complete(t, "durable@example.com", "durable_owner")
	public := watchlistPublicService(t, f)
	var id int64
	err := f.service.store.withTransaction(t.Context(), func(tx pgx.Tx) error {
		var err error
		id, err = enrichment.ImportCatalogMovie(t.Context(), tx, watchlistDetails(42), f.now())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Import commits without refreshing this live schedule replica.
	if public.HasCatalog() || public.HasSnapshot() {
		t.Fatal("fixture snapshot already contains durable import")
	}
	view, err := f.service.SearchWatchlist(t.Context(), one.Cookie.Token, "durable_owner", "film imported")
	if err != nil || len(view.Catalog) != 1 || view.Catalog[0].Slug != "film-"+strconv.FormatInt(id, 10) || view.CatalogHasMore {
		t.Fatalf("view=%+v err=%v", view, err)
	}
}

func TestWatchlistSearchCancellationReturnsNoPartialCatalogIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	one := f.complete(t, "cancel@example.com", "cancel_owner")
	watchlistSeedMovie(t, f, 42, "Matching Film")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	f.service.watchlistProvider = watchlistFakeProvider{search: func(providerCtx context.Context, _ string) ([]tmdb.Candidate, error) {
		close(started)
		<-providerCtx.Done()
		return nil, providerCtx.Err()
	}}
	type outcome struct {
		view WatchlistSearchView
		err  error
	}
	result := make(chan outcome, 1)
	go func() {
		view, err := f.service.SearchWatchlist(ctx, one.Cookie.Token, "cancel_owner", "matching film")
		result <- outcome{view: view, err: err}
	}()
	<-started
	cancel()
	got := <-result
	if !errors.Is(got.err, ErrWatchlistUnavailable) || got.view.Username != "" || got.view.Catalog != nil || got.view.External != nil {
		t.Fatalf("partial response survived cancellation: %+v %v", got.view, got.err)
	}
}

type watchlistCatalogQueryKey struct{}

type watchlistCatalogCancelTracer struct {
	cancel context.CancelFunc
	ended  chan struct{}
}

func (tracer watchlistCatalogCancelTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "COALESCE(tmdb.provider_title,'')") {
		return context.WithValue(ctx, watchlistCatalogQueryKey{}, true)
	}
	return ctx
}

func (tracer watchlistCatalogCancelTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if ctx.Value(watchlistCatalogQueryKey{}) == true {
		// pgx signals query completion when rows close. Cancel after the local
		// matches have been accumulated, before any partial view can be returned.
		tracer.cancel()
		close(tracer.ended)
	}
}

func TestWatchlistSearchCatalogCancellationReturnsNoPartialResultsIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	one := f.complete(t, "catalog-cancel@example.com", "catalog_cancel")
	for id := int64(1); id <= 25; id++ {
		watchlistSeedMovie(t, f, id, "Matching Film")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ended := make(chan struct{})
	config := f.pool.Config()
	config.ConnConfig.Tracer = watchlistCatalogCancelTracer{cancel: cancel, ended: ended}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f.service.store = NewPostgresStore(pool)
	view, err := f.service.SearchWatchlist(ctx, one.Cookie.Token, "catalog_cancel", "matching film")
	select {
	case <-ended:
	default:
		t.Fatal("catalog cancellation boundary not reached")
	}
	if !errors.Is(err, ErrWatchlistUnavailable) || view.Username != "" || view.Catalog != nil || view.External != nil {
		t.Fatalf("partial catalog survived cancellation: %+v %v", view, err)
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
