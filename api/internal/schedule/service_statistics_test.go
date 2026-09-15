package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func statisticsDataset() Dataset {
	data := combinedTestDataset()
	data.PublicMovies = []PublicMovieRecord{
		{ID: 1, Title: "Même titre", RuntimeMinutes: 89, Genres: []string{"Drame", " drame ", "Action", " "}},
		{ID: 2, Title: "Même titre", RuntimeMinutes: 90, Genres: []string{"drame"}},
		{ID: 3, Title: "Minuit", RuntimeMinutes: 120, Genres: []string{"Comédie"}},
		{ID: 4, Title: "Long", RuntimeMinutes: 121, Genres: []string{"Drame"}},
		{ID: 5, Title: "Inconnu", Genres: []string{" "}},
		{ID: 6, RedirectToID: 1, Title: "Ancien titre", RuntimeMinutes: 500},
		{ID: 7, Title: "Sans séance", RuntimeMinutes: 100, Genres: []string{"Catalogue"}},
	}
	for i, id := range []int64{1, 1, 2, 3, 4, 6} {
		data.Showtimes[i].Movie.PublicMovieID = id
	}
	simultaneous := data.Showtimes[0]
	simultaneous.ID, simultaneous.ProviderShowingID = "ugc-showing-105", "105"
	unknown := data.Showtimes[0]
	unknown.ID, unknown.ProviderShowingID, unknown.Movie.PublicMovieID = "ugc-showing-106", "106", 5
	unknown.Language, unknown.Format = "", ""
	data.Showtimes = append(data.Showtimes, simultaneous, unknown)
	data.Theaters = append(data.Theaters, TheaterRecord{Provider: ProviderPathe, ID: "pathe-empty", Slug: "pathe-empty", Name: "Sans séances", City: "Paris", AcceptedPasses: []string{"CINÉ PASS", "CINÉ PASS"}})
	return data
}

