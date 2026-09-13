package schedule

import (
	"errors"
	"reflect"
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
		{10, "Écho", "2026-09-14", []string{"Drame"}, true}, {2, "Écho", "2026-09-14", []string{"Action"}, true}, {3, "Alpha", "2026-10-02", []string{"Comédie"}, true}, {4, "Beyond", "2027-09-14", nil, false}, {5, "Today", "2026-09-13", nil, true}, {6, "Withdrawn", "", nil, false},
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
	now = now.Add(12 * time.Hour)
	aged, _ := s.UpcomingMovies(UpcomingMoviesQuery{})
	if aged.Total != 1 || aged.CatalogRevision == first.CatalogRevision || !aged.GeneratedAt.Equal(first.GeneratedAt) {
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
	for _, test := range []struct{ slug, status string }{{"film-2", "upcoming"}, {"film-4", "upcoming"}, {"film-5", "ended"}, {"film-6", "unavailable"}} {
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
	data.PublicMovies = []PublicMovieRecord{{ID: 1, IdentityAnchorProvider: ProviderUGC, IdentityAnchorSourceID: "200", Title: "Preview", RuntimeMinutes: 100, TMDBID: 42, HasUpcomingRelease: true, FrenchReleaseDate: "2026-09-01", UpcomingActive: true}}
	data.Showtimes = data.Showtimes[:1]
	data.Showtimes[0].Movie.PublicMovieID = 1
	data.MovieSources = []PublicMovieSourceRecord{{Provider: ProviderUGC, SourceMovieID: "200", PublicMovieID: 1, SourceSlug: "ugc-film-200"}}
	s, err := NewService(newTestSource(data), ServiceOptions{Now: testServiceNow})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.MovieShowtimes(MovieShowtimesQuery{Slug: "film-1", Date: "2026-08-15"})
	if err != nil || result.ReleaseStatus != "upcoming" || !result.CurrentlyScreened || len(result.Theaters) != 1 || len(result.Theaters[0].Showtimes) != 1 {
		t.Fatalf("preview=%+v err=%v", result, err)
	}
}
