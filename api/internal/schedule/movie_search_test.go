package schedule

import (
	"encoding/json"
	"os"
	"testing"
)

func TestMoviesSharedTitleSearch(t *testing.T) {
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
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			data := testDataset()
			data.Showtimes = data.Showtimes[:1]
			data.Showtimes[0].Movie.PublicMovieID = 1
			original := ""
			if tc.OriginalTitle != nil {
				original = *tc.OriginalTitle
			}
			data.PublicMovies = []PublicMovieRecord{{ID: 1, IdentityAnchorTMDBID: 42, TMDBID: 42, Title: tc.Title, OriginalTitle: original, RuntimeMinutes: 100, UpdatedAt: data.GeneratedAt}}
			service, err := NewService(newTestSource(data), ServiceOptions{Now: testServiceNow})
			if err != nil {
				t.Fatal(err)
			}
			for _, theaters := range [][]string{nil, {"ugc-25"}} {
				result, err := service.Movies(MovieCatalogQuery{Search: tc.Query, TheaterIDs: theaters})
				if err != nil {
					t.Fatal(err)
				}
				want := 0
				if tc.Expected {
					want = 1
				}
				if result.Total != want || len(result.Items) != want {
					t.Fatalf("theaters=%v result=%+v want=%d", theaters, result, want)
				}
				if want > 0 && (result.Items[0].Title != tc.Title || result.Items[0].ShowtimeCount != 1) {
					t.Fatal("display title or counts changed")
				}
			}
		})
	}
}

func TestMoviesTitleSearchCannotBorrowAcrossMovies(t *testing.T) {
	data := testDataset()
	data.Showtimes = data.Showtimes[:2]
	data.Showtimes[0].Movie.PublicMovieID = 1
	data.Showtimes[1].Movie.PublicMovieID = 2
	data.PublicMovies = []PublicMovieRecord{
		{ID: 1, IdentityAnchorTMDBID: 42, TMDBID: 42, Title: "Spider", RuntimeMinutes: 100, UpdatedAt: data.GeneratedAt},
		{ID: 2, IdentityAnchorTMDBID: 43, TMDBID: 43, Title: "Man", RuntimeMinutes: 100, UpdatedAt: data.GeneratedAt},
	}
	service, err := NewService(newTestSource(data), ServiceOptions{Now: testServiceNow})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Movies(MovieCatalogQuery{Search: "spider man"})
	if err != nil || result.Total != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestMoviesLegacySearchUsesRawTitle(t *testing.T) {
	data := testDataset()
	data.Showtimes = data.Showtimes[:1]
	// Default Unicode lowercase maps this final sigma differently from the
	// simple lowercase used by legacy identity/variant keys.
	data.Showtimes[0].Movie.Title = "ΟΣ: Spider‑Man"
	service, err := NewService(newTestSource(data), ServiceOptions{Now: testServiceNow})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Movies(MovieCatalogQuery{Search: "man ος spider"})
	if err != nil || result.Total != 1 || len(result.Items) != 1 || result.Items[0].Title != data.Showtimes[0].Movie.Title {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