func statisticsService(t *testing.T, data Dataset, now time.Time) *Service {
	t.Helper()
	service, err := NewService(newTestSource(data), ServiceOptions{DefaultCity: "Lille", CityAliases: map[string][]string{"Lille": {"Lille", "Villeneuve d'Ascq"}}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func getStatistics(t *testing.T, service *Service, query StatisticsQuery) Statistics {
	t.Helper()
	result, err := service.Statistics(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	assertStatisticsSums(t, result)
	return result
}

func assertStatisticsSums(t *testing.T, result Statistics) {
	t.Helper()
	sums := map[string]int{}
	for _, cell := range result.Heatmap {
		sums["heatmap"] += cell.ShowtimeCount
	}
	for _, bucket := range result.Versions {
		sums["versions"] += bucket.Count
	}
	for _, bucket := range result.Formats {
		sums["formats"] += bucket.Count
	}
	for _, row := range result.Local.Cities {
		sums["cities"] += row.ShowtimeCount
	}
	for _, row := range result.Local.Theaters {
		sums["theaters"] += row.ShowtimeCount
	}
	sums["concentration"] = result.Concentration.TopShowtimeCount + result.Concentration.OtherShowtimeCount
	for name, sum := range sums {
		if sum != result.Totals.Showtimes {
			t.Errorf("%s sum=%d totals=%+v", name, sum, result.Totals)
		}
	}
	runtimeCount := 0
	for _, bucket := range result.Runtimes {
		runtimeCount += bucket.Count
	}
	if runtimeCount != result.Totals.Movies {
		t.Errorf("runtime sum=%d movies=%d", runtimeCount, result.Totals.Movies)
	}
	if len(result.Heatmap) != 168 {
		t.Fatalf("heatmap length=%d", len(result.Heatmap))
	}
	for i, cell := range result.Heatmap {
		if cell.Weekday != i/24+1 || cell.Hour != i%24 {
			t.Fatalf("cell %d=%+v", i, cell)
		}
	}
}

func TestStatisticsAggregatesPublicIdentityAndUnknowns(t *testing.T) {
	data := statisticsDataset()
	service := statisticsService(t, data, testServiceNow())
	result := getStatistics(t, service, StatisticsQuery{})
	if result.Totals != (StatisticsTotals{Showtimes: 8, Movies: 5, Theaters: 4, Cities: 4}) {
		t.Fatalf("totals=%+v", result.Totals)
	}
	if result.Range != (Window{From: "2026-08-15", Through: "2026-08-21"}) || result.Timezone != Timezone || !result.GeneratedAt.Equal(data.GeneratedAt) {
		t.Fatalf("metadata=%+v", result)
	}
	wantTop := StatisticsMovieRank{Slug: "film-1", Title: "Même titre", ShowtimeCount: 4, TheaterCount: 3}
	if result.TopMovies.ByShowtimes[0] != wantTop || result.TopMovies.ByTheaters[0] != wantTop {
		t.Fatalf("top=%+v", result.TopMovies)
	}
	if result.Heatmap[5*24].ShowtimeCount != 1 || result.Heatmap[6*24].ShowtimeCount != 0 {
		t.Fatal("after-midnight session lost its Saturday service day")
	}
	for i, count := range []int{1, 2, 1, 1} {
		if result.Runtimes[i].Count != count {
			t.Fatalf("runtimes=%+v", result.Runtimes)
		}
	}
	wantGenres := []StatisticsCountBucket{{Value: "drame", Label: "Drame", Count: 3}, {Value: "action", Label: "Action", Count: 1}, {Value: "comédie", Label: "Comédie", Count: 1}, {Value: "unknown", Label: "Non renseigné", Count: 1}}
	if !reflect.DeepEqual(result.Genres, wantGenres) {
		t.Fatalf("genres=%+v", result.Genres)
	}
	if result.Concentration != (StatisticsConcentration{TopMovieCount: 5, TopShowtimeCount: 8}) {
		t.Fatalf("concentration=%+v", result.Concentration)
	}
	if len(result.Options.Theaters) != 5 || len(result.Options.Cities) != 5 || !slices.Contains(result.Options.Chains, ProviderPathe) || !slices.Contains(result.Options.Passes, "CINÉ PASS") {
		t.Fatalf("inventory=%+v", result.Options)
	}
	if len(result.Options.Genres) != 4 || !slices.Contains(result.Options.Languages, "unknown") || !slices.Contains(result.Options.Formats, "unknown") {
		t.Fatalf("observed options=%+v", result.Options)
	}
	// Source identities deduplicate, but the distinct simultaneous ID above counts.
	data.Showtimes = append(data.Showtimes, data.Showtimes[0])
	duplicate := getStatistics(t, statisticsService(t, data, testServiceNow()), StatisticsQuery{})
	if !reflect.DeepEqual(result, duplicate) {
		t.Fatal("duplicate source identity changed result")
	}
}

func TestStatisticsFiltersIntersectWithoutChangingOptions(t *testing.T) {
	service := statisticsService(t, statisticsDataset(), testServiceNow())
	options := getStatistics(t, service, StatisticsQuery{}).Options
	tests := []struct {
		name  string
		query StatisticsQuery
		count int
	}{
		{"city exact no regional alias", StatisticsQuery{City: []string{"lille"}}, 4},
		{"theater", StatisticsQuery{Theater: []string{"ugc-26"}}, 2},
		{"chain", StatisticsQuery{Chain: "kinepolis"}, 1},
		{"VF exact", StatisticsQuery{Language: "VF"}, 2},
		{"VF SME exact", StatisticsQuery{Language: "VF_SME"}, 1},
		{"format", StatisticsQuery{Format: "IMAX"}, 1},
		{"genre normalized", StatisticsQuery{Genre: " DRAME "}, 6},
		{"pass", StatisticsQuery{Pass: "UGC_ILLIMITE"}, 7},
		{"unknown language", StatisticsQuery{Language: "unknown"}, 1},
		{"unknown format", StatisticsQuery{Format: "unknown"}, 1},
		{"unknown genre", StatisticsQuery{Genre: "unknown"}, 1},
		{"intersection", StatisticsQuery{Date: "2026-08-15", DateTo: "2026-08-15", City: []string{" lille "}, Theater: []string{" ugc-25 "}, Chain: " ugc ", Language: "VOSTFR", Format: "2D", Genre: "action", Pass: "UGC_ILLIMITE"}, 2},
		{"contradictory", StatisticsQuery{City: []string{"lille"}, Theater: []string{"ugc-26"}}, 0},
		{"absent city", StatisticsQuery{City: []string{"removed"}}, 0},
		{"display name not slug", StatisticsQuery{City: []string{"Lille"}}, 0},
		{"absent theater", StatisticsQuery{Theater: []string{"removed"}}, 0},
		{"absent genre", StatisticsQuery{Genre: "removed"}, 0},
		{"absent pass", StatisticsQuery{Pass: "removed"}, 0},
		{"cinema without sessions", StatisticsQuery{Theater: []string{"pathe-empty"}}, 0},
		{"future disjoint", StatisticsQuery{Date: "2026-09-01"}, 0},
		{"city union", StatisticsQuery{City: []string{"lille", "lyon"}}, 5},
		{"theater union", StatisticsQuery{Theater: []string{"ugc-25", "ugc-26"}}, 6},
		{"both unions intersect", StatisticsQuery{City: []string{"lille", "lyon"}, Theater: []string{"ugc-25", "ugc-26"}}, 4},
		{"unions intersect scalar", StatisticsQuery{City: []string{"lille", "lyon"}, Theater: []string{"ugc-25", "ugc-99"}, Language: "VF"}, 1},
		{"duplicates", StatisticsQuery{City: []string{"lille", " lille "}, Theater: []string{"ugc-25", " ugc-25 "}}, 4},
		{"known and unknown cities", StatisticsQuery{City: []string{"removed", "lille"}}, 4},
		{"known and unknown theaters", StatisticsQuery{Theater: []string{"removed", "ugc-26"}}, 2},
		{"unknown cities only", StatisticsQuery{City: []string{"removed", "gone"}}, 0},
		{"unknown theaters only", StatisticsQuery{Theater: []string{"removed", "gone"}}, 0},
		{"contradictory lists", StatisticsQuery{City: []string{"lille", "lyon"}, Theater: []string{"ugc-26", "kinepolis-LOM"}}, 0},
		{"nil lists", StatisticsQuery{City: nil, Theater: nil}, 8},
		{"empty lists", StatisticsQuery{City: []string{}, Theater: []string{}}, 8},
		{"comma city is opaque", StatisticsQuery{City: []string{"lille,lyon"}}, 0},
		{"comma theater is opaque", StatisticsQuery{Theater: []string{"ugc-25,ugc-26"}}, 0},
		{"theater case sensitive", StatisticsQuery{Theater: []string{"UGC-25"}}, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := getStatistics(t, service, test.query)
			if result.Totals.Showtimes != test.count {
				t.Errorf("showtimes=%d want=%d", result.Totals.Showtimes, test.count)
			}
			if !reflect.DeepEqual(result.Options, options) {
				t.Fatal("filters changed options")
			}
		})
	}
}

func TestStatisticsSelectionOrderAndImmutability(t *testing.T) {
	service := statisticsService(t, statisticsDataset(), testServiceNow())
	query := StatisticsQuery{City: []string{" lyon ", "lille", "lyon", "removed"}, Theater: []string{" ugc-99 ", "ugc-25", "ugc-26", "ugc-25", "gone"}}
	before := query
	before.City, before.Theater = slices.Clone(query.City), slices.Clone(query.Theater)
	result := getStatistics(t, service, query)
	if result.Totals != (StatisticsTotals{Showtimes: 5, Movies: 4, Theaters: 2, Cities: 2}) {
		t.Fatalf("totals=%+v", result.Totals)
	}
	if !reflect.DeepEqual(query, before) {
		t.Fatal("service mutated caller selections")
	}
	normalizedQuery, _, err := service.statisticsQuery(query, testServiceNow())
	if err != nil || !slices.Equal(normalizedQuery.City, []string{"lyon", "lille", "removed"}) || !slices.Equal(normalizedQuery.Theater, []string{"ugc-99", "ugc-25", "ugc-26", "gone"}) {
		t.Fatalf("normalized=%+v error=%v", normalizedQuery, err)
	}
	normalizedQuery.City[0], normalizedQuery.Theater[0] = "changed", "changed"
	if !reflect.DeepEqual(query, before) {
		t.Fatal("normalized selections alias caller slices")
	}
	slices.Reverse(query.City)
	slices.Reverse(query.Theater)
	if again := getStatistics(t, service, query); !reflect.DeepEqual(result, again) {
		t.Fatal("selection order changed aggregates or options")
	}
	union := getStatistics(t, service, StatisticsQuery{Theater: []string{"ugc-25", "ugc-26"}})
	if union.Totals != (StatisticsTotals{Showtimes: 6, Movies: 4, Theaters: 2, Cities: 2}) {
		t.Fatalf("shared movies counted more than once: %+v", union.Totals)
	}
}

func TestStatisticsDirectSelectionBounds(t *testing.T) {
	service := statisticsService(t, statisticsDataset(), testServiceNow())
	for _, dimension := range []string{"city", "theater"} {
		t.Run(dimension, func(t *testing.T) {
			known := "lille"
			if dimension == "theater" {
				known = "ugc-25"
			}
			for _, test := range []struct {
				name    string
				values  []string
				invalid bool
			}{
				{"50 duplicates", slices.Repeat([]string{known}, 50), false},
				{"51 duplicates", slices.Repeat([]string{known}, 51), true},
				{"empty member", []string{known, ""}, true},
				{"whitespace member", []string{known, " \t\n"}, true},
				{"200 bytes", []string{strings.Repeat("x", 200)}, false},
				{"201 bytes", []string{strings.Repeat("x", 201)}, true},
				{"200 UTF8 bytes", []string{strings.Repeat("é", 100)}, false},
				{"201 UTF8 bytes", []string{strings.Repeat("é", 100) + "x"}, true},
				{"200 padded bytes", []string{known + strings.Repeat(" ", 200-len(known))}, false},
				{"201 padded bytes", []string{known + strings.Repeat(" ", 201-len(known))}, true},
				{"invalid after duplicate", []string{known, known, ""}, true},
			} {
				t.Run(test.name, func(t *testing.T) {
					query := StatisticsQuery{}
					if dimension == "city" {
						query.City = test.values
					} else {
						query.Theater = test.values
					}
					before := slices.Clone(test.values)
					result, err := service.Statistics(t.Context(), query)
					var validation *ValidationError
					if test.invalid {
						if !errors.As(err, &validation) {
							t.Fatalf("error=%v", err)
						}
					} else {
						if err != nil {
							t.Fatal(err)
						}
						assertStatisticsSums(t, result)
					}
					if !slices.Equal(test.values, before) {
						t.Fatal("validation mutated caller slice")
					}
				})
			}
		})
	}
	// Limits apply independently to both dimensions, including direct callers.
	cities, theaters := make([]string, 50), make([]string, 50)
	for i := range cities {
		cities[i], theaters[i] = fmt.Sprintf("city-%d", i), fmt.Sprintf("theater-%d", i)
	}
	getStatistics(t, service, StatisticsQuery{City: cities, Theater: theaters})
	for _, query := range []StatisticsQuery{{City: append(slices.Clone(cities), "extra")}, {Theater: append(slices.Clone(theaters), "extra")}} {
		var validation *ValidationError
		if _, err := service.Statistics(t.Context(), query); !errors.As(err, &validation) {
			t.Fatalf("accepted 51 unique values: %v", err)
		}
	}
}

func TestStatisticsDatesUseParisCalendar(t *testing.T) {
	tests := []struct {
		name, now string
		query     StatisticsQuery
		window    Window
		invalid   bool
	}{
		{"Paris today differs UTC", "2026-08-14T23:30:00Z", StatisticsQuery{}, Window{"2026-08-15", "2026-08-21"}, false},
		{"one explicit day", "2026-08-15T08:00:00Z", StatisticsQuery{Date: "2026-08-16"}, Window{"2026-08-16", "2026-08-16"}, false},
		{"31 spring days", "2026-03-01T08:00:00Z", StatisticsQuery{Date: "2026-03-01", DateTo: "2026-03-31"}, Window{"2026-03-01", "2026-03-31"}, false},
		{"32 spring days", "2026-03-01T08:00:00Z", StatisticsQuery{Date: "2026-03-01", DateTo: "2026-04-01"}, Window{}, true},
		{"31 autumn days", "2026-10-01T08:00:00Z", StatisticsQuery{Date: "2026-10-01", DateTo: "2026-10-31"}, Window{"2026-10-01", "2026-10-31"}, false},
		{"32 autumn days", "2026-10-01T08:00:00Z", StatisticsQuery{Date: "2026-10-01", DateTo: "2026-11-01"}, Window{}, true},
		{"leap day", "2028-02-28T08:00:00Z", StatisticsQuery{Date: "2028-02-29"}, Window{"2028-02-29", "2028-02-29"}, false},
		{"not leap day", "2026-02-28T08:00:00Z", StatisticsQuery{Date: "2026-02-29"}, Window{}, true},
		{"past Paris date", "2026-08-14T23:30:00Z", StatisticsQuery{Date: "2026-08-14"}, Window{}, true},
		{"end only", "2026-08-15T08:00:00Z", StatisticsQuery{DateTo: "2026-08-16"}, Window{}, true},
		{"reversed", "2026-08-15T08:00:00Z", StatisticsQuery{Date: "2026-08-17", DateTo: "2026-08-16"}, Window{}, true},
		{"not padded", "2026-08-15T08:00:00Z", StatisticsQuery{Date: "2026-8-16"}, Window{}, true},
		{"whitespace date", "2026-08-15T08:00:00Z", StatisticsQuery{Date: " 2026-08-16"}, Window{}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, test.now)
			if err != nil {
				t.Fatal(err)
			}
			result, err := statisticsService(t, testDataset(), now).Statistics(t.Context(), test.query)
			var validation *ValidationError
			if test.invalid {
				if !errors.As(err, &validation) {
					t.Fatalf("error=%v", err)
				}
				return
			}
			if err != nil || result.Range != test.window {
				t.Fatalf("range=%+v error=%v", result.Range, err)
			}
		})
	}
}

func TestStatisticsCoverageEmptyAndUnavailable(t *testing.T) {
	data := testDataset()
	service := statisticsService(t, data, testServiceNow())
	result := getStatistics(t, service, StatisticsQuery{})
	if result.Coverage.Stale || result.Coverage.Completeness != "unknown" || result.Coverage.Intersection == nil || *result.Coverage.Intersection != data.Window {
		t.Fatalf("coverage=%+v", result.Coverage)
	}
	future := getStatistics(t, service, StatisticsQuery{Date: "2026-10-01"})
	if future.Coverage.Intersection != nil || future.Totals != (StatisticsTotals{}) {
		t.Fatalf("future=%+v", future)
	}
	stale := getStatistics(t, statisticsService(t, data, testServiceNow().AddDate(0, 0, 2)), StatisticsQuery{})
	if !stale.Coverage.Stale || stale.Range.From != "2026-08-17" || stale.Range.Through != "2026-08-23" {
		t.Fatalf("stale=%+v", stale)
	}
	data.Showtimes = nil
	empty := getStatistics(t, statisticsService(t, data, testServiceNow()), StatisticsQuery{})
	if empty.Totals != (StatisticsTotals{}) || empty.Concentration != (StatisticsConcentration{}) || len(empty.Options.Theaters) != 3 {
		t.Fatalf("empty=%+v", empty)
	}
	encoded, err := json.Marshal(empty)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	assertStatisticsNoNullArrays(t, object, "")
	for _, view := range []*SnapshotView{nil, NewSnapshotView(data, SnapshotRevision{})} {
		unavailable, err := NewService(testSource{view: view}, ServiceOptions{Now: testServiceNow})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := unavailable.Statistics(t.Context(), StatisticsQuery{}); !errors.Is(err, ErrNoCompleteSnapshot) {
			t.Fatalf("unavailable error=%v", err)
		}
	}
}

func assertStatisticsNoNullArrays(t *testing.T, value any, path string) {
	t.Helper()
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if child == nil && key != "intersection" {
				t.Errorf("unexpected null at %s.%s", path, key)
			}
			assertStatisticsNoNullArrays(t, child, path+"."+key)
		}
	case []any:
		for _, child := range value {
			assertStatisticsNoNullArrays(t, child, path+"[]")
		}
	}
}

