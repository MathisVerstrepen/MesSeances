package schedule

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func summaryTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func summaryService(t *testing.T, source Source, now time.Time) *Service {
	t.Helper()
	service, err := NewService(source, ServiceOptions{DefaultCity: "Lille", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func assertSummaryCount(t *testing.T, item MovieCatalogItem, current, remaining, next7Days int) {
	t.Helper()
	if item.ShowtimeCount != current || item.RemainingShowtimeCount == nil || *item.RemainingShowtimeCount != remaining || item.Next7DaysShowtimeCount == nil || *item.Next7DaysShowtimeCount != next7Days {
		t.Fatalf("item=%+v want current=%d remaining=%d next7Days=%d", item, current, remaining, next7Days)
	}
}

func TestMoviesScreeningSummarySelectedScopeAndPaging(t *testing.T) {
	service := summaryService(t, newTestSource(combinedTestDataset()), testServiceNow())
	for _, tc := range []struct {
		name string
		ids  []string
		want int
	}{
		{"all includes theaters outside default city", nil, 3},
		{"initialized empty selection is all", []string{}, 3},
		{"selected", []string{"ugc-25"}, 1},
		{"deduplicated selection sums cinemas", []string{"ugc-26", "ugc-25", "ugc-26"}, 2},
		{"merged provider slug", []string{"kinepolis-LOM", "ugc-25"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := MovieCatalogQuery{ScreeningSummary: true, TheaterIDs: tc.ids, Sort: MovieCatalogSortShowtimesDesc, PageSize: 1}
			result, err := service.Movies(query)
			if err != nil || len(result.Items) != 1 || result.Items[0].Slug != "tmdb-film-42" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			assertSummaryCount(t, result.Items[0], tc.want, tc.want, tc.want)
			query.ScreeningSummary = false
			ordinary, err := service.Movies(query)
			if err != nil || ordinary.Total != result.Total || ordinary.CatalogRevision != result.CatalogRevision || ordinary.ScreeningWindow != nil {
				t.Fatalf("ordinary=%+v err=%v", ordinary, err)
			}
			result.Items[0].RemainingShowtimeCount = nil
			result.Items[0].Next7DaysShowtimeCount = nil
			if !reflect.DeepEqual(result.Items, ordinary.Items) {
				t.Fatalf("ordinary items changed: %+v != %+v", result.Items, ordinary.Items)
			}
		})
	}
	first, err := service.Movies(MovieCatalogQuery{ScreeningSummary: true, Sort: MovieCatalogSortTitleAsc, PageSize: 1})
	if err != nil || first.Total != 4 || first.Items[0].Title != "Film A" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	for page := 2; page <= 5; page++ {
		result, err := service.Movies(MovieCatalogQuery{ScreeningSummary: true, Sort: MovieCatalogSortTitleAsc, PageSize: 1, Page: page})
		if err != nil || result.Total != first.Total || result.Page != page || !reflect.DeepEqual(result.ScreeningWindow, first.ScreeningWindow) {
			t.Fatalf("page=%d result=%+v err=%v", page, result, err)
		}
		if page == 5 {
			if len(result.Items) != 0 {
				t.Fatal("out-of-range page not empty")
			}
			continue
		}
		if len(result.Items) != 1 || result.Items[0].Title != "Film "+string(rune('A'+page-1)) {
			t.Fatalf("title order changed: %+v", result.Items)
		}
		assertSummaryCount(t, result.Items[0], 1, 1, 1)
	}
}

func TestMoviesScreeningSummaryStrictInstantBoundaries(t *testing.T) {
	now := summaryTime(t, "2026-10-13T12:00:00+02:00")
	for _, tc := range []struct {
		name, start                   string
		current, remaining, next7Days int
	}{
		{"expired grace", "2026-10-13T11:39:59.999999999+02:00", 0, 0, 0},
		{"inclusive grace", "2026-10-13T11:40:00+02:00", 1, 0, 0},
		{"already started", "2026-10-13T11:59:59.999999999+02:00", 1, 0, 0},
		{"exactly as of", "2026-10-13T12:00:00+02:00", 1, 1, 1},
		{"Tuesday 23:59", "2026-10-13T23:59:00+02:00", 1, 1, 1},
		{"last instant", "2026-10-13T23:59:59.999999999+02:00", 1, 1, 1},
		{"Wednesday midnight Tuesday service date", "2026-10-14T00:00:00+02:00", 1, 0, 1},
		{"Wednesday 00:30 Tuesday service date", "2026-10-14T00:30:00+02:00", 1, 0, 1},
		{"future beyond Tuesday", "2026-10-14T20:00:00+02:00", 1, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := testDataset()
			data.Window = Window{From: "2026-10-13", Through: "2026-10-14"}
			for i := range data.Theaters {
				data.Theaters[i].AvailableDates = []string{"2026-10-13", "2026-10-14"}
			}
			data.Showtimes = data.Showtimes[:1]
			showing := &data.Showtimes[0]
			showing.ServiceDate = "2026-10-13"
			showing.StartTime = summaryTime(t, tc.start)
			showing.EndTime = showing.StartTime.Add(100 * time.Minute)
			if tc.name == "future beyond Tuesday" {
				showing.ServiceDate = "2026-10-14"
			}
			if err := ValidateDataset(data, true); err != nil {
				t.Fatal(err)
			}
			service := summaryService(t, newTestSource(data), now)
			result, err := service.Movies(MovieCatalogQuery{ScreeningSummary: true})
			if err != nil || result.ScreeningWindow == nil || result.ScreeningWindow.DayCount != 1 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if tc.current == 0 {
				if result.Total != 0 || len(result.Items) != 0 {
					t.Fatalf("expired screening included: %+v", result)
				}
				return
			}
			if len(result.Items) != 1 {
				t.Fatalf("items=%+v", result.Items)
			}
			assertSummaryCount(t, result.Items[0], tc.current, tc.remaining, tc.next7Days)
		})
	}
}

func TestMoviesScreeningSummaryParisCalendarAndSingleClockRead(t *testing.T) {
	for _, tc := range []struct {
		name, now, from, through, end string
		days                          int
	}{
		{"Wednesday", "2026-10-07T10:00:00Z", "2026-10-07", "2026-10-13", "2026-10-14T00:00:00+02:00", 7},
		{"Thursday", "2026-10-08T10:00:00Z", "2026-10-08", "2026-10-13", "2026-10-14T00:00:00+02:00", 6},
		{"Friday", "2026-10-09T10:00:00Z", "2026-10-09", "2026-10-13", "2026-10-14T00:00:00+02:00", 5},
		{"Saturday", "2026-10-10T10:00:00Z", "2026-10-10", "2026-10-13", "2026-10-14T00:00:00+02:00", 4},
		{"Sunday", "2026-10-11T10:00:00Z", "2026-10-11", "2026-10-13", "2026-10-14T00:00:00+02:00", 3},
		{"Monday", "2026-10-12T10:00:00Z", "2026-10-12", "2026-10-13", "2026-10-14T00:00:00+02:00", 2},
		{"Tuesday last instant", "2026-10-13T23:59:59+02:00", "2026-10-13", "2026-10-13", "2026-10-14T00:00:00+02:00", 1},
		{"Paris Wednesday UTC Tuesday", "2026-10-13T22:00:00Z", "2026-10-14", "2026-10-20", "2026-10-21T00:00:00+02:00", 7},
		{"spring DST", "2026-03-28T12:00:00+01:00", "2026-03-28", "2026-03-31", "2026-04-01T00:00:00+02:00", 4},
		{"fall DST", "2026-10-24T12:00:00+02:00", "2026-10-24", "2026-10-27", "2026-10-28T00:00:00+01:00", 4},
		{"year rollover", "2026-12-30T23:00:00+01:00", "2026-12-30", "2027-01-05", "2027-01-06T00:00:00+01:00", 7},
		{"leap month", "2028-02-28T12:00:00+01:00", "2028-02-28", "2028-02-29", "2028-03-01T00:00:00+01:00", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := summaryTime(t, tc.now)
			calls := 0
			service, err := NewService(newTestSource(testDataset()), ServiceOptions{Now: func() time.Time {
				calls++
				return now
			}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Movies(MovieCatalogQuery{ScreeningSummary: true, Search: "absent"})
			window := result.ScreeningWindow
			if err != nil || calls != 1 || window == nil || window.From != tc.from || window.Through != tc.through || window.DayCount != tc.days || window.Timezone != Timezone || !window.AsOf.Equal(now) || window.AsOf.Equal(result.GeneratedAt) || result.Total != 0 {
				t.Fatalf("result=%+v window=%+v calls=%d err=%v", result, window, calls, err)
			}
			_, end := service.movieScreeningWindow(now)
			if !end.Equal(summaryTime(t, tc.end)) {
				t.Fatalf("exclusive end=%s want=%s", end, tc.end)
			}
		})
	}
}

func TestMoviesScreeningSummaryCanonicalInventoryAndOmission(t *testing.T) {
	data := combinedTestDataset()
	data.Showtimes = []ShowtimeRecord{data.Showtimes[0], data.Showtimes[1], data.Showtimes[5]}
	for i := range data.Showtimes {
		data.Showtimes[i].Movie.PublicMovieID = 1
	}
	data.PublicMovies = []PublicMovieRecord{
		{ID: 1, IdentityAnchorTMDBID: 42, TMDBID: 42, Title: "Canonical film", RuntimeMinutes: 100, UpdatedAt: data.GeneratedAt},
		{ID: 2, RedirectToID: 1, IdentityAnchorTMDBID: 43, TMDBID: 43, Title: "Merged film", UpdatedAt: data.GeneratedAt},
		{ID: 3, IdentityAnchorTMDBID: 44, TMDBID: 44, Title: "Imported no sessions", UpdatedAt: data.GeneratedAt},
	}
	data.MovieSources = []PublicMovieSourceRecord{
		{Provider: ProviderUGC, SourceMovieID: "200", SourceSlug: "ugc-film-200", PublicMovieID: 1, Title: "Film A", RuntimeMinutes: 100},
		{Provider: ProviderKinepolis, SourceMovieID: "HO200", SourceSlug: "kinepolis-film-HO200", PublicMovieID: 1, Title: "Film A", RuntimeMinutes: 100},
	}
	data.MovieAliases = []MovieSlugAliasRecord{{Slug: "tmdb-film-42", PublicMovieID: 1, Kind: "tmdb"}}
	if err := ValidateDataset(data, true); err != nil {
		t.Fatal(err)
	}
	service := summaryService(t, newTestSource(data), testServiceNow())
	result, err := service.Movies(MovieCatalogQuery{ScreeningSummary: true, IncludeEnded: true, Sort: MovieCatalogSortTitleAsc})
	if err != nil || result.Total != 2 || len(result.Items) != 2 || result.Items[0].Slug != "film-1" || result.Items[1].Slug != "film-3" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	assertSummaryCount(t, result.Items[0], 3, 3, 3)
	assertSummaryCount(t, result.Items[1], 0, 0, 0)
	encoded, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(encoded), `"remaining_showtime_count":0`) || !strings.Contains(string(encoded), `"next_7_days_showtime_count":0`) {
		t.Fatalf("zero omitted: %s err=%v", encoded, err)
	}
	current, err := service.Movies(MovieCatalogQuery{ScreeningSummary: true})
	if err != nil || current.Total != 1 {
		t.Fatalf("imported movie invented sessions: %+v %v", current, err)
	}
	ordinary, err := service.Movies(MovieCatalogQuery{IncludeEnded: true})
	if err != nil {
		t.Fatal(err)
	}
	city, err := service.City("lille")
	if err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"film-1", "film-2", "tmdb-film-42"} {
		detail, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: alias, Date: "2026-08-15"})
		if err != nil || detail.Movie.Slug != "film-1" {
			t.Fatalf("alias detail=%+v err=%v", detail, err)
		}
		for _, value := range []any{ordinary, city, detail} {
			encoded, err := json.Marshal(value)
			if err != nil || strings.Contains(string(encoded), "remaining_showtime_count") || strings.Contains(string(encoded), "next_7_days_showtime_count") || strings.Contains(string(encoded), "screening_window") {
				t.Fatalf("summary leaked: %s err=%v", encoded, err)
			}
		}
	}
}

