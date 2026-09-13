package schedule

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func upcomingCatalogFixture() Dataset {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	data := Dataset{SchemaVersion: SchemaVersion, Timezone: Timezone, GeneratedAt: now, UpcomingCompletedAt: now}
	for _, v := range []struct {
		id          int64
		title, date string
		genres      []string
		active      bool
	}{
		{10, "Écho", "2026-09-16", []string{"Drame"}, true}, {2, "Écho", "2026-09-16", []string{"Action"}, true}, {3, "Alpha", "2026-10-02", []string{"Comédie"}, true}, {4, "Beyond", "2027-09-14", nil, false}, {5, "Today", "2026-09-13", nil, true}, {6, "Withdrawn", "", nil, false},
		{7, "Current week Monday", "2026-09-14", []string{"Animation"}, true}, {8, "Current week Tuesday", "2026-09-15", []string{"Animation"}, true},
	} {
		data.PublicMovies = append(data.PublicMovies, PublicMovieRecord{ID: v.id, IdentityAnchorTMDBID: v.id, TMDBID: v.id, Title: v.title, FrenchReleaseDate: v.date, HasUpcomingRelease: true, UpcomingActive: v.active, Genres: v.genres, UpdatedAt: now})
	}
	return data
}

func TestUpcomingCalendarWindow(t *testing.T) {
	for _, test := range []struct{ now, from, through string }{
		{"2026-09-13T21:59:59Z", "2026-09-14", "2027-09-13"},
		{"2026-09-13T22:00:00Z", "2026-09-15", "2027-09-14"},
		{"2028-02-29T12:00:00Z", "2028-03-01", "2029-02-28"},
		{"2027-02-28T12:00:00Z", "2027-03-01", "2028-02-28"},
		{"2026-03-29T01:00:00Z", "2026-03-30", "2027-03-29"},
		{"2026-10-25T01:00:00Z", "2026-10-26", "2027-10-25"},
	} {
		now, _ := time.Parse(time.RFC3339, test.now)
		if got := UpcomingWindow(now); got != (Window{From: test.from, Through: test.through}) {
			t.Fatalf("%s: %+v", test.now, got)
		}
	}
}