func TestStatisticsConcentrationAndDeterministicTies(t *testing.T) {
	data := testDataset()
	base := data.Showtimes[0]
	data.Showtimes = nil
	for i := 12; i >= 1; i-- {
		showing := base
		showing.ID = fmt.Sprintf("ugc-showing-%d", i)
		showing.Movie.PublicMovieID = int64(i)
		data.PublicMovies = append(data.PublicMovies, PublicMovieRecord{ID: int64(i), Title: "Titre", RuntimeMinutes: 100, Genres: []string{"drame", "Drame"}})
		data.Showtimes = append(data.Showtimes, showing)
	}
	result := getStatistics(t, statisticsService(t, data, testServiceNow()), StatisticsQuery{})
	if result.Concentration != (StatisticsConcentration{TopMovieCount: 10, TopShowtimeCount: 10, OtherShowtimeCount: 2}) || len(result.TopMovies.ByTheaters) != 10 {
		t.Fatalf("concentration=%+v", result.Concentration)
	}
	if result.TopMovies.ByShowtimes[0].Slug != "film-1" || result.TopMovies.ByShowtimes[1].Slug != "film-10" {
		t.Fatalf("ties=%+v", result.TopMovies)
	}
	slices.Reverse(data.Showtimes)
	slices.Reverse(data.Theaters)
	slices.Reverse(data.PublicMovies)
	again := getStatistics(t, statisticsService(t, data, testServiceNow()), StatisticsQuery{})
	if !reflect.DeepEqual(result, again) {
		t.Fatal("input order changed deterministic result")
	}
}

