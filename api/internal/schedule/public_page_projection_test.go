package schedule

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"
)

func pageProjection(t *testing.T, data Dataset, now time.Time, images map[string]PublicPageImage) SitemapProjection {
	t.Helper()
	if err := ValidateDataset(data, true); err != nil {
		t.Fatal(err)
	}
	result, err := BuildSitemapProjection(NewSnapshotView(data), now, images)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPublicPageContentIsolationAndBookkeeping(t *testing.T) {
	data := publicPageFixture()
	baseline := pageProjection(t, data, pageNow(), nil)
	if baseline.Data.MovieTotal != 6 || len(baseline.Data.Movies) != 6 || len(baseline.Fingerprints) != 12 || len(baseline.Data.LastmodByPath) != 12 || baseline.Data.UpcomingAvailable {
		t.Fatalf("inventory=%+v", baseline.Data)
	}
	for path, date := range baseline.Data.LastmodByPath {
		if date != nil || path == "/films" {
			t.Fatal("invented baseline/hub date")
		}
	}
	bookkeeping := cloneDataset(data)
	bookkeeping.GeneratedAt = bookkeeping.GeneratedAt.Add(time.Minute)
	for i := range bookkeeping.PublicMovies {
		bookkeeping.PublicMovies[i].UpdatedAt = bookkeeping.PublicMovies[i].UpdatedAt.Add(time.Hour)
	}
	bookkeeping.UpcomingCompletedAt = pageNow()
	bookkeeping.CatalogPublishedAt = pageNow()
	slices.Reverse(bookkeeping.Theaters)
	slices.Reverse(bookkeeping.Showtimes)
	slices.Reverse(bookkeeping.PublicMovies)
	result := pageProjection(t, bookkeeping, pageNow().Add(time.Minute), nil)
	if !reflect.DeepEqual(result.Fingerprints, baseline.Fingerprints) || !result.Data.UpcomingAvailable {
		t.Fatal("bookkeeping/order/clock changed public content")
	}
	changed := cloneDataset(data)
	changed.PublicMovies[0].Overview = "Real changed synopsis"
	result = pageProjection(t, changed, pageNow(), nil)
	for path, fingerprint := range baseline.Fingerprints {
		if (fingerprint != result.Fingerprints[path]) != (path == "/film/film-1") {
			t.Fatalf("overview isolation: %s", path)
		}
	}
	changed = cloneDataset(data)
	changed.PublicMovies[0].Title = "New title"
	result = pageProjection(t, changed, pageNow(), nil)
	for _, path := range []string{"/film/film-4", "/cinema/ugc-99", "/ville/lyon/cinemas", "/film/film-5"} {
		if baseline.Fingerprints[path] != result.Fingerprints[path] {
			t.Fatalf("unrelated metadata change: %s", path)
		}
	}
}

func TestPublicPageProgrammeChanges(t *testing.T) {
	data := publicPageFixture()
	baseline := pageProjection(t, data, pageNow(), nil)
	mutations := map[string]func(*Dataset){
		"addition": func(d *Dataset) {
			s := d.Showtimes[0]
			s.ID = "ugc-showing-999"
			s.ProviderShowingID = "999"
			s.BookingURL = "https://www.ugc.fr/reservationSeances.html?id=999"
			d.Showtimes = append(d.Showtimes, s)
		},
		"newest removal older remains": func(d *Dataset) { d.Showtimes = append(d.Showtimes[:1], d.Showtimes[2:]...) },
		"final cancellation":           func(d *Dataset) { d.Showtimes = d.Showtimes[:4] },
		"language":                     func(d *Dataset) { d.Showtimes[0].Language = LanguageVF },
		"room":                         func(d *Dataset) { d.Showtimes[0].Room = "Salle 7" },
		"time":                         func(d *Dataset) { d.Showtimes[0].StartTime = d.Showtimes[0].StartTime.Add(time.Minute) },
		"booking": func(d *Dataset) {
			d.Showtimes[0].BookingURL = "https://www.ugc.fr/reservationSeances.html?id=%31%30%30"
		},
		"format":                func(d *Dataset) { d.Showtimes[0].Format = Format3D },
		"membership same count": func(d *Dataset) { d.Showtimes[0].Movie = d.Showtimes[2].Movie },
		"theater rename":        func(d *Dataset) { d.Theaters[0].Name = "Renamed cinema" },
		"theater address":       func(d *Dataset) { d.Theaters[0].Address = "New address" },
		"theater city":          func(d *Dataset) { d.Theaters[0].City = "Paris" },
		"theater slug": func(d *Dataset) {
			d.Theaters[0].Slug = "ugc-250"
			d.Theaters[0].ID = "ugc-250"
			d.Theaters[0].ProviderID = "250"
			for i := range d.Showtimes {
				if d.Showtimes[i].TheaterID == "ugc-25" {
					d.Showtimes[i].TheaterID = "ugc-250"
				}
			}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := cloneDataset(data)
			mutate(&changed)
			result := pageProjection(t, changed, pageNow(), nil)
			if reflect.DeepEqual(result.Fingerprints, baseline.Fingerprints) {
				t.Fatal("missed significant change")
			}
			if result.Fingerprints["/film/film-5"] != baseline.Fingerprints["/film/film-5"] {
				t.Fatal("ended unrelated film changed")
			}
			if name != "final cancellation" {
				for _, path := range []string{"/film/film-4", "/cinema/ugc-99", "/ville/lyon/cinemas"} {
					if result.Fingerprints[path] != baseline.Fingerprints[path] {
						t.Fatalf("unrelated %s", path)
					}
				}
			}
		})
	}
}

