package accounts

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"messeances/api/internal/tmdb"
)

func TestWatchlistTagsCRUDIsolationIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "tags@example.com", "tags_owner")
	two := f.complete(t, "other@example.com", "other_owner")
	raw := one.Cookie.Token
	view, err := f.service.Watchlist(ctx, raw)
	if err != nil || view.Tags == nil || len(view.Tags) != 0 || view.Revision != "0" {
		t.Fatal("empty tags", err)
	}
	view, err = f.service.CreateWatchlistTag(ctx, raw, "tags_owner", "0", "  E\u0301te\u0301  ")
	if err != nil || view.Revision != "1" || view.SortOrder != "added_desc" || len(view.Tags) != 1 || view.Tags[0].Name != "Été" || len(view.Items) != 0 {
		t.Fatal("tag-only initialization", err)
	}
	id := view.Tags[0].ID
	for _, name := range []string{"été", "E\u0301TE\u0301"} {
		if _, err := f.service.CreateWatchlistTag(ctx, raw, "tags_owner", "1", name); !errors.Is(err, ErrWatchlistTagNameTaken) {
			t.Fatal("normalized collision", err)
		}
	}
	if _, err := f.service.CreateWatchlistTag(ctx, raw, "tags_owner", "0", "Été"); !errors.Is(err, ErrWatchlistChanged) {
		t.Fatal("collision preceded CAS", err)
	}
	other, err := f.service.CreateWatchlistTag(ctx, two.Cookie.Token, "other_owner", "0", "Été")
	if err != nil || len(other.Tags) != 1 || other.Tags[0].ID == id {
		t.Fatal("same name not owner-private", err)
	}
	for _, target := range []string{other.Tags[0].ID, "9223372036854775807"} {
		if _, err := f.service.DeleteWatchlistTag(ctx, raw, "tags_owner", "1", target); !errors.Is(err, ErrWatchlistTagNotFound) {
			t.Fatal("foreign/absent delete", err)
		}
		if _, err := f.service.RenameWatchlistTag(ctx, raw, "tags_owner", "1", target, "Other"); !errors.Is(err, ErrWatchlistTagNotFound) {
			t.Fatal("foreign/absent rename", err)
		}
		if _, err := f.service.AssignWatchlistTag(ctx, raw, "tags_owner", "1", "film-10", target, true); !errors.Is(err, ErrWatchlistTagNotFound) {
			t.Fatal("foreign/absent assignment", err)
		}
	}
	if _, err := f.service.CreateWatchlistTag(ctx, two.Cookie.Token, "tags_owner", "1", "Other"); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("wrong owner created tag", err)
	}
	unchanged, err := f.service.RenameWatchlistTag(ctx, raw, "tags_owner", "1", id, " Éte\u0301 ")
	if err != nil || !reflect.DeepEqual(unchanged, view) {
		t.Fatal("unchanged rename", err)
	}
	view, err = f.service.RenameWatchlistTag(ctx, raw, "tags_owner", "1", id, "ÉTÉ")
	if err != nil || view.Revision != "2" || view.Tags[0].Name != "ÉTÉ" {
		t.Fatal("case-only rename", err)
	}
	view, err = f.service.CreateWatchlistTag(ctx, raw, "tags_owner", "2", "Ete")
	if err != nil || len(view.Tags) != 2 {
		t.Fatal("accents incorrectly removed", err)
	}
	if _, err := f.service.RenameWatchlistTag(ctx, raw, "tags_owner", view.Revision, id, "ete"); !errors.Is(err, ErrWatchlistTagNameTaken) {
		t.Fatal("rename collision", err)
	}
	slug := watchlistSeedMovie(t, f, 10, "Film")
	if _, err := f.service.AssignWatchlistTag(ctx, raw, "tags_owner", view.Revision, slug, id, true); !errors.Is(err, ErrWatchlistMovieNotSaved) {
		t.Fatal("unsaved movie tagged", err)
	}
	if _, err := f.service.AssignWatchlistTag(ctx, raw, "tags_owner", view.Revision, "film-999", id, false); !errors.Is(err, ErrMovieNotFound) {
		t.Fatal("unknown movie", err)
	}
	view, err = f.service.SaveWatchlistSort(ctx, raw, "tags_owner", view.Revision, "release_asc")
	if err != nil {
		t.Fatal(err)
	}
	view, err = f.service.SaveWatchlist(ctx, raw, "tags_owner", view.Revision, slug, true)
	if err != nil || view.Items[0].TagIDs == nil || len(view.Items[0].TagIDs) != 0 {
		t.Fatal("new movie tags", err)
	}
	added := view.Items[0].AddedAt
	before := watchlistTagUnrelatedState(t, f)
	for _, tag := range view.Tags {
		view, err = f.service.AssignWatchlistTag(ctx, raw, "tags_owner", view.Revision, slug, tag.ID, true)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(view.Items[0].TagIDs) != 2 || view.SortOrder != "release_asc" || !view.Items[0].AddedAt.Equal(added) || before != watchlistTagUnrelatedState(t, f) {
		t.Fatal("assignment changed unrelated state")
	}
	unchanged, err = f.service.AssignWatchlistTag(ctx, raw, "tags_owner", view.Revision, slug, id, true)
	if err != nil || !reflect.DeepEqual(view, unchanged) {
		t.Fatal("assignment no-op", err)
	}
	if _, err := f.service.AssignWatchlistTag(ctx, raw, "tags_owner", "0", slug, id, true); !errors.Is(err, ErrWatchlistChanged) {
		t.Fatal("stale no-op", err)
	}
	view, err = f.service.DeleteWatchlistTag(ctx, raw, "tags_owner", view.Revision, id)
	if err != nil || len(view.Items) != 1 || len(view.Items[0].TagIDs) != 1 || len(view.Tags) != 1 {
		t.Fatal("delete removed film", err)
	}
	view, err = f.service.SaveWatchlist(ctx, raw, "tags_owner", view.Revision, slug, false)
	if err != nil || len(view.Tags) != 1 {
		t.Fatal("remove erased reusable tag", err)
	}
	view, err = f.service.SaveWatchlist(ctx, raw, "tags_owner", view.Revision, slug, true)
	if err != nil || len(view.Items[0].TagIDs) != 0 {
		t.Fatal("re-add retained tags", err)
	}
	view, err = f.service.DeleteWatchlistTag(ctx, raw, "tags_owner", view.Revision, view.Tags[0].ID)
	if err != nil || view.Tags == nil || len(view.Tags) != 0 || view.SortOrder != "release_asc" {
		t.Fatal("last tag reset state", err)
	}
	login, err := f.service.Login(ctx, "tags@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := f.service.Watchlist(ctx, login.Cookie.Token)
	if err != nil || !reflect.DeepEqual(view, persisted) {
		t.Fatal("other session lost snapshot", err)
	}
}

func watchlistTagUnrelatedState(t *testing.T, f *lifecycleFixture) string {
	t.Helper()
	var state string
	err := f.pool.QueryRow(t.Context(), `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(i) ORDER BY account_id,public_movie_id) FROM account_watchlist_items i),
 (SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM accounts a),
 (SELECT jsonb_agg(to_jsonb(c) ORDER BY tmdb_id) FROM tmdb_french_release_cache c),
 (SELECT jsonb_agg(to_jsonb(e)) FROM movie_enrichment_state e),
 (SELECT jsonb_agg(to_jsonb(s)) FROM schedule_snapshot s),
 (SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM public_movies p))::text`).Scan(&state)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestWatchlistTagsCanonicalUnionSplitIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "tags@example.com", "tags_owner")
	raw := one.Cookie.Token
	view, err := f.service.Watchlist(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{20, 10, 30} {
		slug := watchlistSeedMovie(t, f, id, "Film")
		view, err = f.service.SaveWatchlist(ctx, raw, "tags_owner", view.Revision, slug, true)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"Action", "Drame", "Nouveau"} {
		view, err = f.service.CreateWatchlistTag(ctx, raw, "tags_owner", view.Revision, name)
		if err != nil {
			t.Fatal(err)
		}
	}
	a, b, c := view.Tags[0].ID, view.Tags[1].ID, view.Tags[2].ID
	for _, pair := range [][2]string{{"film-10", a}, {"film-20", a}, {"film-30", b}} {
		view, err = f.service.AssignWatchlistTag(ctx, raw, "tags_owner", view.Revision, pair[0], pair[1], true)
		if err != nil {
			t.Fatal(err)
		}
	}
	beforeRevision := view.Revision
	releaseSQL(t, f, `UPDATE public_movies SET redirect_to_id=10,confirmed_tmdb_id=NULL WHERE id=20`)
	releaseSQL(t, f, `UPDATE public_movies SET redirect_to_id=20,confirmed_tmdb_id=NULL WHERE id=30`)
	releaseSQL(t, f, `INSERT INTO movie_slug_aliases(slug,public_movie_id,alias_kind) VALUES('tmdb-film-30',30,'tmdb')`)
	view, err = f.service.Watchlist(ctx, raw)
	if err != nil || len(view.Items) != 1 || !reflect.DeepEqual(view.Items[0].TagIDs, []string{a, b}) || view.Revision != beforeRevision {
		t.Fatal("merge union", err)
	}
	view, err = f.service.AssignWatchlistTag(ctx, raw, "tags_owner", view.Revision, "tmdb-film-30", a, true)
	if err != nil || view.Revision != beforeRevision {
		t.Fatal("union no-op", err)
	}
	view, err = f.service.AssignWatchlistTag(ctx, raw, "tags_owner", view.Revision, "tmdb-film-30", c, true)
	if err != nil {
		t.Fatal(err)
	}
	var storedID int64
	if err = f.pool.QueryRow(ctx, `SELECT public_movie_id FROM account_watchlist_item_tags WHERE tag_id=$1`, c).Scan(&storedID); err != nil || storedID != 10 {
		t.Fatal("numeric earliest-row tie break", err)
	}
	view, err = f.service.AssignWatchlistTag(ctx, raw, "tags_owner", view.Revision, "film-10", c, false)
	if err != nil {
		t.Fatal(err)
	}
	releaseSQL(t, f, `UPDATE account_watchlist_items SET added_at=added_at-interval '1 day' WHERE public_movie_id=20`)
	view, err = f.service.AssignWatchlistTag(ctx, raw, "tags_owner", view.Revision, "film-10", c, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.pool.QueryRow(ctx, `SELECT public_movie_id FROM account_watchlist_item_tags WHERE tag_id=$1`, c).Scan(&storedID); err != nil || storedID != 20 {
		t.Fatal("earliest-row assignment", err)
	}
	view, err = f.service.AssignWatchlistTag(ctx, raw, "tags_owner", view.Revision, "film-30", a, false)
	if err != nil || !reflect.DeepEqual(view.Items[0].TagIDs, []string{b, c}) {
		t.Fatal("whole-union removal", err)
	}
	releaseSQL(t, f, `UPDATE public_movies SET redirect_to_id=NULL,confirmed_tmdb_id=id WHERE id IN (20,30)`)
	view, err = f.service.Watchlist(ctx, raw)
	if err != nil || len(view.Items) != 3 {
		t.Fatal("split", err)
	}
	for _, item := range view.Items {
		want := map[string][]string{"film-10": {}, "film-20": {c}, "film-30": {b}}[item.Slug]
		if !reflect.DeepEqual(item.TagIDs, want) {
			t.Fatalf("split fanned tags: %+v", item)
		}
	}
	releaseSQL(t, f, `UPDATE public_movies SET redirect_to_id=10,confirmed_tmdb_id=NULL WHERE id IN (20,30)`)
	view, err = f.service.SaveWatchlist(ctx, raw, "tags_owner", view.Revision, "film-20", false)
	if err != nil || len(view.Items) != 0 || len(view.Tags) != 3 {
		t.Fatal("merged membership removal", err)
	}
	var count int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM account_watchlist_item_tags`).Scan(&count); err != nil || count != 0 {
		t.Fatal("associations left behind", err)
	}
	view, err = f.service.SaveWatchlist(ctx, raw, "tags_owner", view.Revision, "film-10", true)
	if err != nil {
		t.Fatal(err)
	}
	releaseSQL(t, f, `UPDATE public_movies SET redirect_to_id=20,confirmed_tmdb_id=NULL WHERE id=10`)
	view, err = f.service.Watchlist(ctx, raw)
	if err != nil || len(view.Items) != 0 || len(view.Tags) != 3 {
		t.Fatal("redirect cycle did not terminate", err)
	}
}

func TestWatchlistTagsCapacityRevisionAndQuotaIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "tags@example.com", "tags_owner")
	raw := one.Cookie.Token
	view := WatchlistView{Revision: "0"}
	var err error
	for i := 0; i < maxWatchlistTags; i++ {
		view, err = f.service.CreateWatchlistTag(ctx, raw, "tags_owner", view.Revision, fmt.Sprintf("Tag %d", i))
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, tag := range view.Tags {
		if tag.ID != strconv.Itoa(i+1) {
			t.Fatal("tags not numerically ordered", view.Tags)
		}
	}
	if _, err = f.service.CreateWatchlistTag(ctx, raw, "tags_owner", view.Revision, "Overflow"); !errors.Is(err, ErrWatchlistTagLimit) {
		t.Fatal("tag cap", err)
	}
	view, err = f.service.DeleteWatchlistTag(ctx, raw, "tags_owner", view.Revision, view.Tags[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	view, err = f.service.CreateWatchlistTag(ctx, raw, "tags_owner", view.Revision, "Available")
	if err != nil || len(view.Tags) != maxWatchlistTags {
		t.Fatal("capacity not recovered", err)
	}
	releaseSQL(t, f, `UPDATE account_watchlist_state SET revision=9007199254740991`)
	max := strconv.FormatInt(maxTheaterPreferenceRevision, 10)
	view, err = f.service.RenameWatchlistTag(ctx, raw, "tags_owner", max, view.Tags[0].ID, view.Tags[0].Name)
	if err != nil || view.Revision != max {
		t.Fatal("max no-op", err)
	}
	if _, err = f.service.RenameWatchlistTag(ctx, raw, "tags_owner", max, view.Tags[0].ID, "Changed"); !errors.Is(err, ErrWatchlistUnavailable) {
		t.Fatal("max changed", err)
	}
	if _, err = f.service.DeleteWatchlistTag(ctx, raw, "tags_owner", max, view.Tags[0].ID); !errors.Is(err, ErrWatchlistUnavailable) {
		t.Fatal("max delete", err)
	}
	persisted, err := f.service.Watchlist(ctx, raw)
	if err != nil || !reflect.DeepEqual(persisted, view) {
		t.Fatal("max revision failed rollback", err)
	}
	releaseSQL(t, f, `UPDATE account_rate_limits SET count=120 WHERE purpose='watchlist_write'`)
	for _, operation := range []func() error{
		func() error { _, err := f.service.CreateWatchlistTag(ctx, raw, "tags_owner", max, "New"); return err },
		func() error {
			_, err := f.service.RenameWatchlistTag(ctx, raw, "tags_owner", max, view.Tags[0].ID, "New")
			return err
		},
		func() error {
			_, err := f.service.DeleteWatchlistTag(ctx, raw, "tags_owner", max, view.Tags[0].ID)
			return err
		},
		func() error {
			_, err := f.service.AssignWatchlistTag(ctx, raw, "tags_owner", max, "film-1", view.Tags[0].ID, true)
			return err
		},
		func() error {
			_, err := f.service.SaveWatchlistSort(ctx, raw, "tags_owner", max, "title_asc")
			return err
		},
		func() error {
			_, err := f.service.SaveWatchlist(ctx, raw, "tags_owner", max, "film-1", true)
			return err
		},
	} {
		var rate *RateLimitError
		if err := operation(); !errors.As(err, &rate) {
			t.Fatal("write quota not shared", err)
		}
	}
}

func TestWatchlistTagsConcurrentCASIntegration(t *testing.T) {
	for _, mode := range []string{"tag", "sort", "membership", "assignment/delete"} {
		t.Run(mode, func(t *testing.T) {
			f := newLifecycleFixture(t)
			ctx := t.Context()
			one := f.complete(t, "tags@example.com", "tags_owner")
			raw := one.Cookie.Token
			slug := watchlistSeedMovie(t, f, 1, "Film")
			view, err := f.service.CreateWatchlistTag(ctx, raw, "tags_owner", "0", "Action")
			if err != nil {
				t.Fatal(err)
			}
			id := view.Tags[0].ID
			view, err = f.service.SaveWatchlist(ctx, raw, "tags_owner", view.Revision, slug, true)
			if err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			go func() {
				<-start
				var err error
				if mode == "assignment/delete" {
					_, err = f.service.AssignWatchlistTag(ctx, raw, "tags_owner", view.Revision, slug, id, true)
				} else {
					_, err = f.service.RenameWatchlistTag(ctx, raw, "tags_owner", view.Revision, id, "New")
				}
				results <- err
			}()
			go func() {
				<-start
				var err error
				switch mode {
				case "tag":
					_, err = f.service.CreateWatchlistTag(ctx, raw, "tags_owner", view.Revision, "Second")
				case "sort":
					_, err = f.service.SaveWatchlistSort(ctx, raw, "tags_owner", view.Revision, "title_asc")
				case "membership":
					_, err = f.service.SaveWatchlist(ctx, raw, "tags_owner", view.Revision, slug, false)
				case "assignment/delete":
					_, err = f.service.DeleteWatchlistTag(ctx, raw, "tags_owner", view.Revision, id)
				}
				results <- err
			}()
			close(start)
			wins, conflicts := 0, 0
			for range 2 {
				err := <-results
				if err == nil {
					wins++
				} else if errors.Is(err, ErrWatchlistChanged) {
					conflicts++
				} else {
					t.Fatal(err)
				}
			}
			if wins != 1 || conflicts != 1 {
				t.Fatal("shared CAS lost")
			}
		})
	}
}

func TestWatchlistTagsFullSnapshotBoundsIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "tags@example.com", "tags_owner")
	raw := one.Cookie.Token
	releaseSQL(t, f, `INSERT INTO public_movies(id,identity_anchor_tmdb_id,title,runtime_minutes) OVERRIDING SYSTEM VALUE SELECT n,n,'Film',0 FROM generate_series(1,1000)n`)
	releaseSQL(t, f, `INSERT INTO account_watchlist_items(account_id,public_movie_id) SELECT a.id,p.id FROM accounts a CROSS JOIN public_movies p`)
	releaseSQL(t, f, `INSERT INTO account_watchlist_tags(account_id,id,name,name_key) OVERRIDING SYSTEM VALUE SELECT a.id,n,'Tag '||n,'tag '||n FROM accounts a CROSS JOIN generate_series(1,49)n`)
	releaseSQL(t, f, `INSERT INTO account_watchlist_tags(account_id,id,name,name_key) OVERRIDING SYSTEM VALUE SELECT id,9223372036854775807,'Max','max' FROM accounts`)
	releaseSQL(t, f, `INSERT INTO account_watchlist_item_tags SELECT i.account_id,i.public_movie_id,t.id FROM account_watchlist_items i JOIN account_watchlist_tags t USING(account_id)`)
	releaseSQL(t, f, `INSERT INTO account_watchlist_state(account_id,revision,sort_order) SELECT id,9007199254740991,'title_asc' FROM accounts`)
	view, err := f.service.Watchlist(ctx, raw)
	if err != nil || len(view.Items) != 1000 || len(view.Tags) != 50 {
		t.Fatal("truncated snapshot", err)
	}
	want := make([]string, 50)
	for i := range 49 {
		want[i] = strconv.Itoa(i + 1)
	}
	want[49] = "9223372036854775807"
	for i, tag := range view.Tags {
		if tag.ID != want[i] {
			t.Fatal("tag precision or numeric order", tag)
		}
	}
	for _, item := range view.Items {
		if !reflect.DeepEqual(item.TagIDs, want) {
			t.Fatal("association precision/order/bounds", item.TagIDs)
		}
	}
	unchanged, err := f.service.AssignWatchlistTag(ctx, raw, "tags_owner", view.Revision, "film-1", want[49], true)
	if err != nil || !reflect.DeepEqual(unchanged, view) {
		t.Fatal("max revision assignment no-op", err)
	}
	if _, err := f.service.AssignWatchlistTag(ctx, raw, "tags_owner", view.Revision, "film-1", want[49], false); !errors.Is(err, ErrWatchlistUnavailable) {
		t.Fatal("max revision unassignment", err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM account_watchlist_item_tags`).Scan(&count); err != nil || count != 50000 {
		t.Fatal("unassignment did not roll back", err)
	}
	releaseSQL(t, f, `UPDATE account_watchlist_state SET revision=7`)
	view, err = f.service.DeleteWatchlistTag(ctx, raw, "tags_owner", "7", want[49])
	if err != nil || view.Revision != "8" || view.SortOrder != "title_asc" || len(view.Items) != 1000 || len(view.Tags) != 49 {
		t.Fatal("bulk association delete revision", err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM account_watchlist_item_tags`).Scan(&count); err != nil || count != 49000 {
		t.Fatal("tag delete partial", err)
	}
}

func TestWatchlistTagsImportSnapshotIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "tags@example.com", "tags_owner")
	raw := one.Cookie.Token
	f.service.watchlistProvider = watchlistFakeProvider{details: func(_ context.Context, id int64) (tmdb.Details, error) { return watchlistDetails(id), nil }}
	f.service.watchlistRefresh = func(context.Context, string) error { return nil }
	view, err := f.service.CreateWatchlistTag(ctx, raw, "tags_owner", "0", "Tag")
	if err != nil {
		t.Fatal(err)
	}
	slug := watchlistSeedMovie(t, f, 10, "Local")
	view, err = f.service.SaveWatchlist(ctx, raw, "tags_owner", view.Revision, slug, true)
	if err != nil {
		t.Fatal(err)
	}
	view, err = f.service.AssignWatchlistTag(ctx, raw, "tags_owner", view.Revision, slug, view.Tags[0].ID, true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.service.ImportWatchlist(ctx, raw, "tags_owner", view.Revision, "42")
	if err != nil || !reflect.DeepEqual(result.Watchlist.Tags, view.Tags) || len(result.Watchlist.Items) != 2 {
		t.Fatal("import dropped definitions", err)
	}
	for _, item := range result.Watchlist.Items {
		if item.Slug == slug {
			if !reflect.DeepEqual(item.TagIDs, view.Items[0].TagIDs) {
				t.Fatal("import changed assignments")
			}
		} else if item.TagIDs == nil || len(item.TagIDs) != 0 {
			t.Fatal("import auto-assigned tag")
		}
	}
}