func TestStatisticsLocalRanksByShowtimesThenMovies(t *testing.T) {
	result := Statistics{}
	cities, theaters := make(map[string]*statisticsLocalCount), make(map[string]*statisticsLocalCount)
	inventory := make(map[string]StatisticsTheaterOption)
	// Input opposes the desired order, including normalized-name and ID ties.
	rows := []struct {
		id, name          string
		showtimes, movies int
	}{
		{"many-movies", "A", 3, 3},
		{"name-last", "Z", 4, 2},
		{"tie-b", "\u2003ALPHA\u00a0", 4, 2},
		{"tie-a", "alpha", 4, 2},
		{"secondary", "Z", 4, 3},
		{"most-showtimes", "Z", 5, 1},
	}
	for _, row := range rows {
		count := &statisticsLocalCount{showtimes: row.showtimes, movies: make(map[string]bool), theaters: map[string]bool{row.id: true}}
		for i := range row.movies {
			count.movies[fmt.Sprint(i)] = true
		}
		cities[row.id], theaters[row.id] = count, count
		result.Options.Cities = append(result.Options.Cities, City{Slug: row.id, Name: row.name})
		inventory[row.id] = StatisticsTheaterOption{ID: row.id, Name: row.name, CitySlug: row.id}
	}
	statisticsFinishRanks(&result, nil, cities, theaters, inventory)
	want := []string{"most-showtimes", "secondary", "tie-a", "tie-b", "name-last", "many-movies"}
	if len(result.Local.Cities) != len(want) || len(result.Local.Theaters) != len(want) {
		t.Fatalf("local rows=%+v", result.Local)
	}
	for i, id := range want {
		city, theater := result.Local.Cities[i], result.Local.Theaters[i]
		if city.Slug != id || theater.ID != id {
			t.Fatalf("position %d: city=%+v theater=%+v want=%s", i, city, theater, id)
		}
		count := cities[id]
		if city.ShowtimeCount != count.showtimes || theater.ShowtimeCount != count.showtimes || city.MovieCount != len(count.movies) || theater.MovieCount != len(count.movies) || city.TheaterCount != 1 {
			t.Fatalf("counts changed: city=%+v theater=%+v", city, theater)
		}
	}
	if result.Totals.Cities != len(rows) || result.Totals.Theaters != len(rows) {
		t.Fatalf("totals=%+v", result.Totals)
	}
}