func TestPublicPageIdentityMergeSplitAndPhoto(t *testing.T) {
	data := publicPageFixture()
	baseline := pageProjection(t, data, pageNow(), nil)
	merged := cloneDataset(data)
	merged.PublicMovies[1].RedirectToID = 1
	for i := range merged.Showtimes {
		if merged.Showtimes[i].Movie.PublicMovieID == 2 {
			merged.Showtimes[i].Movie.PublicMovieID = 1
		}
	}
	for i := range merged.MovieSources {
		if merged.MovieSources[i].PublicMovieID == 2 {
			merged.MovieSources[i].PublicMovieID = 1
		}
	}
	result := pageProjection(t, merged, pageNow(), nil)
	if result.Data.MovieTotal != 5 || result.Fingerprints["/film/film-1"] == baseline.Fingerprints["/film/film-1"] {
		t.Fatal("merge not reflected")
	}
	if _, exists := result.Fingerprints["/film/film-2"]; exists {
		t.Fatal("redirected path remains")
	}
	split := pageProjection(t, data, pageNow(), nil)
	if !reflect.DeepEqual(split.Fingerprints, baseline.Fingerprints) {
		t.Fatal("split failed")
	}
	for _, image := range []PublicPageImage{{URL: "/api/v1/theaters/ugc/25/image/1", Width: 800, Height: 600}, {URL: "/api/v1/theaters/ugc/25/image/2", Width: 800, Height: 600}} {
		withPhoto := pageProjection(t, data, pageNow(), map[string]PublicPageImage{"ugc-25": image})
		for path, fingerprint := range baseline.Fingerprints {
			if (fingerprint != withPhoto.Fingerprints[path]) != (path == "/cinema/ugc-25") {
				t.Fatalf("photo isolation %s", path)
			}
		}
		baseline = withPhoto
	}
	removed := pageProjection(t, data, pageNow(), nil)
	if removed.Fingerprints["/cinema/ugc-25"] == baseline.Fingerprints["/cinema/ugc-25"] {
		t.Fatal("photo removal ignored")
	}
}

func TestPublicPageTimeTransitions(t *testing.T) {
	data := publicPageFixture()
	before := pageProjection(t, data, pageNow(), nil)
	after := pageProjection(t, data, pageNow().Add(time.Minute), nil)
	if !reflect.DeepEqual(before.Fingerprints, after.Fingerprints) {
		t.Fatal("raw clock used as content")
	}
	start := data.Showtimes[4].StartTime
	atBoundary := pageProjection(t, data, start.Add(20*time.Minute), nil)
	afterBoundary := pageProjection(t, data, start.Add(20*time.Minute+time.Nanosecond), nil)
	if atBoundary.Fingerprints["/film/film-4"] == afterBoundary.Fingerprints["/film/film-4"] || atBoundary.Fingerprints["/ville/lyon/cinemas"] == afterBoundary.Fingerprints["/ville/lyon/cinemas"] {
		t.Fatal("current-screening expiry not visible")
	}
	if atBoundary.Fingerprints["/cinema/ugc-99"] != afterBoundary.Fingerprints["/cinema/ugc-99"] {
		t.Fatal("cinema dated scheduled counts are not future-only")
	}
	beforeMidnight := time.Date(2026, 8, 15, 21, 59, 0, 0, time.UTC)
	afterMidnight := beforeMidnight.Add(time.Minute)
	before = pageProjection(t, data, beforeMidnight, nil)
	after = pageProjection(t, data, afterMidnight, nil)
	for _, path := range []string{"/film/film-6", "/film/film-1", "/cinema/ugc-25", "/ville/lille/cinemas"} {
		if before.Fingerprints[path] == after.Fingerprints[path] {
			t.Fatalf("midnight/release crossover absent %s", path)
		}
	}
	if before.Fingerprints["/film/film-5"] != after.Fingerprints["/film/film-5"] {
		t.Fatal("Paris date injected into ended film")
	}
}