func TestMoviesScreeningSummaryValidationAndAvailability(t *testing.T) {
	service := testService(t)
	date, dateTo := "today", "2026-08-18"
	for _, query := range []MovieCatalogQuery{
		{ScreeningSummary: true, Date: &date},
		{ScreeningSummary: true, DateTo: &dateTo},
		{ScreeningSummary: true, Date: &date, DateTo: &dateTo},
		{ScreeningSummary: true, TheaterIDs: []string{"unknown"}},
		{ScreeningSummary: true, PageSize: 101},
	} {
		var validation *ValidationError
		if _, err := service.Movies(query); !errors.As(err, &validation) {
			t.Fatalf("query=%+v err=%v", query, err)
		}
	}
	falseValue := false
	empty, err := service.Movies(MovieCatalogQuery{ScreeningSummary: true, CurrentlyScreened: &falseValue})
	if err != nil || empty.Total != 0 || empty.ScreeningWindow == nil {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
	for _, view := range []*SnapshotView{nil, NewSnapshotView(importedCatalogFixture(), SnapshotRevision{EnrichmentVersion: 1})} {
		unavailable := summaryService(t, testSource{view: view}, testServiceNow())
		if _, err := unavailable.Movies(MovieCatalogQuery{ScreeningSummary: true}); !errors.Is(err, ErrNoCompleteSnapshot) {
			t.Fatalf("unavailable summary err=%v", err)
		}
		if view != nil {
			ordinary, err := unavailable.Movies(MovieCatalogQuery{IncludeEnded: true})
			if err != nil || ordinary.Total != 1 || ordinary.ScreeningWindow != nil || ordinary.Items[0].RemainingShowtimeCount != nil || ordinary.Items[0].Next7DaysShowtimeCount != nil {
				t.Fatalf("ordinary import=%+v err=%v", ordinary, err)
			}
		}
	}
}

func TestMoviesScreeningSummaryPreservesSearchVariantCounts(t *testing.T) {
	data := testDataset()
	data.Showtimes = data.Showtimes[:2]
	for i := range data.Showtimes {
		data.Showtimes[i].Movie.Enrichment = &MovieEnrichment{TMDBID: 42}
	}
	data.Showtimes[1].Movie.Title = "Other title"
	service := summaryService(t, newTestSource(data), testServiceNow())
	for _, query := range []string{"other title", " TITLE - othér "} {
		result, err := service.Movies(MovieCatalogQuery{ScreeningSummary: true, Search: query})
		if err != nil || result.Total != 1 || result.Items[0].Title != "Other title" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		assertSummaryCount(t, result.Items[0], 1, 1, 1)
	}
}

func TestMoviesScreeningSummaryRollingSevenDayBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, now, start string
		next7Days        int
	}{
		{"just before cutoff", "2026-10-13T12:00:00+02:00", "2026-10-20T11:59:59.999999999+02:00", 1},
		{"exact cutoff", "2026-10-13T12:00:00+02:00", "2026-10-20T12:00:00+02:00", 0},
		{"after cutoff", "2026-10-13T12:00:00+02:00", "2026-10-20T12:00:00.000000001+02:00", 0},
		{"two months away", "2026-10-13T12:00:00+02:00", "2026-12-13T12:00:00+01:00", 0},
		{"spring DST before 168 hours", "2026-03-28T12:00:00+01:00", "2026-04-04T12:59:59.999999999+02:00", 1},
		{"spring DST exact 168 hours", "2026-03-28T12:00:00+01:00", "2026-04-04T13:00:00+02:00", 0},
		{"fall DST before 168 hours", "2026-10-24T12:00:00+02:00", "2026-10-31T10:59:59.999999999+01:00", 1},
		{"fall DST exact 168 hours", "2026-10-24T12:00:00+02:00", "2026-10-31T11:00:00+01:00", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := summaryTime(t, tc.now)
			data := testDataset()
			data.Showtimes = data.Showtimes[:1]
			showing := &data.Showtimes[0]
			showing.StartTime = summaryTime(t, tc.start)
			showing.EndTime = showing.StartTime.Add(100 * time.Minute)
			showing.ServiceDate = showing.StartTime.Format(dateLayout)
			data.Window = Window{From: now.Format(dateLayout), Through: showing.ServiceDate}
			for i := range data.Theaters {
				data.Theaters[i].AvailableDates = []string{showing.ServiceDate}
			}
			if err := ValidateDataset(data, true); err != nil {
				t.Fatal(err)
			}
			calls := 0
			service, err := NewService(newTestSource(data), ServiceOptions{Now: func() time.Time {
				calls++
				return now.Add(time.Duration(calls-1) * time.Hour)
			}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Movies(MovieCatalogQuery{ScreeningSummary: true})
			if err != nil || result.Total != 1 || len(result.Items) != 1 || calls != 1 || result.ScreeningWindow == nil || !result.ScreeningWindow.AsOf.Equal(now) {
				t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
			}
			assertSummaryCount(t, result.Items[0], 1, 0, tc.next7Days)
		})
	}
}