func TestStatisticsMovieRankingPrioritiesUnchanged(t *testing.T) {
	rows := []StatisticsMovieRank{
		{Slug: "film-1", Title: "A", ShowtimeCount: 3, TheaterCount: 3},
		{Slug: "film-2", Title: "Z", ShowtimeCount: 5, TheaterCount: 1},
		{Slug: "film-3", Title: "Z", ShowtimeCount: 5, TheaterCount: 2},
		{Slug: "film-4", Title: "Z", ShowtimeCount: 4, TheaterCount: 3},
	}
	for _, tc := range []struct {
		name       string
		byTheaters bool
		want       []string
	}{
		{"showtimes", false, []string{"film-3", "film-2", "film-4", "film-1"}},
		{"theaters", true, []string{"film-4", "film-1", "film-3", "film-2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			movies := slices.Clone(rows)
			statisticsSortMovies(movies, tc.byTheaters)
			for i, slug := range tc.want {
				if movies[i].Slug != slug {
					t.Fatalf("position %d: got=%s want=%s", i, movies[i].Slug, slug)
				}
			}
		})
	}
}

func TestStatisticsDSTRepeatedHourAndServiceWeekday(t *testing.T) {
	data := testDataset()
	data.Window = Window{"2026-10-24", "2026-10-25"}
	data.Showtimes = data.Showtimes[:2]
	for i, timestamp := range []string{"2026-10-25T00:30:00Z", "2026-10-25T01:30:00Z"} {
		start, err := time.Parse(time.RFC3339, timestamp)
		if err != nil {
			t.Fatal(err)
		}
		data.Showtimes[i].ServiceDate, data.Showtimes[i].StartTime = "2026-10-24", start
	}
	now := time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)
	result := getStatistics(t, statisticsService(t, data, now), StatisticsQuery{})
	if result.Heatmap[5*24+2].ShowtimeCount != 2 {
		t.Fatalf("repeated hour=%+v", result.Heatmap[5*24+2])
	}
}