func TestPublicPageCityDefaultCatalogueBoundary(t *testing.T) {
	data := publicPageFixture()
	data.Showtimes = nil
	data.PublicMovies = nil
	data.MovieSources = nil
	for i := 1; i <= 26; i++ {
		id := int64(i)
		title := fmt.Sprintf("Film %02d", i)
		data.PublicMovies = append(data.PublicMovies, PublicMovieRecord{ID: id, IdentityAnchorProvider: ProviderUGC, IdentityAnchorSourceID: fmt.Sprint(i), Title: title, RuntimeMinutes: 100, UpdatedAt: data.GeneratedAt})
		data.MovieSources = append(data.MovieSources, PublicMovieSourceRecord{Provider: ProviderUGC, SourceMovieID: fmt.Sprint(i), SourceSlug: "ugc-film-" + fmt.Sprint(i), PublicMovieID: id, Title: title, RuntimeMinutes: 100})
		showing := testDataset().Showtimes[0]
		showing.ID = fmt.Sprintf("ugc-showing-%d", i)
		showing.ProviderShowingID = fmt.Sprint(i)
		showing.BookingURL = "https://www.ugc.fr/reservationSeances.html?id=" + showing.ProviderShowingID
		showing.Movie.ProviderID = fmt.Sprint(i)
		showing.Movie.Slug = "ugc-film-" + fmt.Sprint(i)
		showing.Movie.PublicMovieID = id
		data.Showtimes = append(data.Showtimes, showing)
	}
	baseline := pageProjection(t, data, pageNow(), nil)
	changed := cloneDataset(data)
	changed.PublicMovies[25].PosterURL = "https://example.test/new.jpg"
	changed.PublicMovies[25].Overview = "Off-page change"
	result := pageProjection(t, changed, pageNow(), nil)
	if result.Fingerprints["/ville/lille/cinemas"] != baseline.Fingerprints["/ville/lille/cinemas"] {
		t.Fatal("unrendered off-page metadata affected city")
	}
	changed.PublicMovies[25].Title = "A first page movie"
	result = pageProjection(t, changed, pageNow(), nil)
	if result.Fingerprints["/ville/lille/cinemas"] == baseline.Fingerprints["/ville/lille/cinemas"] {
		t.Fatal("page-1 membership change ignored")
	}
	changed = cloneDataset(data)
	changed.Showtimes = changed.Showtimes[:25]
	result = pageProjection(t, changed, pageNow(), nil)
	if result.Fingerprints["/ville/lille/cinemas"] == baseline.Fingerprints["/ville/lille/cinemas"] {
		t.Fatal("catalogue total/header count change ignored")
	}
}

func TestPublicPageCatalogueOnlyAndUnavailable(t *testing.T) {
	data := publicPageFixture()
	data.Theaters = nil
	data.Showtimes = nil
	data.Window = Window{}
	data.Provider = ""
	data.Scope = ""
	data.CatalogPublishedAt = data.GeneratedAt
	view := NewSnapshotView(data, SnapshotRevision{EnrichmentVersion: 9})
	projection, err := BuildSitemapProjection(view, pageNow(), nil)
	if err != nil || projection.Data.Cities != nil || projection.Data.MovieTotal != 6 || projection.Data.UpcomingAvailable || len(projection.Fingerprints) != 6 {
		t.Fatalf("catalogue-only %+v %v", projection.Data, err)
	}
	data.UpcomingCompletedAt = data.GeneratedAt
	projection, err = BuildSitemapProjection(NewSnapshotView(data, SnapshotRevision{EnrichmentVersion: 10}), pageNow(), nil)
	if err != nil || !projection.Data.UpcomingAvailable {
		t.Fatal("successful empty upcoming publication unavailable", err)
	}
	if _, err := BuildSitemapProjection(nil, pageNow(), nil); err == nil {
		t.Fatal("unavailable became empty success")
	}
}
