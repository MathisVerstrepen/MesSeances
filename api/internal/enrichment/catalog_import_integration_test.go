package enrichment

import (
	"slices"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/schedulepg"
	"messeances/api/internal/tmdb"
)

func TestCatalogImportReconciliationAndRefreshIntegration(t *testing.T) {
	for _, sourceFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "import first", true: "source first"}[sourceFirst], func(t *testing.T) {
			pool := upcomingIntegrationPool(t)
			ctx := t.Context()
			store := NewPostgresStore(pool)
			reader := schedulepg.NewStore(pool)
			now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
			adult := false
			details := tmdb.Details{ID: 42, Adult: &adult, Title: "Imported title", OriginalTitle: "Original", ReleaseDate: "2000-01-01", Runtime: 90, Genres: []string{"Drame"}, TrailerVFYouTubeKey: "abcdefghijk"}
			metadata := metadataFromDetails(details, 0, now)
			var beforeID int64
			if sourceFirst {
				if _, err := reader.Replace(ctx, []schedule.Dataset{upcomingProviderDataset(now)}); err != nil {
					t.Fatal(err)
				}
				if err := store.Publish(ctx, upcomingMatch(now, 42), metadata); err != nil {
					t.Fatal(err)
				}
				if err := pool.QueryRow(ctx, `SELECT id FROM public_movies WHERE confirmed_tmdb_id=42 AND redirect_to_id IS NULL`).Scan(&beforeID); err != nil {
					t.Fatal(err)
				}
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			id, err := ImportCatalogMovie(ctx, tx, details, now)
			if err != nil {
				_ = tx.Rollback(ctx)
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if sourceFirst && id != beforeID {
				t.Fatal("import replaced existing canonical movie")
			}
			if _, err = pool.Exec(ctx, `INSERT INTO public_movie_metadata_overrides(public_movie_id,title_overridden,title) VALUES($1,true,'Manual title')`, id); err != nil {
				t.Fatal(err)
			}
			if !sourceFirst {
				if _, err = reader.Replace(ctx, []schedule.Dataset{upcomingProviderDataset(now)}); err != nil {
					t.Fatal(err)
				}
				if err = store.Publish(ctx, upcomingMatch(now, 42), metadata); err != nil {
					t.Fatal(err)
				}
			}
			data, _, err := reader.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if publicSourceID(data, schedule.ProviderUGC, "10") != id {
				t.Fatal("provider match duplicated imported identity")
			}
			items, err := store.PendingMatches(ctx, PendingMatchFilterMatched, "", 10, 0)
			if err != nil || len(items) != 1 {
				t.Fatal("expected matched source")
			}
			provider := &matchedCorrectionProvider{details: tmdb.Details{ID: 99, Title: "Correction", OriginalTitle: "Correction", Runtime: 91}}
			if err = NewReviewService(store, provider, func() time.Time { return now.Add(time.Hour) }).Correct(ctx, SourceUGC, "10", 99, *items[0].UpdatedAt); err != nil {
				t.Fatal(err)
			}
			data, _, err = reader.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var retained int64
			if err = pool.QueryRow(ctx, `SELECT public_movie_id FROM tmdb_catalog_imports WHERE tmdb_id=42`).Scan(&retained); err != nil {
				t.Fatal(err)
			}
			old, corrected := publicMovieByID(data, retained), publicMovieByID(data, publicSourceID(data, schedule.ProviderUGC, "10"))
			if old.TMDBID != 42 || old.RedirectToID != 0 || old.Title != "Manual title" || corrected.TMDBID != 99 || retained == corrected.ID || old.HasUpcomingRelease || corrected.HasUpcomingRelease {
				t.Fatalf("lost catalog evidence old=%+v corrected=%+v", old, corrected)
			}
			if !sourceFirst && retained != id {
				t.Fatal("TMDB anchor moved on correction")
			}
			ids, err := store.MatchedTMDBIDs(ctx)
			if err != nil || !slices.Equal(ids, []int64{42, 99}) {
				t.Fatalf("refresh enumeration %v %v", ids, err)
			}
			metadata.LocalizedTitle = "Refreshed base"
			if err = store.RefreshMetadata(ctx, []Metadata{metadata}); err != nil {
				t.Fatal(err)
			}
			data, _, err = reader.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if publicMovieByID(data, retained).Title != "Manual title" {
				t.Fatal("metadata refresh overwrote override")
			}
			// Later French release evidence coexists without creating another identity.
			if err = store.PublishUpcoming(ctx, UpcomingPublication{CompletedAt: now, Window: schedule.UpcomingWindow(now), Metadata: []Metadata{metadata}, Releases: []UpcomingRelease{upcomingTestRelease(42, "2026-10-07", true)}}); err != nil {
				t.Fatal(err)
			}
			var same bool
			if err = pool.QueryRow(ctx, `SELECT u.public_movie_id=i.public_movie_id FROM tmdb_upcoming_movies u JOIN tmdb_catalog_imports i USING(tmdb_id) WHERE tmdb_id=42`).Scan(&same); err != nil || !same {
				t.Fatal("public evidence owners diverged")
			}
		})
	}
}