func TestStatisticsIncludesStartedSessionsAndSumsWeeks(t *testing.T) {
	data := testDataset()
	// At 23:00 Paris, the same day's earlier sessions still count.
	now := time.Date(2026, 8, 15, 21, 0, 0, 0, time.UTC)
	result := getStatistics(t, statisticsService(t, data, now), StatisticsQuery{Date: "2026-08-15"})
	if result.Totals.Showtimes != 5 {
		t.Fatalf("already-started sessions excluded: %+v", result.Totals)
	}
	showing := data.Showtimes[0]
	showing.ID, showing.ProviderShowingID = "ugc-showing-999", "999"
	showing.ServiceDate = "2026-08-22"
	showing.StartTime = showing.StartTime.AddDate(0, 0, 7)
	data.Showtimes = append(data.Showtimes, showing)
	data.Window.Through = "2026-08-22"
	result = getStatistics(t, statisticsService(t, data, now), StatisticsQuery{Date: "2026-08-15", DateTo: "2026-08-22"})
	if result.Totals.Showtimes != 6 || result.Heatmap[5*24+12].ShowtimeCount != 3 {
		t.Fatalf("multiweek totals=%+v Saturday noon=%+v", result.Totals, result.Heatmap[5*24+12])
	}
}

func TestStatisticsExactStoredBucketsAndRuntimeBoundaries(t *testing.T) {
	for _, language := range []Language{LanguageVF, LanguageVOSTFR, LanguageVO, LanguageVFSME, LanguageVFSTF} {
		data := testDataset()
		data.Showtimes = data.Showtimes[:1]
		data.Showtimes[0].Language = language
		result := getStatistics(t, statisticsService(t, data, testServiceNow()), StatisticsQuery{Language: string(language)})
		if result.Totals.Showtimes != 1 || result.Versions[0].Value != string(language) {
			t.Fatalf("language %s: %+v", language, result.Versions)
		}
	}
	for _, format := range []Format{Format2D, Format3D, FormatIMAX, FormatDolby, FormatScreenX, FormatLaserUltra, Format4DX, FormatICE} {
		data := testDataset()
		data.Showtimes = data.Showtimes[:1]
		data.Showtimes[0].Format = format
		result := getStatistics(t, statisticsService(t, data, testServiceNow()), StatisticsQuery{Format: string(format)})
		if result.Totals.Showtimes != 1 || result.Formats[0].Value != string(format) {
			t.Fatalf("format %s: %+v", format, result.Formats)
		}
	}
	data := testDataset()
	data.Showtimes = data.Showtimes[:1]
	data.Showtimes[0].Language, data.Showtimes[0].Format = "unclassified", "unclassified"
	result := getStatistics(t, statisticsService(t, data, testServiceNow()), StatisticsQuery{Language: "unknown", Format: "unknown"})
	if result.Totals.Showtimes != 1 || result.Versions[0].Label != "Non renseigné" || result.Formats[0].Label != "Non renseigné" {
		t.Fatalf("unclassified versions=%+v formats=%+v", result.Versions, result.Formats)
	}
	for minutes, bucket := range map[int]int{-1: 3, 0: 3, 1: 0, 89: 0, 90: 1, 120: 1, 121: 2} {
		if got := statisticsRuntime(minutes); got != bucket {
			t.Errorf("runtime %d: bucket=%d want=%d", minutes, got, bucket)
		}
	}
}

type statisticsCountingSource struct {
	views []*SnapshotView
	calls int
}

func (source *statisticsCountingSource) Snapshot() *SnapshotView {
	view := source.views[min(source.calls, len(source.views)-1)]
	source.calls++
	return view
}

func TestStatisticsSingleSnapshotNowAndCancellation(t *testing.T) {
	data := testDataset()
	empty := data
	empty.Showtimes = nil
	source := &statisticsCountingSource{views: []*SnapshotView{NewSnapshotView(data), NewSnapshotView(empty)}}
	nowCalls := 0
	service, err := NewService(source, ServiceOptions{Now: func() time.Time { nowCalls++; return testServiceNow() }})
	if err != nil {
		t.Fatal(err)
	}
	result := getStatistics(t, service, StatisticsQuery{})
	if source.calls != 1 || nowCalls != 1 || result.Totals.Showtimes != 5 {
		t.Fatalf("snapshot calls=%d now calls=%d totals=%+v", source.calls, nowCalls, result.Totals)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.Statistics(ctx, StatisticsQuery{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
}
