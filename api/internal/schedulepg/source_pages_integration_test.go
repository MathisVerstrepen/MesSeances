package schedulepg

import (
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestSourceMoviePagesPublicationReloadAndCopyIntegration(t *testing.T) {
	for _, tc := range []struct {
		name string
		data schedule.Dataset
		page string
	}{
		{"cgr", cgrTestDataset(), "https://www.cgrcinemas.fr/films-a-l-affiche/1001-original-title/"},
		{"cineville", cinevilleTestDataset(), "https://www.cineville.fr/programmes/katorzaquimper#-693091020261"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := newHistoryPool(t)
			store := NewStore(pool)
			data := tc.data
			page := tc.page
			data.Showtimes[0].Movie.SourcePageURL = page
			historyPublish(t, store, data)
			assertURL := func(want string) {
				t.Helper()
				loaded, _, err := store.Load(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				var movie schedule.MovieRecord
				for _, r := range loaded.Showtimes {
					if r.Provider == data.Provider {
						movie = r.Movie
					}
				}
				if movie.SourcePageURL != want {
					t.Fatalf("source URL lost on reload: %q", movie.SourcePageURL)
				}
				source, err := schedule.NewPostgresSource(t.Context(), store)
				if err != nil {
					t.Fatal(err)
				}
				service, err := schedule.NewService(source, schedule.ServiceOptions{Now: func() time.Time { return data.GeneratedAt }})
				if err != nil {
					t.Fatal(err)
				}
				slug := "film-" // Resolve via the catalog rather than assuming sequence values.
				catalog, err := service.Movies(schedule.MovieCatalogQuery{PageSize: 100})
				if err != nil {
					t.Fatal(err)
				}
				for _, m := range catalog.Items {
					if m.Title == data.Showtimes[0].Movie.Title {
						slug = m.Slug
					}
				}
				result, err := service.MovieShowtimes(schedule.MovieShowtimesQuery{Slug: slug, Date: data.Window.From, TheaterIDs: []string{data.Theaters[0].ID}})
				if err != nil || len(result.Theaters) != 1 {
					t.Fatalf("detail unavailable: %+v %v", result, err)
				}
				got := result.Theaters[0].MovieURL
				if want == "" {
					if got != nil {
						t.Fatalf("missing source should be null: %v", got)
					}
				} else if got == nil || *got != want {
					t.Fatal("source URL not projected")
				}
			}
			assertURL(page)
			// Replacing another chain copies the captured URL into the next generation.
			historyPublish(t, store, testDataset())
			assertURL(page)
			// Invalid new observations cannot overwrite a valid published generation.
			data.Showtimes[0].Movie.SourcePageURL = "https://evil.example/film"
			if _, err := store.Replace(t.Context(), []schedule.Dataset{data}); err == nil {
				t.Fatal("unsafe URL published")
			}
			assertURL(page)
			// A successful observation without a published film page clears the link.
			data.Showtimes[0].Movie.SourcePageURL = ""
			historyPublish(t, store, data)
			assertURL("")
		})
	}
}
