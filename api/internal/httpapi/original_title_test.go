package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func originalTitleHandler(t *testing.T, original string, canonical bool) http.Handler {
	t.Helper()
	data := readinessFixtureDataset(t)
	data.Showtimes = data.Showtimes[:1]
	if canonical {
		data.Showtimes[0].Movie.PublicMovieID = 1
		data.PublicMovies = []schedule.PublicMovieRecord{{ID: 1, IdentityAnchorProvider: schedule.ProviderUGC, IdentityAnchorSourceID: "200", TMDBID: 42, Title: "L'Invitation", OriginalTitle: original, RuntimeMinutes: 100, UpdatedAt: data.GeneratedAt}}
		data.MovieSources = []schedule.PublicMovieSourceRecord{{Provider: schedule.ProviderUGC, SourceMovieID: "200", SourceSlug: data.Showtimes[0].Movie.Slug, PublicMovieID: 1, Title: "Film A", RuntimeMinutes: 100}}
	}
	if err := schedule.ValidateDataset(data, true); err != nil {
		t.Fatal(err)
	}
	service, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(data)}, schedule.ServiceOptions{Now: func() time.Time { return time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return NewHandler(service, "http://localhost:3000")
}

func TestOriginalTitleMovieWireContract(t *testing.T) {
	for _, tc := range []struct {
		name, original, title, want string
		canonical                   bool
	}{
		{"distinct", " \tThe Invite\n", "L'Invitation", "The Invite", true},
		{"missing", "", "L'Invitation", "", true},
		{"whitespace", " \t\n", "L'Invitation", "", true},
		{"legacy", "", "Film A", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slug := "film-1"
			if !tc.canonical {
				slug = "tmdb-film-42"
			}
			for _, route := range []struct {
				path   string
				movies int
			}{
				{"/api/v1/movies", 1},
				{"/api/v1/movies/" + slug + "/showtimes?date=2026-08-15", 2},
				{"/api/v1/theaters/ugc-25/showtimes?date=2026-08-15", 2},
				{"/api/v1/timeline?date=2026-08-15&theaters=ugc-25", 1},
				{"/api/v1/search/slot?date=2026-08-15&theaters=ugc-25&start_after=08:00&finish_before=02:00", 1},
			} {
				response := performRequest(t, originalTitleHandler(t, tc.original, tc.canonical), route.path)
				if response.Code != http.StatusOK {
					t.Fatalf("%s: %d %s", route.path, response.Code, response.Body)
				}
				assertOriginalTitleWire(t, response.Body.Bytes(), tc.title, tc.want, route.movies)
			}
		})
	}
}

func TestMoviesOriginalTitleSearchTransport(t *testing.T) {
	for _, tc := range []struct {
		search string
		total  int
	}{
		{" THE INVITE ", 1},
		{"invitation", 1},
		{"Invite", 1},
		{"invité - THE", 1},
		{"invítation l", 1},
		{"invitation invite", 0},
		{"the invite missing", 0},
		{"%_", 0},
		{"missing", 0},
	} {
		t.Run(tc.search, func(t *testing.T) {
			for _, scope := range []string{"", "&theaters=ugc-25&currently_screened=true"} {
				response := performRequest(t, originalTitleHandler(t, "The Invite", true), "/api/v1/movies?search="+url.QueryEscape(tc.search)+"&page_size=1"+scope)
				var catalog schedule.MovieCatalog
				if response.Code != http.StatusOK {
					t.Fatalf("%d %s", response.Code, response.Body)
				}
				if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil {
					t.Fatal(err)
				}
				if catalog.Total != tc.total || len(catalog.Items) != tc.total || catalog.Page != 1 || catalog.PageSize != 1 {
					t.Fatalf("catalog=%+v", catalog)
				}
				assertOriginalTitleWire(t, response.Body.Bytes(), "L'Invitation", "The Invite", tc.total)
			}
		})
	}
}

func TestUpcomingOriginalTitleWireContract(t *testing.T) {
	for _, original := range []string{"  The Invite  ", "", " \t\n"} {
		now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
		data := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Timezone: schedule.Timezone, GeneratedAt: now, UpcomingCompletedAt: now, PublicMovies: []schedule.PublicMovieRecord{{ID: 1, IdentityAnchorTMDBID: 42, TMDBID: 42, Title: "L'Invitation", OriginalTitle: original, HasUpcomingRelease: true, UpcomingActive: true, FrenchReleaseDate: "2026-10-07", UpdatedAt: now}}}
		if err := schedule.ValidateCatalogOnlyDataset(data); err != nil {
			t.Fatal(err)
		}
		service, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(data)}, schedule.ServiceOptions{Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{"/api/v1/movies/upcoming", "/api/v1/movies/film-1/showtimes?date=2026-09-13"} {
			response := performRequest(t, NewHandler(service, "http://localhost:3000"), path)
			if response.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", path, response.Code, response.Body)
			}
			want := ""
			if original == "  The Invite  " {
				want = "The Invite"
			}
			assertOriginalTitleWire(t, response.Body.Bytes(), "L'Invitation", want, 1)
		}
	}
}

func assertOriginalTitleWire(t *testing.T, body []byte, title, original string, wantMovies int) {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	count := 0
	var visit func(any)
	visit = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if _, movie := node["runtime_minutes"]; movie {
				count++
				got, exists := node["original_title"]
				if !exists || original == "" && got != nil || original != "" && got != original || node["title"] != title {
					t.Fatalf("movie=%v want title=%q nullable original_title=%q", node, title, original)
				}
			}
			for _, child := range node {
				visit(child)
			}
		case []any:
			for _, child := range node {
				visit(child)
			}
		}
	}
	visit(decoded)
	if count != wantMovies {
		t.Fatalf("movie count=%d want=%d body=%s", count, wantMovies, body)
	}
}