func TestMoviesScreeningSummarySelectedSevenDayHorizon(t *testing.T) {
	data := testDataset()
	data.Showtimes = data.Showtimes[:2]
	data.Window = Window{From: "2026-10-13", Through: "2026-12-13"}
	for i := range data.Theaters {
		data.Theaters[i].AvailableDates = []string{"2026-10-14", "2026-12-13"}
	}
	for i, start := range []string{"2026-12-13T12:00:00+01:00", "2026-10-14T12:00:00+02:00"} {
		showing := &data.Showtimes[i]
		showing.StartTime = summaryTime(t, start)
		showing.EndTime = showing.StartTime.Add(100 * time.Minute)
		showing.ServiceDate = showing.StartTime.Format(dateLayout)
		showing.Movie.Enrichment = &MovieEnrichment{TMDBID: 42}
	}
	if err := ValidateDataset(data, true); err != nil {
		t.Fatal(err)
	}
	service := summaryService(t, newTestSource(data), summaryTime(t, "2026-10-13T12:00:00+02:00"))
	for _, tc := range []struct {
		name               string
		ids                []string
		current, next7Days int
	}{
		{"all", nil, 2, 1},
		{"empty selects all", []string{}, 2, 1},
		{"selected distant only", []string{"ugc-25"}, 1, 0},
		{"selected after Tuesday within seven days", []string{"ugc-26"}, 1, 1},
		{"deduplicated selected cinemas", []string{"ugc-25", "ugc-26", "ugc-26"}, 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := MovieCatalogQuery{ScreeningSummary: true, TheaterIDs: tc.ids}
			result, err := service.Movies(query)
			if err != nil || result.Total != 1 || len(result.Items) != 1 || result.Items[0].Slug != "tmdb-film-42" || result.ScreeningWindow.DayCount != 1 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			assertSummaryCount(t, result.Items[0], tc.current, 0, tc.next7Days)
			query.ScreeningSummary = false
			ordinary, err := service.Movies(query)
			result.Items[0].RemainingShowtimeCount = nil
			result.Items[0].Next7DaysShowtimeCount = nil
			if err != nil || ordinary.Total != result.Total || !reflect.DeepEqual(ordinary.Items, result.Items) {
				t.Fatalf("ordinary=%+v err=%v", ordinary, err)
			}
		})
	}
}
