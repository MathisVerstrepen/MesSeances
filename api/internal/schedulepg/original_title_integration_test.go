package schedulepg

import (
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
)

func TestOriginalTitleLoadIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	data := testDataset()
	historyPublish(t, store, data)
	now := data.GeneratedAt
	match := enrichment.Match{SourceProvider: enrichment.SourceUGC, SourceMovieID: "200", MetadataProvider: enrichment.ProviderTMDB, Status: enrichment.StatusMatched, MetadataMovieID: 42, Score: 1, NormalizedSourceTitle: "film a", SourceRuntimeMinutes: 100, Candidates: []enrichment.Candidate{{ID: 42, Title: "L'Invitation", Runtime: 100, Score: 1}}, EvaluatedAt: now, RetryAfter: now.Add(30 * 24 * time.Hour)}
	metadata := enrichment.Metadata{Provider: enrichment.ProviderTMDB, ProviderMovieID: 42, Locale: enrichment.LocaleFrench, ProviderTitle: "The Invite", LocalizedTitle: "L'Invitation", RuntimeMinutes: 100, Genres: []string{}, FetchedAt: now, RefreshAfter: now.Add(30 * 24 * time.Hour)}
	if err := enrichment.NewPostgresStore(pool).Publish(t.Context(), match, metadata); err != nil {
		t.Fatal(err)
	}
	// The original title remains cache-backed while the primary title keeps override precedence.
	historyExec(t, pool, `INSERT INTO public_movie_metadata_overrides (public_movie_id,title,title_overridden) SELECT id,'L''Invitation éditée',true FROM public_movies WHERE confirmed_tmdb_id=42`)
	for _, tc := range []struct {
		name, sql, raw, want string
	}{
		{"confirmed cache", "", "The Invite", "The Invite"},
		{"refreshed cache", `UPDATE movie_metadata_cache SET provider_title='  The Invite Updated  ' WHERE provider='tmdb' AND locale='fr-FR' AND provider_movie_id=42`, "  The Invite Updated  ", "The Invite Updated"},
		{"missing cache", `DELETE FROM movie_metadata_cache WHERE provider='tmdb' AND locale='fr-FR' AND provider_movie_id=42`, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.sql != "" {
				historyExec(t, pool, tc.sql)
			}
			loaded, _, err := store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			matched := 0
			for _, movie := range loaded.PublicMovies {
				if movie.TMDBID == 42 {
					matched++
					if movie.OriginalTitle != tc.raw || movie.Title != "L'Invitation éditée" {
						t.Fatalf("confirmed movie=%+v want raw=%q", movie, tc.raw)
					}
				} else if movie.OriginalTitle != "" {
					t.Fatalf("unmatched movie gained original title: %+v", movie)
				}
			}
			if matched != 1 || len(loaded.PublicMovies) != 4 {
				t.Fatalf("matched=%d public movies=%d", matched, len(loaded.PublicMovies))
			}
			source, err := NewPostgresSource(t.Context(), store)
			if err != nil {
				t.Fatal(err)
			}
			service, err := NewService(source, ServiceOptions{Now: func() time.Time { return time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC) }})
			if err != nil {
				t.Fatal(err)
			}
			catalog, err := service.Movies(MovieCatalogQuery{Search: "invitation"})
			if err != nil || catalog.Total != 1 || len(catalog.Items) != 1 || catalog.Items[0].ShowtimeCount != 2 {
				t.Fatalf("French search=%+v err=%v", catalog, err)
			}
			original := catalog.Items[0].OriginalTitle
			if tc.want == "" && original != nil || tc.want != "" && (original == nil || *original != tc.want) {
				t.Fatalf("catalog original_title=%v want=%q", original, tc.want)
			}
			searched, err := service.Movies(MovieCatalogQuery{Search: " THE INVITE "})
			wantTotal := 1
			if tc.want == "" {
				wantTotal = 0
			}
			if err != nil || searched.Total != wantTotal || len(searched.Items) != wantTotal {
				t.Fatalf("original search=%+v err=%v", searched, err)
			}
			timeline, err := service.Timeline(TimelineQuery{Date: "2026-08-15", TheaterIDs: []string{"ugc-25"}, Language: LanguageAll})
			if err != nil || len(timeline.Theaters) != 1 || len(timeline.Theaters[0].Showtimes) != 2 {
				t.Fatalf("timeline=%+v err=%v", timeline, err)
			}
			movie := timeline.Theaters[0].Showtimes[0].Movie
			if movie.Title != "L'Invitation éditée" || !strings.HasPrefix(movie.Slug, "film-") || tc.want == "" && movie.OriginalTitle != nil || tc.want != "" && (movie.OriginalTitle == nil || *movie.OriginalTitle != tc.want) {
				t.Fatalf("nested movie=%+v want original=%q", movie, tc.want)
			}
		})
	}
}