func TestUpcomingCatalogFilteringPagingAndMidnight(t *testing.T) {
	data := upcomingCatalogFixture()
	if err := ValidateSnapshotDataset(data, SnapshotRevision{EnrichmentVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDataset(data, true); err == nil {
		t.Fatal("catalog-only passed complete validation")
	}
	now := data.GeneratedAt
	s, err := NewService(testSource{NewSnapshotView(data, SnapshotRevision{EnrichmentVersion: 1})}, ServiceOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if !s.HasCatalog() || s.HasSnapshot() {
		t.Fatal("availability conflated")
	}
	first, err := s.UpcomingMovies(UpcomingMoviesQuery{PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 3 || len(first.Items) != 1 || first.Items[0].Slug != "film-2" || !reflect.DeepEqual(first.AvailableMonths, []string{"2026-09", "2026-10"}) || !reflect.DeepEqual(first.AvailableGenres, []string{"Action", "Comédie", "Drame"}) {
		t.Fatalf("first=%+v", first)
	}
	second, _ := s.UpcomingMovies(UpcomingMoviesQuery{Page: 2, PageSize: 1})
	if second.Items[0].Slug != "film-10" {
		t.Fatalf("second=%+v", second)
	}
	filtered, _ := s.UpcomingMovies(UpcomingMoviesQuery{Month: "2026-09", Genres: []string{"action", "Comédie"}})
	if filtered.Total != 1 || !reflect.DeepEqual(filtered.AvailableGenres, first.AvailableGenres) || !reflect.DeepEqual(filtered.AvailableMonths, first.AvailableMonths) {
		t.Fatalf("filtered=%+v", filtered)
	}
	for _, q := range []UpcomingMoviesQuery{{Month: "2030-01"}, {Page: int(^uint(0) >> 1)}} {
		result, err := s.UpcomingMovies(q)
		if err != nil || result.Items == nil || len(result.Items) != 0 {
			t.Fatalf("empty=%+v error=%v", result, err)
		}
	}
	now = time.Date(2026, 9, 15, 21, 59, 59, 0, time.UTC)
	before, err := s.UpcomingMovies(UpcomingMoviesQuery{})
	if err != nil || before.Total != 3 || before.Window.From != "2026-09-16" {
		t.Fatalf("before Wednesday=%+v err=%v", before, err)
	}
	now = now.Add(time.Second)
	aged, _ := s.UpcomingMovies(UpcomingMoviesQuery{})
	if aged.Total != 1 || aged.Window.From != "2026-09-23" || aged.CatalogRevision == before.CatalogRevision || !aged.GeneratedAt.Equal(first.GeneratedAt) {
		t.Fatalf("aged=%+v", aged)
	}
	for _, q := range []UpcomingMoviesQuery{{Month: "2026-9"}, {Month: "2026-13"}, {Page: -1}, {PageSize: 101}, {Genres: []string{""}}} {
		if _, err := s.UpcomingMovies(q); err == nil {
			t.Fatalf("accepted %+v", q)
		}
	}
}

func TestUpcomingDetailStatusesAndBundleClock(t *testing.T) {
	data := upcomingCatalogFixture()
	clockCalls := 0
	s, err := NewService(testSource{NewSnapshotView(data, SnapshotRevision{EnrichmentVersion: 1})}, ServiceOptions{Now: func() time.Time { clockCalls++; return data.GeneratedAt }})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ slug, status string }{{"film-2", "upcoming"}, {"film-4", "upcoming"}, {"film-5", "ended"}, {"film-6", "unavailable"}, {"film-7", "upcoming"}, {"film-8", "upcoming"}} {
		before := clockCalls
		scoped, national, err := s.MovieShowtimesBundle(MovieShowtimesQuery{Slug: test.slug, Date: "2026-09-13", City: "Paris"})
		if err != nil || scoped.ReleaseStatus != test.status || !reflect.DeepEqual(scoped, national) || scoped.CurrentlyScreened || scoped.Theaters == nil || scoped.AvailableDates == nil || clockCalls != before+1 {
			t.Fatalf("scoped=%+v nationwide=%+v err=%v clock=%d", scoped, national, err, clockCalls-before)
		}
	}
	if _, err := s.MovieShowtimes(MovieShowtimesQuery{Slug: "film-2", Date: "2026-09-13", TheaterIDs: []string{"ugc-unknown"}}); err == nil {
		t.Fatal("accepted unknown theater")
	}
	if _, err := s.MovieShowtimes(MovieShowtimesQuery{Slug: "film-2"}); err == nil {
		t.Fatal("accepted missing date")
	}
	data.UpcomingCompletedAt = time.Time{}
	unpublished, _ := NewService(newTestSource(data), ServiceOptions{})
	if _, err := unpublished.UpcomingMovies(UpcomingMoviesQuery{}); !errors.Is(err, ErrUpcomingUnavailable) {
		t.Fatalf("never-run=%v", err)
	}
}

func TestUpcomingPreviewStillHasSessions(t *testing.T) {
	data := testDataset()
	data.UpcomingCompletedAt = data.GeneratedAt
	data.PublicMovies = []PublicMovieRecord{{ID: 1, IdentityAnchorProvider: ProviderUGC, IdentityAnchorSourceID: "200", Title: "Preview", RuntimeMinutes: 100, TMDBID: 42, HasUpcomingRelease: true, FrenchReleaseDate: "2026-08-17", UpcomingActive: true}}
	data.Showtimes = data.Showtimes[:1]
	data.Showtimes[0].Movie.PublicMovieID = 1
	data.MovieSources = []PublicMovieSourceRecord{{Provider: ProviderUGC, SourceMovieID: "200", PublicMovieID: 1, SourceSlug: "ugc-film-200"}}
	s, err := NewService(newTestSource(data), ServiceOptions{Now: testServiceNow})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.MovieShowtimes(MovieShowtimesQuery{Slug: "film-1", Date: "2026-08-15"})
	if err != nil || result.ReleaseStatus != "upcoming" || !result.CurrentlyScreened || len(result.Theaters) != 1 || len(result.Theaters[0].Showtimes) != 1 || result.Movie.FrenchReleaseDate == nil || *result.Movie.FrenchReleaseDate != "2026-08-17" {
		t.Fatalf("preview=%+v err=%v", result, err)
	}
	upcoming, err := s.UpcomingMovies(UpcomingMoviesQuery{})
	if err != nil || upcoming.Total != 0 {
		t.Fatalf("current-week preview listed=%+v err=%v", upcoming, err)
	}
	inventory, err := s.Movies(MovieCatalogQuery{})
	if err != nil || len(inventory.Items) != 1 {
		t.Fatalf("current-week inventory=%+v err=%v", inventory, err)
	}
}

func TestUpcomingDisplayEligibilityBeforeFacetsFiltersAndPages(t *testing.T) {
	for _, test := range []struct{ name, today, hidden, from, through, beyond string }{
		{"current week", "2026-09-13", "2026-09-15", "2026-09-16", "2027-09-13", "2027-09-14"},
		{"hidden unique month", "2026-08-30", "2026-08-31", "2026-09-02", "2027-08-30", "2027-08-31"},
		{"leap clamp", "2028-02-29", "2028-02-29", "2028-03-01", "2029-02-28", "2029-03-01"},
	} {
		t.Run(test.name, func(t *testing.T) {
			location, err := time.LoadLocation(Timezone)
			if err != nil {
				t.Fatal(err)
			}
			now, err := time.ParseInLocation(time.DateOnly, test.today, location)
			if err != nil {
				t.Fatal(err)
			}
			data := Dataset{SchemaVersion: SchemaVersion, Timezone: Timezone, GeneratedAt: now, UpcomingCompletedAt: now}
			for i, date := range []string{test.hidden, test.from, test.through, test.beyond} {
				genre := "Drame"
				if i == 0 || i == 3 {
					genre = "Animation"
				}
				id := int64(i + 1)
				data.PublicMovies = append(data.PublicMovies, PublicMovieRecord{ID: id, IdentityAnchorTMDBID: id, TMDBID: id, Title: "Film", FrenchReleaseDate: date, HasUpcomingRelease: true, UpcomingActive: true, Genres: []string{genre}, UpdatedAt: now})
			}
			view := NewSnapshotView(data, SnapshotRevision{EnrichmentVersion: 1})
			s, err := NewService(testSource{view}, ServiceOptions{Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			for page := 1; page <= 3; page++ {
				result, err := s.UpcomingMovies(UpcomingMoviesQuery{Page: page, PageSize: 1})
				if err != nil || result.Total != 2 || result.Window != (Window{From: test.from, Through: test.through}) || !reflect.DeepEqual(result.AvailableGenres, []string{"Drame"}) || !reflect.DeepEqual(result.AvailableMonths, []string{test.from[:7], test.through[:7]}) {
					t.Fatalf("page %d=%+v err=%v", page, result, err)
				}
				if page <= 2 {
					want := []string{test.from, test.through}[page-1]
					if len(result.Items) != 1 || result.Items[0].FrenchReleaseDate == nil || *result.Items[0].FrenchReleaseDate != want {
						t.Fatalf("page %d exact date=%+v", page, result.Items)
					}
				} else if len(result.Items) != 0 {
					t.Fatalf("extra page=%+v", result.Items)
				}
			}
			queries := []UpcomingMoviesQuery{{Genres: []string{"Animation"}}, {Month: test.hidden[:7], Genres: []string{"Animation"}}}
			if test.hidden[:7] != test.from[:7] {
				queries = append(queries, UpcomingMoviesQuery{Month: test.hidden[:7]})
			}
			for _, query := range queries {
				result, err := s.UpcomingMovies(query)
				if err != nil || result.Total != 0 || len(result.Items) != 0 || !reflect.DeepEqual(result.AvailableGenres, []string{"Drame"}) {
					t.Fatalf("hidden filter=%+v err=%v", result, err)
				}
			}
			if !reflect.DeepEqual(view.data.PublicMovies, data.PublicMovies) {
				t.Fatal("display changed stored movie records")
			}
		})
	}
}

func TestUpcomingExclusionOnlyAffectsUpcomingList(t *testing.T) {
	data := upcomingCatalogFixture()
	for i := range data.PublicMovies {
		if data.PublicMovies[i].ID == 3 {
			data.PublicMovies[i].UpcomingExcluded = true
		}
	}
	s, err := NewService(testSource{NewSnapshotView(data, SnapshotRevision{EnrichmentVersion: 2})}, ServiceOptions{Now: func() time.Time { return data.GeneratedAt }})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.UpcomingMovies(UpcomingMoviesQuery{PageSize: 1})
	if err != nil || result.Total != 2 || len(result.Items) != 1 || !reflect.DeepEqual(result.AvailableGenres, []string{"Action", "Drame"}) || !reflect.DeepEqual(result.AvailableMonths, []string{"2026-09"}) {
		t.Fatalf("exclusion facets %+v %v", result, err)
	}
	filtered, err := s.UpcomingMovies(UpcomingMoviesQuery{Genres: []string{"Comédie"}})
	if err != nil || filtered.Total != 0 {
		t.Fatal("excluded genre matched")
	}
	detail, err := s.MovieShowtimes(MovieShowtimesQuery{Slug: "film-3", Date: "2026-09-13"})
	if err != nil || detail.ReleaseStatus != "upcoming" {
		t.Fatalf("detail %+v %v", detail, err)
	}
	encoded, err := json.Marshal([]any{result, detail})
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"UpcomingExcluded", "decision", "french_releases", "reason_codes", "assessed_at", "assessment_status", "review_revision"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("private field %s", private)
		}
	}
	// Real sessions, generic inventory and status remain independent from exclusion.
	data = testDataset()
	data.UpcomingCompletedAt = data.GeneratedAt
	data.PublicMovies = []PublicMovieRecord{{ID: 1, IdentityAnchorProvider: ProviderUGC, IdentityAnchorSourceID: "200", Title: "Preview", RuntimeMinutes: 100, TMDBID: 42, HasUpcomingRelease: true, FrenchReleaseDate: "2026-09-01", UpcomingActive: true, UpcomingExcluded: true}}
	data.Showtimes = data.Showtimes[:1]
	data.Showtimes[0].Movie.PublicMovieID = 1
	data.MovieSources = []PublicMovieSourceRecord{{Provider: ProviderUGC, SourceMovieID: "200", PublicMovieID: 1, SourceSlug: "ugc-film-200"}}
	s, err = NewService(newTestSource(data), ServiceOptions{Now: testServiceNow})
	if err != nil {
		t.Fatal(err)
	}
	detail, err = s.MovieShowtimes(MovieShowtimesQuery{Slug: "film-1", Date: "2026-08-15"})
	if err != nil || !detail.CurrentlyScreened || len(detail.Theaters) != 1 || detail.ReleaseStatus != "upcoming" {
		t.Fatalf("real sessions hidden %+v %v", detail, err)
	}
	inventory, err := s.Movies(MovieCatalogQuery{})
	if err != nil || len(inventory.Items) != 1 {
		t.Fatalf("inventory %+v %v", inventory, err)
	}
}
