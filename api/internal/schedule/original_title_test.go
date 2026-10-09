package schedule

import (
	"reflect"
	"testing"
)

func TestOriginalTitleMaterialization(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tmdbID   int64
		original string
		want     string
	}{
		{"distinct", 42, " \tThe Invite\n", "The Invite"},
		{"same title", 42, "Film A", "Film A"},
		{"missing", 42, "", ""},
		{"whitespace", 42, " \t\n", ""},
		{"unconfirmed", 0, "The Invite", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := testDataset()
			data.Showtimes[0].Movie.PublicMovieID = 1
			data.PublicMovies = []PublicMovieRecord{{ID: 1, TMDBID: tc.tmdbID, Title: "Film A", OriginalTitle: tc.original, RuntimeMinutes: 100}}
			view := NewSnapshotView(data)
			catalog := materializeCatalogMovie(view, data.Showtimes[0].Movie)
			showtime := materializeRecord(view, data.Showtimes[0])
			for _, original := range []*string{catalog.OriginalTitle, showtime.Movie.OriginalTitle} {
				if tc.want == "" && original != nil || tc.want != "" && (original == nil || *original != tc.want) {
					t.Fatalf("original_title=%v want=%q", original, tc.want)
				}
			}
			if catalog.Title != "Film A" || showtime.Movie.Title != "Film A" {
				t.Fatal("primary title changed")
			}
		})
	}
	data := testDataset()
	legacy := materializeRecord(NewSnapshotView(data), data.Showtimes[0])
	if legacy.Movie.OriginalTitle != nil || legacy.Movie.Title != data.Showtimes[0].Movie.Title {
		t.Fatalf("legacy fallback changed: %+v", legacy.Movie)
	}
}

func TestMoviesCatalogOriginalTitleSearch(t *testing.T) {
	data := testDataset()
	data.PublicMovies = []PublicMovieRecord{
		{ID: 1, IdentityAnchorProvider: ProviderUGC, IdentityAnchorSourceID: "200", TMDBID: 42, Title: "L'Invitation", OriginalTitle: "  The Invite  ", RuntimeMinutes: 130, Genres: []string{"Drame"}, UpdatedAt: data.GeneratedAt},
		{ID: 2, IdentityAnchorProvider: ProviderUGC, IdentityAnchorSourceID: "201", TMDBID: 43, Title: "Alpha", OriginalTitle: "The Invite Again", RuntimeMinutes: 95, UpdatedAt: data.GeneratedAt},
		{ID: 3, IdentityAnchorProvider: ProviderUGC, IdentityAnchorSourceID: "202", TMDBID: 44, Title: "The Invite français", OriginalTitle: "The Invite français", RuntimeMinutes: 75, UpdatedAt: data.GeneratedAt},
		{ID: 4, IdentityAnchorProvider: ProviderUGC, IdentityAnchorSourceID: "203", Title: "Film D", RuntimeMinutes: 90, UpdatedAt: data.GeneratedAt},
		{ID: 5, IdentityAnchorTMDBID: 45, TMDBID: 45, Title: "Épilogue", OriginalTitle: "The Invite ended", UpdatedAt: data.GeneratedAt},
	}
	for i := range data.Showtimes {
		record := &data.Showtimes[i].Movie
		for _, movie := range data.PublicMovies[:4] {
			if record.ProviderID == movie.IdentityAnchorSourceID {
				record.PublicMovieID = movie.ID
			}
		}
	}
	for _, movie := range data.PublicMovies[:4] {
		data.MovieSources = append(data.MovieSources, PublicMovieSourceRecord{Provider: ProviderUGC, SourceMovieID: movie.IdentityAnchorSourceID, PublicMovieID: movie.ID, SourceSlug: "ugc-film-" + movie.IdentityAnchorSourceID, Title: movie.Title, RuntimeMinutes: movie.RuntimeMinutes})
	}
	if err := ValidateDataset(data, true); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(newTestSource(data), ServiceOptions{Now: testServiceNow})
	if err != nil {
		t.Fatal(err)
	}
	long := MovieCatalogDurationLong
	tomorrow := "tomorrow"
	for _, tc := range []struct {
		name  string
		query MovieCatalogQuery
		slugs []string
		total int
		count int
	}{
		{"original case and whitespace", MovieCatalogQuery{Search: " THE INVITE "}, []string{"film-2", "film-1", "film-3"}, 3, 1},
		{"French substring", MovieCatalogQuery{Search: " INVITATION "}, []string{"film-1"}, 1, 2},
		{"French accents", MovieCatalogQuery{Search: "FRANÇAIS"}, []string{"film-3"}, 1, 1},
		{"accent folding", MovieCatalogQuery{Search: "francais"}, []string{"film-3"}, 1, 1},
		{"missing original falls back", MovieCatalogQuery{Search: "film d"}, []string{"film-4"}, 1, 1},
		{"no match", MovieCatalogQuery{Search: "unknown"}, []string{}, 0, 0},
		{"page one", MovieCatalogQuery{Search: "the invite", PageSize: 1}, []string{"film-2"}, 3, 1},
		{"page two", MovieCatalogQuery{Search: "the invite", PageSize: 1, Page: 2}, []string{"film-1"}, 3, 2},
		{"page out of range", MovieCatalogQuery{Search: "the invite", PageSize: 1, Page: 4}, []string{}, 3, 0},
		{"theater filter", MovieCatalogQuery{Search: "the invite", TheaterIDs: []string{"ugc-26"}}, []string{"film-1", "film-3"}, 2, 1},
		{"genre filter", MovieCatalogQuery{Search: "the invite", Genres: []string{"Drame"}}, []string{"film-1"}, 1, 2},
		{"duration filter", MovieCatalogQuery{Search: "the invite", Duration: &long}, []string{"film-1"}, 1, 2},
		{"date filter", MovieCatalogQuery{Search: "the invite", Date: &tomorrow}, []string{}, 0, 0},
		{"include ended", MovieCatalogQuery{Search: "the invite", IncludeEnded: true}, []string{"film-2", "film-1", "film-3", "film-5"}, 4, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.query.Sort = MovieCatalogSortTitleAsc
			result, err := service.Movies(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			slugs := make([]string, 0, len(result.Items))
			for _, movie := range result.Items {
				slugs = append(slugs, movie.Slug)
			}
			if result.Total != tc.total || !reflect.DeepEqual(slugs, tc.slugs) || len(result.Items) > 0 && result.Items[0].ShowtimeCount != tc.count {
				t.Fatalf("catalog=%+v slugs=%v want=%v total=%d count=%d", result, slugs, tc.slugs, tc.total, tc.count)
			}
		})
	}
}
