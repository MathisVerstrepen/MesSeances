package schedule

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func scopedSearchService(t *testing.T) *Service {
	t.Helper()
	data := testDataset()
	data.Window.Through = "2026-08-17"
	for i := range data.Theaters {
		data.Theaters[i].AvailableDates = []string{"2026-08-15", "2026-08-16", "2026-08-17"}
	}
	data.PublicMovies = []PublicMovieRecord{
		{ID: 1, Title: "Film A ailleurs", RuntimeMinutes: 50, ReleaseDate: "2030-01-01", Genres: []string{"Animation"}},
		{ID: 2, Title: "Film B", RuntimeMinutes: 100, ReleaseDate: "2020-01-01", Genres: []string{"Drame"}},
		{ID: 3, Title: "Film C", RuntimeMinutes: 110, ReleaseDate: "2021-01-01", Genres: []string{"Drame"}},
		{ID: 4, Title: "Film Z autre date", RuntimeMinutes: 200, ReleaseDate: "2029-01-01", Genres: []string{"Drame"}},
		{ID: 5, Title: "Film D passé", RuntimeMinutes: 100, Genres: []string{"Drame"}},
		{ID: 6, Title: "Film E inédit", TMDBID: 6, OriginalTitle: "Unreleased original", RuntimeMinutes: 100, ReleaseDate: "2020-01-01", Genres: []string{"Comédie"}},
		{ID: 7, RedirectToID: 2, Title: "Film doublon", RuntimeMinutes: 100},
	}
	for i := range data.PublicMovies {
		movie := &data.PublicMovies[i]
		movie.IdentityAnchorProvider = ProviderUGC
		movie.IdentityAnchorSourceID = fmt.Sprint(movie.ID)
		movie.UpdatedAt = data.GeneratedAt
		if movie.RedirectToID == 0 {
			data.MovieSources = append(data.MovieSources, PublicMovieSourceRecord{Provider: ProviderUGC, SourceMovieID: movie.IdentityAnchorSourceID, PublicMovieID: movie.ID, SourceSlug: "ugc-film-" + movie.IdentityAnchorSourceID, Title: "Source", RuntimeMinutes: 100})
		}
	}
	template := data.Showtimes[0]
	location := template.StartTime.Location()
	data.Showtimes = nil
	add := func(movieID int64, theater, date string, start time.Time) {
		id := fmt.Sprint(len(data.Showtimes) + 1)
		record := template
		record.ID, record.ProviderShowingID = "ugc-showing-"+id, id
		record.BookingURL = "https://www.ugc.fr/reservationSeances.html?id=" + id
		record.TheaterID, record.ServiceDate = theater, date
		record.Movie = MovieRecord{ProviderID: fmt.Sprint(movieID), Slug: fmt.Sprintf("ugc-film-%d", movieID), PublicMovieID: movieID, Title: "Source", RuntimeMinutes: 100}
		record.StartTime, record.EndTime = start.In(location), start.Add(100*time.Minute).In(location)
		data.Showtimes = append(data.Showtimes, record)
	}
	now := testServiceNow()
	add(1, "ugc-26", "2026-08-15", now.Add(time.Hour))
	add(2, "ugc-25", "2026-08-15", now.Add(-20*time.Minute)) // Grace boundary remains current.
	add(2, "ugc-25", "2026-08-15", now.Add(time.Hour))
	// After midnight Paris, but still belongs to inclusive service-date endpoint.
	add(3, "ugc-25", "2026-08-16", time.Date(2026, 8, 16, 22, 30, 0, 0, time.UTC))
	add(4, "ugc-25", "2026-08-17", now.Add(48*time.Hour))
	add(5, "ugc-25", "2026-08-15", now.Add(-21*time.Minute))
	if err := ValidateDataset(data, true); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(newTestSource(data), ServiceOptions{Now: testServiceNow})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestMoviesIncludeEndedScopedSearchCountsAndEligibility(t *testing.T) {
	service := scopedSearchService(t)
	date, dateTo := "2026-08-15", "2026-08-16"
	query := MovieCatalogQuery{IncludeEnded: true, Search: " film ", TheaterIDs: []string{"ugc-25"}, Date: &date, DateTo: &dateTo, Sort: MovieCatalogSortTitleAsc, PageSize: 100}
	for _, tc := range []struct {
		name         string
		ids          []string
		date, dateTo *string
		counts       map[string]int
	}{
		{"selected range", []string{"ugc-25"}, &date, &dateTo, map[string]int{"film-2": 2, "film-3": 1}},
		{"nationwide range", nil, &date, &dateTo, map[string]int{"film-1": 1, "film-2": 2, "film-3": 1}},
		{"selected without date", []string{"ugc-25"}, nil, nil, map[string]int{"film-2": 2, "film-3": 1, "film-4": 1}},
		{"selected single date", []string{"ugc-25"}, &date, nil, map[string]int{"film-2": 2}},
		{"all zero", []string{"ugc-99"}, &date, &dateTo, map[string]int{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := query
			q.TheaterIDs, q.Date, q.DateTo = tc.ids, tc.date, tc.dateTo
			catalog, err := service.Movies(q)
			if err != nil || catalog.Total != 6 || len(catalog.Items) != 6 {
				t.Fatalf("catalog=%+v err=%v", catalog, err)
			}
			seen := map[string]bool{}
			for _, item := range catalog.Items {
				if item.ShowtimeCount != tc.counts[item.Slug] || item.Slug == "film-7" || seen[item.Slug] {
					t.Fatalf("item=%+v want counts=%v", item, tc.counts)
				}
				seen[item.Slug] = true
			}
			for id := 1; id <= 6; id++ {
				if !seen[fmt.Sprintf("film-%d", id)] {
					t.Fatalf("missing canonical movie %d", id)
				}
			}
		})
	}
	medium := MovieCatalogDurationMedium
	query.Genres, query.Duration = []string{"Drame"}, &medium
	filtered, err := service.Movies(query)
	if err != nil || filtered.Total != 3 || len(filtered.Items) != 3 || filtered.Items[0].Slug != "film-2" || filtered.Items[1].Slug != "film-3" || filtered.Items[2].Slug != "film-5" || !reflect.DeepEqual(filtered.AvailableGenres, []string{"Animation", "Comédie", "Drame"}) {
		t.Fatalf("filtered=%+v err=%v", filtered, err)
	}
	query.Genres, query.Duration, query.Search = nil, nil, "unreleased original"
	original, err := service.Movies(query)
	if err != nil || original.Total != 1 || len(original.Items) != 1 || original.Items[0].Slug != "film-6" || original.Items[0].ShowtimeCount != 0 {
		t.Fatalf("original=%+v err=%v", original, err)
	}
	query.Search, query.IncludeEnded = "film", false
	current, err := service.Movies(query)
	if err != nil || current.Total != 2 || len(current.Items) != 2 || current.Items[0].Slug != "film-2" || current.Items[1].Slug != "film-3" {
		t.Fatalf("current=%+v err=%v", current, err)
	}
}

func TestMoviesIncludeEndedSearchGroupingPrecedesSortAndPagination(t *testing.T) {
	service := scopedSearchService(t)
	date, dateTo := "2026-08-15", "2026-08-16"
	for _, tc := range []struct {
		sort  MovieCatalogSort
		want  []string
		blank []string
	}{
		{MovieCatalogSortTitleAsc, []string{"film-2", "film-3", "film-1", "film-5", "film-6", "film-4"}, []string{"film-1", "film-2", "film-3", "film-5", "film-6", "film-4"}},
		{MovieCatalogSortTitleDesc, []string{"film-3", "film-2", "film-4", "film-6", "film-5", "film-1"}, []string{"film-4", "film-6", "film-5", "film-3", "film-2", "film-1"}},
		{MovieCatalogSortRuntimeAsc, []string{"film-2", "film-3", "film-1", "film-5", "film-6", "film-4"}, []string{"film-1", "film-2", "film-5", "film-6", "film-3", "film-4"}},
		{MovieCatalogSortRuntimeDesc, []string{"film-3", "film-2", "film-4", "film-5", "film-6", "film-1"}, []string{"film-4", "film-3", "film-2", "film-5", "film-6", "film-1"}},
		{MovieCatalogSortReleaseDateDesc, []string{"film-3", "film-2", "film-1", "film-4", "film-6", "film-5"}, []string{"film-1", "film-4", "film-3", "film-2", "film-6", "film-5"}},
		{MovieCatalogSortShowtimesDesc, []string{"film-2", "film-3", "film-1", "film-5", "film-6", "film-4"}, []string{"film-2", "film-3", "film-1", "film-5", "film-6", "film-4"}},
		{"unknown", []string{"film-2", "film-3", "film-1", "film-5", "film-6", "film-4"}, []string{"film-2", "film-3", "film-1", "film-5", "film-6", "film-4"}},
	} {
		t.Run(string(tc.sort), func(t *testing.T) {
			query := MovieCatalogQuery{IncludeEnded: true, Search: "film", TheaterIDs: []string{"ugc-25"}, Date: &date, DateTo: &dateTo, Sort: tc.sort, PageSize: 100}
			full, err := service.Movies(query)
			if err != nil || full.Total != len(tc.want) || len(full.Items) != len(tc.want) {
				t.Fatalf("full=%+v err=%v", full, err)
			}
			for i, slug := range tc.want {
				if full.Items[i].Slug != slug {
					t.Fatalf("item[%d]=%s want=%s", i, full.Items[i].Slug, slug)
				}
			}
			query.PageSize = 1
			for page := 1; page <= len(tc.want)+1; page++ {
				query.Page = page
				got, err := service.Movies(query)
				if err != nil || got.Total != full.Total {
					t.Fatalf("page=%d catalog=%+v err=%v", page, got, err)
				}
				if page > len(tc.want) {
					if len(got.Items) != 0 {
						t.Fatalf("out of range items=%v", got.Items)
					}
				} else if !reflect.DeepEqual(got.Items, full.Items[page-1:page]) {
					t.Fatalf("page=%d items=%v want=%v", page, got.Items, full.Items[page-1:page])
				}
			}
			query.Page, query.PageSize, query.Search = 1, 100, " \t "
			blank, err := service.Movies(query)
			if err != nil || blank.Total != len(tc.blank) || len(blank.Items) != len(tc.blank) {
				t.Fatalf("blank=%+v err=%v", blank, err)
			}
			for i, slug := range tc.blank {
				if blank.Items[i].Slug != slug {
					t.Fatalf("blank item[%d]=%s want=%s", i, blank.Items[i].Slug, slug)
				}
			}
		})
	}
}
