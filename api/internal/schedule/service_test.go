package schedule

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func testDataset() Dataset {
	location, _ := time.LoadLocation(Timezone)
	showing := func(id, theater, movieID, title, poster, clock, language string, runtime int) ShowtimeRecord {
		start, _ := time.ParseInLocation("2006-01-02 15:04", "2026-08-15 "+clock, location)
		if start.Hour() < 8 {
			start = start.AddDate(0, 0, 1)
		}
		return ShowtimeRecord{ID: "ugc-showing-" + id, ProviderShowingID: id, ServiceDate: "2026-08-15", TheaterID: theater, Movie: MovieRecord{ProviderID: movieID, Slug: "ugc-film-" + movieID, Title: title, RuntimeMinutes: runtime, PosterURL: poster}, StartTime: start, EndTime: start.Add(time.Duration(runtime) * time.Minute), Language: language, ProviderVersion: language, Format: "2D", Room: "Salle 1", BookingURL: "https://www.ugc.fr/reservationSeances.html?id=" + id}
	}
	return Dataset{SchemaVersion: 1, Provider: ProviderUGC, Scope: ScopeAll, GeneratedAt: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC), Timezone: Timezone, Window: Window{From: "2026-08-15", Through: "2026-08-15"}, Theaters: []TheaterRecord{{ID: "ugc-25", ProviderID: "25", Slug: "ugc-25", Name: "UGC Lille", Address: "Lille", City: "Lille", PostalCode: "59000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}}, {ID: "ugc-26", ProviderID: "26", Slug: "ugc-26", Name: "UGC Villeneuve", Address: "Villeneuve", City: "Villeneuve d'Ascq", PostalCode: "59650", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}}, {ID: "ugc-99", ProviderID: "99", Slug: "ugc-99", Name: "UGC Lyon", Address: "Lyon", City: "Lyon", PostalCode: "69000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}}}, Showtimes: []ShowtimeRecord{showing("100", "ugc-25", "200", "Film A", "https://static.ugc.fr/posters/200.jpg", "12:00", LanguageVOSTFR, 100), showing("104", "ugc-26", "200", "Film A", "https://static.ugc.fr/posters/200.jpg", "18:00", LanguageVOSTFR, 100), showing("101", "ugc-25", "201", "Film B", "", "14:30", LanguageVFSME, 95), showing("102", "ugc-26", "202", "Film C", "", "00:15", LanguageVO, 75), showing("103", "ugc-99", "203", "Film D", "", "12:30", LanguageVF, 90)}}
}

func kinepolisTestDataset() Dataset {
	location, _ := time.LoadLocation(Timezone)
	start, _ := time.ParseInLocation("2006-01-02 15:04", "2026-08-15 20:00", location)
	return Dataset{SchemaVersion: SchemaVersion, Provider: ProviderKinepolis, Scope: ScopeAll, GeneratedAt: time.Date(2026, 8, 14, 13, 0, 0, 0, time.UTC), Timezone: Timezone, Window: Window{From: "2026-08-15", Through: "2026-08-15"}, Theaters: []TheaterRecord{{Provider: ProviderKinepolis, ID: "kinepolis-LOM", ProviderID: "LOM", Slug: "kinepolis-LOM", Name: "Kinepolis Lomme", City: "Lomme", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{}}}, Showtimes: []ShowtimeRecord{{Provider: ProviderKinepolis, ID: "kinepolis-showing-VS1", ProviderShowingID: "VS1", ServiceDate: "2026-08-15", TheaterID: "kinepolis-LOM", Movie: MovieRecord{Provider: ProviderKinepolis, ProviderID: "HO200", Slug: "kinepolis-film-HO200", Title: "Film A", RuntimeMinutes: 100, PosterURL: "https://cdn.kinepolis.fr/images/posters/ho200.jpg", Overview: "Résumé Kinepolis", ReleaseDate: "2026-01-02", Genres: []string{"Drame"}}, StartTime: start, EndTime: start.Add(100 * time.Minute), Language: LanguageVF, ProviderVersion: "VF", Format: "IMAX", Room: "7", BookingURL: "https://kinepolis.fr/direct-vista-redirect/VS1/0/LOM/0"}}}
}

func combinedTestDataset() Dataset {
	ugc, kinepolis := testDataset(), kinepolisTestDataset()
	for index := range ugc.Showtimes {
		if ugc.Showtimes[index].Movie.ProviderID == "200" {
			ugc.Showtimes[index].Movie.Enrichment = &MovieEnrichment{TMDBID: 42, Overview: "Résumé TMDB", PosterURL: "https://image.tmdb.org/t/p/w500/a.jpg"}
		}
	}
	kinepolis.Showtimes[0].Movie.Enrichment = &MovieEnrichment{TMDBID: 42, Overview: "Résumé TMDB", PosterURL: "https://image.tmdb.org/t/p/w500/a.jpg"}
	ugc.Provider = ProviderCombined
	ugc.GeneratedAt = kinepolis.GeneratedAt
	ugc.Theaters = append(ugc.Theaters, kinepolis.Theaters...)
	ugc.Showtimes = append(ugc.Showtimes, kinepolis.Showtimes...)
	return ugc
}

type testSource struct{ view *SnapshotView }

func (s testSource) Snapshot() *SnapshotView { return s.view }

func newTestSource(data Dataset) testSource { return testSource{view: NewSnapshotView(data)} }

func testService(t *testing.T) *Service {
	t.Helper()
	data := testDataset()
	if err := ValidateDataset(data, true); err != nil {
		t.Fatal(err)
	}
	source := newTestSource(data)
	service, err := NewService(source, ServiceOptions{DefaultCity: "Lille", CityAliases: map[string][]string{"Lille": {"Lille", "Villeneuve d'Ascq"}}})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestTimelineLilleDefaultAndExplicitFrance(t *testing.T) {
	service := testService(t)
	timeline, err := service.Timeline(TimelineQuery{Date: "2026-08-15", Language: LanguageAll})
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline.Theaters) != 2 {
		t.Fatalf("default theaters=%d", len(timeline.Theaters))
	}
	explicit, err := service.Timeline(TimelineQuery{Date: "2026-08-15", Language: LanguageAll, TheaterIDs: []string{"ugc-99"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(explicit.Theaters) != 1 || explicit.Theaters[0].City != "Lyon" {
		t.Fatalf("explicit=%+v", explicit.Theaters)
	}
	if explicit.Theaters[0].Showtimes[0].BookingURL == nil || explicit.Theaters[0].Showtimes[0].StartTime.Location() != time.UTC {
		t.Fatal("booking URL or UTC conversion missing")
	}
}

func TestTimelineMediaIsNullableAndUsesCatalogPosterPrecedence(t *testing.T) {
	data := testDataset()
	for i := range data.Showtimes {
		if data.Showtimes[i].Movie.ProviderID == "200" {
			data.Showtimes[i].Movie.Enrichment = &MovieEnrichment{TMDBID: 42, PosterURL: "https://image.tmdb.org/t/p/w500/a.jpg", BackdropURL: "https://image.tmdb.org/t/p/w780/a.jpg"}
		}
	}
	service, err := NewService(newTestSource(data), ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	timeline, err := service.Timeline(TimelineQuery{Date: "2026-08-15", Language: LanguageAll, TheaterIDs: []string{"ugc-25", "ugc-26"}})
	if err != nil || len(timeline.Theaters) != 2 || len(timeline.Theaters[0].Showtimes) != 2 || len(timeline.Theaters[1].Showtimes) != 2 {
		t.Fatalf("timeline=%+v err=%v", timeline, err)
	}
	matched, missing, repeated := timeline.Theaters[0].Showtimes[0], timeline.Theaters[0].Showtimes[1], timeline.Theaters[1].Showtimes[0]
	if matched.BackdropURL == nil || *matched.BackdropURL != "https://image.tmdb.org/t/p/w780/a.jpg" || missing.BackdropURL != nil {
		t.Fatalf("matched=%+v missing=%+v", matched, missing)
	}
	if matched.PosterURL == nil || *matched.PosterURL != "https://image.tmdb.org/t/p/w500/a.jpg" {
		t.Fatalf("enriched poster did not win: %+v", matched)
	}
	if repeated.PosterURL == nil || *repeated.PosterURL != "https://image.tmdb.org/t/p/w500/a.jpg" || repeated.Movie.Slug != "tmdb-film-42" {
		t.Fatalf("matched movie identity changed across showtimes: %+v", repeated)
	}
	if missing.PosterURL != nil {
		t.Fatalf("missing poster is not null: %+v", missing)
	}
	if matched.StartOffsetMinutes != 240 || matched.DurationMinutes != 100 || missing.StartOffsetMinutes != 390 || missing.DurationMinutes != 95 {
		t.Fatalf("timeline geometry changed: matched=%+v missing=%+v", matched, missing)
	}
	results, err := service.SearchSlot(SlotQuery{TheaterIDs: []string{"ugc-25"}, Date: "2026-08-15", StartAfter: "12:00", FinishBefore: "17:00", Language: LanguageAll})
	if err != nil || len(results) != 2 {
		t.Fatalf("slot results=%+v err=%v", results, err)
	}
	if results[0].PosterURL == nil || *results[0].PosterURL != "https://image.tmdb.org/t/p/w500/a.jpg" || results[0].BackdropURL == nil || *results[0].BackdropURL != "https://image.tmdb.org/t/p/w780/a.jpg" {
		t.Fatalf("slot enriched media=%+v", results[0])
	}
	if results[1].PosterURL != nil || results[1].BackdropURL != nil {
		t.Fatalf("slot missing media must remain null: %+v", results[1])
	}
}

func TestSearchSlotStrictBoundariesAndFilters(t *testing.T) {
	service := testService(t)
	tests := []struct {
		name  string
		query SlotQuery
		want  []string
	}{{"inclusive", SlotQuery{City: "Lille", Date: "2026-08-15", StartAfter: "12:00", FinishBefore: "13:40", Language: LanguageAll}, []string{"ugc-showing-100"}}, {"ads exclusion", SlotQuery{City: "Lille", Date: "2026-08-15", StartAfter: "12:00", FinishBefore: "13:40", BufferAds: 20, Language: LanguageAll}, nil}, {"VF includes SME", SlotQuery{City: "Lille", Date: "2026-08-15", StartAfter: "12:00", FinishBefore: "17:00", Language: LanguageVF}, []string{"ugc-showing-101"}}, {"post midnight alias", SlotQuery{City: "LILLE", Date: "2026-08-15", StartAfter: "00:15", FinishBefore: "01:30", Language: LanguageAll}, []string{"ugc-showing-102"}}, {"exact theater", SlotQuery{TheaterIDs: []string{"ugc-99"}, Date: "2026-08-15", StartAfter: "12:00", FinishBefore: "14:00", Language: LanguageAll}, []string{"ugc-showing-103"}}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results, err := service.SearchSlot(test.query)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != len(test.want) {
				t.Fatalf("count=%d want=%d", len(results), len(test.want))
			}
			for i := range results {
				if results[i].Showtime.ID != test.want[i] {
					t.Fatalf("id=%s", results[i].Showtime.ID)
				}
			}
		})
	}
}

func TestSearchSlotAdsChangeEffectiveStartButNotEnd(t *testing.T) {
	service := testService(t)
	base := SlotQuery{TheaterIDs: []string{"ugc-25"}, Date: "2026-08-15", StartAfter: "12:00", FinishBefore: "14:00", BufferAds: 20, Language: LanguageAll}

	includedQuery := base
	includedQuery.IncludeAds = true
	included, err := service.SearchSlot(includedQuery)
	if err != nil || len(included) != 1 {
		t.Fatalf("included=%+v err=%v", included, err)
	}
	excluded, err := service.SearchSlot(base)
	if err != nil || len(excluded) != 1 {
		t.Fatalf("excluded=%+v err=%v", excluded, err)
	}
	if !included[0].EffectiveStartTime.Equal(included[0].Showtime.StartTime) {
		t.Fatalf("included effective start=%s showtime start=%s", included[0].EffectiveStartTime, included[0].Showtime.StartTime)
	}
	if !excluded[0].EffectiveStartTime.Equal(excluded[0].Showtime.StartTime.Add(20 * time.Minute)) {
		t.Fatalf("excluded effective start=%s showtime start=%s", excluded[0].EffectiveStartTime, excluded[0].Showtime.StartTime)
	}
	if !included[0].EffectiveEndTime.Equal(excluded[0].EffectiveEndTime) || !included[0].EffectiveEndTime.Equal(included[0].Showtime.EndTime.Add(20*time.Minute)) {
		t.Fatalf("included end=%s excluded end=%s showtime end=%s", included[0].EffectiveEndTime, excluded[0].EffectiveEndTime, included[0].Showtime.EndTime)
	}
	if included[0].SlackBeforeMinutes != 0 || excluded[0].SlackBeforeMinutes != 20 || included[0].SlackAfterMinutes != 0 || excluded[0].SlackAfterMinutes != 0 {
		t.Fatalf("included slack=%d/%d excluded slack=%d/%d", included[0].SlackBeforeMinutes, included[0].SlackAfterMinutes, excluded[0].SlackBeforeMinutes, excluded[0].SlackAfterMinutes)
	}

	exactExcluded := base
	exactExcluded.StartAfter = "12:20"
	results, err := service.SearchSlot(exactExcluded)
	if err != nil || len(results) != 1 || results[0].SlackBeforeMinutes != 0 {
		t.Fatalf("exact excluded boundary=%+v err=%v", results, err)
	}
	exactIncluded := exactExcluded
	exactIncluded.IncludeAds = true
	results, err = service.SearchSlot(exactIncluded)
	if err != nil || len(results) != 0 {
		t.Fatalf("included after advertised start=%+v err=%v", results, err)
	}
}

func TestSearchSlotExcludedAdsUsesEffectiveBoundariesAfterMidnight(t *testing.T) {
	service := testService(t)
	query := SlotQuery{City: "Lille", Date: "2026-08-15", StartAfter: "00:35", FinishBefore: "01:50", BufferAds: 20, Language: LanguageAll}
	results, err := service.SearchSlot(query)
	if err != nil || len(results) != 1 {
		t.Fatalf("excluded midnight=%+v err=%v", results, err)
	}
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		t.Fatal(err)
	}
	effectiveStart := results[0].EffectiveStartTime.In(location)
	effectiveEnd := results[0].EffectiveEndTime.In(location)
	if results[0].Showtime.ID != "ugc-showing-102" || effectiveStart.Day() != 16 || effectiveStart.Hour() != 0 || effectiveStart.Minute() != 35 || effectiveEnd.Day() != 16 || effectiveEnd.Hour() != 1 || effectiveEnd.Minute() != 50 {
		t.Fatalf("excluded midnight result=%+v", results[0])
	}
	query.IncludeAds = true
	results, err = service.SearchSlot(query)
	if err != nil || len(results) != 0 {
		t.Fatalf("included midnight after advertised start=%+v err=%v", results, err)
	}
}

func TestSearchSlotFormatFilteringAndValidation(t *testing.T) {
	data := testDataset()
	data.Showtimes[0].Format = Format3D
	data.Showtimes[1].Format = FormatScreenX
	data.Showtimes[2].Format = FormatLaserUltra
	service, err := NewService(newTestSource(data), ServiceOptions{CityAliases: map[string][]string{"Lille": {"Lille", "Villeneuve d'Ascq"}}})
	if err != nil {
		t.Fatal(err)
	}
	base := SlotQuery{City: "Lille", Date: "2026-08-15", StartAfter: "12:00", FinishBefore: "20:00", Language: LanguageAll}
	for _, test := range []struct {
		name   string
		format string
		want   []string
	}{
		{"omitted defaults to all", "", []string{"ugc-showing-100", "ugc-showing-101", "ugc-showing-104"}},
		{"explicit all", FormatAll, []string{"ugc-showing-100", "ugc-showing-101", "ugc-showing-104"}},
		{"screenx", FormatScreenX, []string{"ugc-showing-104"}},
		{"laser ultra", FormatLaserUltra, []string{"ugc-showing-101"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := base
			query.Format = test.format
			results, err := service.SearchSlot(query)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != len(test.want) {
				t.Fatalf("results=%+v", results)
			}
			for index := range test.want {
				if results[index].Showtime.ID != test.want[index] {
					t.Fatalf("result[%d]=%q want=%q", index, results[index].Showtime.ID, test.want[index])
				}
			}
		})
	}
	base.Format = "screenx"
	if _, err := service.SearchSlot(base); err == nil || err.Error() != "Le paramètre format doit être ALL, 2D, 3D, IMAX, DOLBY, SCREENX, LASER_ULTRA ou 4DX." {
		t.Fatalf("invalid format error=%v", err)
	}
}

func TestTheatersCatalogAliasOrderingAndCopies(t *testing.T) {
	service := testService(t)
	all := service.Theaters(TheaterCatalogQuery{})
	want := []string{"ugc-25", "ugc-99", "ugc-26"}
	if len(all) != len(want) {
		t.Fatalf("theaters=%d", len(all))
	}
	for i := range want {
		if all[i].ID != want[i] {
			t.Fatalf("theater[%d]=%s", i, all[i].ID)
		}
	}
	all[0].AvailableDates[0] = "changed"
	if service.Theaters(TheaterCatalogQuery{})[0].AvailableDates[0] != "2026-08-15" {
		t.Fatal("catalog leaked snapshot slices")
	}
	alias := service.Theaters(TheaterCatalogQuery{City: "lille"})
	if len(alias) != 2 || alias[0].ID != "ugc-25" || alias[1].ID != "ugc-26" {
		t.Fatalf("alias=%+v", alias)
	}
	if other := service.Theaters(TheaterCatalogQuery{Chain: "Pathé"}); len(other) != 0 {
		t.Fatalf("non-UGC theaters=%+v", other)
	}
}

func TestCombinedProviderIdentityAndTheaterFiltering(t *testing.T) {
	data := combinedTestDataset()
	if err := ValidateDataset(data, true); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(newTestSource(data), ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if all := service.Theaters(TheaterCatalogQuery{}); len(all) != 4 {
		t.Fatalf("all=%+v", all)
	}
	kinepolis := service.Theaters(TheaterCatalogQuery{Chain: "KINEPOLIS"})
	if len(kinepolis) != 1 || kinepolis[0].Provider != ProviderKinepolis {
		t.Fatalf("kinepolis=%+v", kinepolis)
	}
	ugc := service.Theaters(TheaterCatalogQuery{Chain: "ugc"})
	if len(ugc) != 3 || ugc[0].Provider != ProviderUGC {
		t.Fatalf("ugc=%+v", ugc)
	}
	catalog, err := service.Movies(MovieCatalogQuery{PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Total != 4 || len(catalog.Items) != 4 || catalog.Items[0].Slug != "tmdb-film-42" || catalog.Items[0].Provider != ProviderUGC {
		t.Fatalf("catalog=%+v", catalog)
	}
	schedule, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: "tmdb-film-42", Date: "2026-08-15"})
	if err != nil || len(schedule.Theaters) != 3 {
		t.Fatalf("schedule=%+v err=%v", schedule, err)
	}
	wantShowtimes := map[string]struct {
		provider string
		booking  string
	}{
		"ugc-showing-100":       {ProviderUGC, "https://www.ugc.fr/reservationSeances.html?id=100"},
		"ugc-showing-104":       {ProviderUGC, "https://www.ugc.fr/reservationSeances.html?id=104"},
		"kinepolis-showing-VS1": {ProviderKinepolis, "https://kinepolis.fr/direct-vista-redirect/VS1/0/LOM/0"},
	}
	for _, theater := range schedule.Theaters {
		for _, showtime := range theater.Showtimes {
			want, exists := wantShowtimes[showtime.ID]
			if !exists || showtime.Provider != want.provider || showtime.Movie.Provider != want.provider || showtime.Movie.Slug != "tmdb-film-42" || showtime.BookingURL == nil || *showtime.BookingURL != want.booking {
				t.Fatalf("showtime=%+v theater=%+v", showtime, theater)
			}
			delete(wantShowtimes, showtime.ID)
		}
	}
	if len(wantShowtimes) != 0 {
		t.Fatalf("missing showtimes=%+v", wantShowtimes)
	}
	for _, oldSlug := range []string{"ugc-film-200", "kinepolis-film-HO200"} {
		_, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: oldSlug, Date: "2026-08-15"})
		var notFound *NotFoundError
		if !errors.As(err, &notFound) {
			t.Fatalf("old slug %q error=%v", oldSlug, err)
		}
	}
	timeline, err := service.Timeline(TimelineQuery{Date: "2026-08-15", TheaterIDs: []string{"kinepolis-LOM"}, Language: LanguageAll})
	if err != nil || timeline.Theaters[0].Provider != ProviderKinepolis || timeline.Theaters[0].Showtimes[0].Provider != ProviderKinepolis || timeline.Theaters[0].Showtimes[0].Movie.Provider != ProviderKinepolis || timeline.Theaters[0].Showtimes[0].Movie.Slug != "tmdb-film-42" {
		t.Fatalf("timeline=%+v err=%v", timeline, err)
	}
	slots, err := service.SearchSlot(SlotQuery{TheaterIDs: []string{"kinepolis-LOM"}, Date: "2026-08-15", StartAfter: "19:00", FinishBefore: "23:00", Language: LanguageAll})
	if err != nil || len(slots) != 1 || slots[0].Showtime.Movie.Slug != "tmdb-film-42" || slots[0].Showtime.ID != "kinepolis-showing-VS1" {
		t.Fatalf("slots=%+v err=%v", slots, err)
	}
}

func TestUnmatchedSameTitleMoviesRemainProviderSpecific(t *testing.T) {
	data := combinedTestDataset()
	for index := range data.Showtimes {
		if data.Showtimes[index].Movie.ProviderID == "200" || data.Showtimes[index].Movie.ProviderID == "HO200" {
			data.Showtimes[index].Movie.Enrichment = nil
		}
	}
	service, err := NewService(newTestSource(data), ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := service.Movies(MovieCatalogQuery{Search: "Film A", PageSize: 1})
	if err != nil || catalog.Total != 2 || len(catalog.Items) != 1 {
		t.Fatalf("catalog=%+v err=%v", catalog, err)
	}
	secondPage, err := service.Movies(MovieCatalogQuery{Search: "Film A", Page: 2, PageSize: 1})
	if err != nil || secondPage.Total != 2 || len(secondPage.Items) != 1 {
		t.Fatalf("second page=%+v err=%v", secondPage, err)
	}
	slugs := map[string]bool{catalog.Items[0].Slug: true, secondPage.Items[0].Slug: true}
	if !slugs["ugc-film-200"] || !slugs["kinepolis-film-HO200"] {
		t.Fatalf("slugs=%+v", slugs)
	}
	ugc, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: "ugc-film-200", Date: "2026-08-15"})
	if err != nil || len(ugc.Theaters) != 2 {
		t.Fatalf("ugc=%+v err=%v", ugc, err)
	}
	for _, theater := range ugc.Theaters {
		for _, showtime := range theater.Showtimes {
			if showtime.Provider != ProviderUGC || showtime.Movie.Slug != "ugc-film-200" {
				t.Fatalf("UGC detail crossed provider boundary: %+v", showtime)
			}
		}
	}
	kinepolis, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: "kinepolis-film-HO200", Date: "2026-08-15"})
	if err != nil || len(kinepolis.Theaters) != 1 || kinepolis.Theaters[0].Showtimes[0].Provider != ProviderKinepolis {
		t.Fatalf("kinepolis=%+v err=%v", kinepolis, err)
	}
}

func TestLocalMovieAggregatesCanonicalMetadataAndSourceShowtimes(t *testing.T) {
	data := combinedTestDataset()
	canonical := MovieRecord{
		Title:          "Film local canonique",
		RuntimeMinutes: 120,
		PosterURL:      "https://static.ugc.fr/posters/local.jpg",
		Overview:       "Résumé local",
		ReleaseDate:    "2026-02-03",
		Genres:         []string{"Comédie", "Famille"},
	}
	for index := range data.Showtimes {
		movie := &data.Showtimes[index].Movie
		if movie.ProviderID != "200" && movie.ProviderID != "HO200" {
			continue
		}
		movie.Title = canonical.Title
		movie.RuntimeMinutes = canonical.RuntimeMinutes
		movie.PosterURL = canonical.PosterURL
		movie.Overview = canonical.Overview
		movie.ReleaseDate = canonical.ReleaseDate
		movie.Genres = append([]string(nil), canonical.Genres...)
		movie.Enrichment = nil
		movie.LocalMovieID = 17
		movie.LocalMetadataProvider = ProviderUGC
		data.Showtimes[index].EndTime = data.Showtimes[index].StartTime.Add(120 * time.Minute)
	}
	if err := ValidateDataset(data, true); err != nil {
		t.Fatalf("valid local movie rejected: %v", err)
	}
	service, err := NewService(newTestSource(data), ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := service.Movies(MovieCatalogQuery{Search: "canonique", PageSize: 10})
	if err != nil || catalog.Total != 1 || len(catalog.Items) != 1 {
		t.Fatalf("catalog=%+v err=%v", catalog, err)
	}
	movie := catalog.Items[0]
	if movie.Slug != "local-film-17" || movie.Title != canonical.Title || movie.RuntimeMinutes != 120 || movie.TMDBID != nil || movie.PosterURL == nil || *movie.PosterURL != canonical.PosterURL || movie.Overview == nil || *movie.Overview != canonical.Overview || movie.ReleaseDate == nil || *movie.ReleaseDate != canonical.ReleaseDate || len(movie.Genres) != 2 {
		t.Fatalf("movie=%+v", movie)
	}
	detail, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: "local-film-17", Date: "2026-08-15"})
	if err != nil || len(detail.Theaters) != 3 {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
	want := map[string]struct {
		provider string
		booking  string
	}{
		"ugc-showing-100":       {ProviderUGC, "https://www.ugc.fr/reservationSeances.html?id=100"},
		"ugc-showing-104":       {ProviderUGC, "https://www.ugc.fr/reservationSeances.html?id=104"},
		"kinepolis-showing-VS1": {ProviderKinepolis, "https://kinepolis.fr/direct-vista-redirect/VS1/0/LOM/0"},
	}
	for _, theater := range detail.Theaters {
		for _, showtime := range theater.Showtimes {
			expected, ok := want[showtime.ID]
			if !ok || theater.Provider != expected.provider || showtime.Provider != expected.provider || showtime.Movie.Provider != expected.provider || showtime.Movie.Slug != "local-film-17" || showtime.BookingURL == nil || *showtime.BookingURL != expected.booking || !showtime.EndTime.Equal(showtime.StartTime.Add(120*time.Minute)) {
				t.Fatalf("theater=%+v showtime=%+v", theater, showtime)
			}
			delete(want, showtime.ID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing showtimes=%v", want)
	}
	timeline, err := service.Timeline(TimelineQuery{Date: "2026-08-15", TheaterIDs: []string{"kinepolis-LOM"}, Language: LanguageAll})
	if err != nil || len(timeline.Theaters) != 1 || timeline.Theaters[0].Showtimes[0].Movie.Slug != "local-film-17" || timeline.Theaters[0].Showtimes[0].DurationMinutes != 120 {
		t.Fatalf("timeline=%+v err=%v", timeline, err)
	}
	slots, err := service.SearchSlot(SlotQuery{TheaterIDs: []string{"kinepolis-LOM"}, Date: "2026-08-15", StartAfter: "20:00", FinishBefore: "22:00", Language: LanguageAll})
	if err != nil || len(slots) != 1 || slots[0].Showtime.Movie.Slug != "local-film-17" || !slots[0].EffectiveEndTime.Equal(slots[0].Showtime.StartTime.Add(120*time.Minute)) {
		t.Fatalf("slots=%+v err=%v", slots, err)
	}
	for _, oldSlug := range []string{"ugc-film-200", "kinepolis-film-HO200"} {
		if _, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: oldSlug, Date: "2026-08-15"}); err == nil {
			t.Fatalf("member slug %q remained public", oldSlug)
		}
	}
}

func TestMoviesCatalogPaginationSearchAndCurrentScope(t *testing.T) {
	service := testService(t)
	catalog, err := service.Movies(MovieCatalogQuery{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Total != 4 || len(catalog.Items) != 2 || catalog.Items[0].Title != "Film A" || catalog.Items[1].Title != "Film B" {
		t.Fatalf("catalog=%+v", catalog)
	}
	if catalog.Items[0].PosterURL == nil || *catalog.Items[0].PosterURL != "https://static.ugc.fr/posters/200.jpg" || catalog.Items[1].PosterURL != nil {
		t.Fatalf("posters=%+v", catalog.Items)
	}
	secondPage, err := service.Movies(MovieCatalogQuery{Page: 2, PageSize: 2})
	if err != nil || len(secondPage.Items) != 2 || secondPage.Items[0].Title != "Film C" || secondPage.Items[1].Title != "Film D" {
		t.Fatalf("second page=%+v err=%v", secondPage, err)
	}
	searched, err := service.Movies(MovieCatalogQuery{Search: " film a "})
	if err != nil || searched.Total != 1 || len(searched.Items) != 1 || searched.Items[0].Slug != "ugc-film-200" {
		t.Fatalf("searched=%+v err=%v", searched, err)
	}
	current := false
	empty, err := service.Movies(MovieCatalogQuery{CurrentlyScreened: &current})
	if err != nil || empty.Total != 0 || len(empty.Items) != 0 || empty.Page != 1 || empty.PageSize != 24 {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
	if _, err := service.Movies(MovieCatalogQuery{Page: -1}); err == nil {
		t.Fatal("negative page accepted")
	}
	if _, err := service.Movies(MovieCatalogQuery{PageSize: 101}); err == nil {
		t.Fatal("oversized page accepted")
	}
}

func TestMoviesCatalogSortOrdersAndDefaults(t *testing.T) {
	data := testDataset()
	record := func(id, slug, title string, runtime int, releaseDate string, tmdbID int64) ShowtimeRecord {
		result := data.Showtimes[0]
		result.ID = id
		result.Movie = MovieRecord{Slug: slug, Title: title, RuntimeMinutes: runtime, ReleaseDate: releaseDate}
		if tmdbID > 0 {
			result.Movie.Enrichment = &MovieEnrichment{TMDBID: tmdbID}
		}
		return result
	}
	data.Showtimes = []ShowtimeRecord{
		record("a-1", "slug-a", "Alpha", 100, "2024-01-01", 0),
		record("b-1", "provider-b-1", "Bravo", 90, "2025-01-01", 20),
		record("b-2", "provider-b-2", "Bravo", 90, "2025-01-01", 20),
		record("b-3", "provider-b-3", "Bravo", 90, "2025-01-01", 20),
		record("c-1", "slug-c", "Charlie", 90, "", 0),
		record("c-2", "slug-c", "Charlie", 90, "", 0),
		record("d-1", "slug-d", "Alpha", 110, "2025-01-01", 0),
	}
	service, err := NewService(newTestSource(data), ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		sort MovieCatalogSort
		want []string
	}{
		{name: "title ascending", sort: MovieCatalogSortTitleAsc, want: []string{"slug-a", "slug-d", "tmdb-film-20", "slug-c"}},
		{name: "title descending", sort: MovieCatalogSortTitleDesc, want: []string{"slug-c", "tmdb-film-20", "slug-a", "slug-d"}},
		{name: "release date descending", sort: MovieCatalogSortReleaseDateDesc, want: []string{"slug-d", "tmdb-film-20", "slug-a", "slug-c"}},
		{name: "runtime ascending", sort: MovieCatalogSortRuntimeAsc, want: []string{"tmdb-film-20", "slug-c", "slug-a", "slug-d"}},
		{name: "runtime descending", sort: MovieCatalogSortRuntimeDesc, want: []string{"slug-d", "slug-a", "tmdb-film-20", "slug-c"}},
		{name: "showtimes descending", sort: MovieCatalogSortShowtimesDesc, want: []string{"tmdb-film-20", "slug-c", "slug-a", "slug-d"}},
		{name: "missing defaults to showtimes", want: []string{"tmdb-film-20", "slug-c", "slug-a", "slug-d"}},
		{name: "invalid defaults to showtimes", sort: MovieCatalogSort("unknown"), want: []string{"tmdb-film-20", "slug-c", "slug-a", "slug-d"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			catalog, err := service.Movies(MovieCatalogQuery{Sort: test.sort, PageSize: 10})
			if err != nil {
				t.Fatal(err)
			}
			if catalog.Total != len(test.want) || len(catalog.Items) != len(test.want) {
				t.Fatalf("catalog=%+v", catalog)
			}
			for index, want := range test.want {
				if catalog.Items[index].Slug != want {
					t.Fatalf("items[%d].Slug=%q want %q; catalog=%+v", index, catalog.Items[index].Slug, want, catalog)
				}
			}
		})
	}

	page, err := service.Movies(MovieCatalogQuery{Sort: MovieCatalogSortTitleDesc, Page: 2, PageSize: 1})
	if err != nil || page.Total != 4 || len(page.Items) != 1 || page.Items[0].Slug != "tmdb-film-20" {
		t.Fatalf("pre-sorted page=%+v err=%v", page, err)
	}
}

func TestMovieCatalogEnrichmentPrecedenceAndNullableDefaults(t *testing.T) {
	data := testDataset()
	for index := range data.Showtimes {
		if data.Showtimes[index].Movie.ProviderID == "200" {
			data.Showtimes[index].Movie.Enrichment = &MovieEnrichment{TMDBID: 42, Overview: "Résumé", ReleaseDate: "2026-01-02", Genres: []string{"Drame"}, PosterURL: "https://image.tmdb.org/t/p/w500/a.jpg"}
		}
	}
	service, err := NewService(newTestSource(data), ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := service.Movies(MovieCatalogQuery{PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	matched, unmatched := catalog.Items[0], catalog.Items[1]
	if matched.TMDBID == nil || *matched.TMDBID != 42 || matched.Overview == nil || matched.ReleaseDate == nil || len(matched.Genres) != 1 || matched.PosterURL == nil || *matched.PosterURL != "https://image.tmdb.org/t/p/w500/a.jpg" {
		t.Fatalf("matched=%+v", matched)
	}
	if unmatched.TMDBID != nil || unmatched.Overview != nil || unmatched.ReleaseDate != nil || unmatched.Genres == nil || len(unmatched.Genres) != 0 {
		t.Fatalf("unmatched=%+v", unmatched)
	}
}

func TestMovieShowtimesBackdropUsesValidatedMatchedEnrichment(t *testing.T) {
	tests := []struct {
		name     string
		backdrop string
		want     *string
	}{
		{name: "matched", backdrop: "https://image.tmdb.org/t/p/w780/a.jpg", want: stringPointer("https://image.tmdb.org/t/p/w780/a.jpg")},
		{name: "missing"},
		{name: "rejected", backdrop: "https://example.com/t/p/w780/a.jpg"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := testDataset()
			for index := range data.Showtimes {
				if data.Showtimes[index].Movie.ProviderID == "200" {
					data.Showtimes[index].Movie.Enrichment = &MovieEnrichment{TMDBID: 42, BackdropURL: test.backdrop}
				}
			}
			service, err := NewService(newTestSource(data), ServiceOptions{})
			if err != nil {
				t.Fatal(err)
			}
			schedule, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: "tmdb-film-42", Date: "2026-08-15"})
			if err != nil {
				t.Fatal(err)
			}
			if test.want == nil && schedule.BackdropURL != nil {
				t.Fatalf("backdrop=%q want nil", *schedule.BackdropURL)
			}
			if test.want != nil && (schedule.BackdropURL == nil || *schedule.BackdropURL != *test.want) {
				t.Fatalf("backdrop=%v want %q", schedule.BackdropURL, *test.want)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }

func TestMovieShowtimesScopesKnownEmptyAndUnknown(t *testing.T) {
	service := testService(t)
	schedule, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: "ugc-film-200", Date: "2026-08-15", City: "Lille"})
	if err != nil {
		t.Fatal(err)
	}
	if schedule.Movie.PosterURL == nil || len(schedule.Theaters) != 2 || schedule.Theaters[0].ID != "ugc-25" || schedule.Theaters[1].ID != "ugc-26" {
		t.Fatalf("schedule=%+v", schedule)
	}
	if schedule.Theaters[0].Showtimes[0].StartTime.Location() != time.UTC {
		t.Fatal("showtime not converted to UTC")
	}
	exact, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: "ugc-film-200", Date: "2026-08-15", TheaterIDs: []string{"ugc-26"}})
	if err != nil || len(exact.Theaters) != 1 || exact.Theaters[0].ID != "ugc-26" {
		t.Fatalf("exact=%+v err=%v", exact, err)
	}
	empty, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: "ugc-film-200", Date: "2026-08-16"})
	if err != nil || len(empty.Theaters) != 0 {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
	if _, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: "ugc-film-200", Date: "2026-08-15", City: "Lille", TheaterIDs: []string{"ugc-25"}}); err == nil {
		t.Fatal("city and theaters accepted together")
	}
	if _, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: "ugc-film-200", Date: "2026-08-15", TheaterIDs: []string{"unknown"}}); err == nil {
		t.Fatal("unknown theater accepted")
	}
	_, err = service.MovieShowtimes(MovieShowtimesQuery{Slug: "ugc-film-999", Date: "2026-08-15"})
	var notFound *NotFoundError
	if !errors.As(err, &notFound) || notFound.Message != "Film introuvable." {
		t.Fatalf("unknown error=%v", err)
	}
}

func TestSearchSlotRequiresOneScopeAndValidatesExactTheaters(t *testing.T) {
	service := testService(t)
	base := SlotQuery{Date: "2026-08-15", StartAfter: "12:00", FinishBefore: "14:00", Language: LanguageAll}
	if _, err := service.SearchSlot(base); err == nil {
		t.Fatal("missing scope accepted")
	}
	base.City = "Lille"
	base.TheaterIDs = []string{"ugc-25"}
	if _, err := service.SearchSlot(base); err == nil {
		t.Fatal("city and theaters accepted together")
	}
	base.City = ""
	base.TheaterIDs = []string{"unknown"}
	if _, err := service.SearchSlot(base); err == nil {
		t.Fatal("unknown theater accepted")
	}
}

func TestCityIndexPreservesEqualFoldAliasSemantics(t *testing.T) {
	data := testDataset()
	data.Theaters[0].City = " Lille "
	data.Theaters[1].City = "Évry"
	data.Theaters[2].City = "Lyon"
	direct, err := NewService(newTestSource(data), ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := direct.Theaters(TheaterCatalogQuery{City: " Lille "}); len(got) != 0 {
		t.Fatalf("trimmed direct request matched padded stored city: %+v", got)
	}
	service, err := NewService(newTestSource(data), ServiceOptions{CityAliases: map[string][]string{
		"ZONE": {"ÉVRY", "Lyon"},
		"zone": {"évry", "Lyon"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := service.Theaters(TheaterCatalogQuery{City: "ZoNe"})
	if len(got) != 2 || got[0].ID != "ugc-99" || got[1].ID != "ugc-26" {
		t.Fatalf("alias union=%+v", got)
	}
	positions, err := service.selectTheaters(service.source.Snapshot(), nil, "ZoNe", false)
	if err != nil || len(positions) != 2 || positions[0] != 1 || positions[1] != 2 {
		t.Fatalf("indexed source order=%v err=%v", positions, err)
	}
}

func TestTheatersUsesPublicationCatalogOrderForFilters(t *testing.T) {
	data := testDataset()
	data.Theaters[0].City, data.Theaters[0].Name = "Zebra", "Zulu"
	data.Theaters[1].Provider = ProviderKinepolis
	data.Theaters[1].City, data.Theaters[1].Name = "Alpha", "Bravo"
	data.Theaters[2].City, data.Theaters[2].Name = "Alpha", "Alpha"
	service, err := NewService(newTestSource(data), ServiceOptions{CityAliases: map[string][]string{"AREA": {"Zebra", "Alpha"}}})
	if err != nil {
		t.Fatal(err)
	}
	assertIDs := func(label string, theaters []Theater, want ...string) {
		t.Helper()
		if len(theaters) != len(want) {
			t.Fatalf("%s count=%d theaters=%+v", label, len(theaters), theaters)
		}
		for index := range want {
			if theaters[index].ID != want[index] {
				t.Fatalf("%s theater[%d]=%q want=%q", label, index, theaters[index].ID, want[index])
			}
		}
	}
	assertIDs("unfiltered", service.Theaters(TheaterCatalogQuery{}), "ugc-99", "ugc-26", "ugc-25")
	assertIDs("chain", service.Theaters(TheaterCatalogQuery{Chain: ProviderUGC}), "ugc-99", "ugc-25")
	assertIDs("selective alias", service.Theaters(TheaterCatalogQuery{City: "area"}), "ugc-99", "ugc-26", "ugc-25")
	view := service.source.Snapshot()
	positions := view.catalogPositionsForCities(service.cityLookupValues("area"))
	if len(positions) != 3 || positions[0] != 2 || positions[1] != 1 || positions[2] != 0 {
		t.Fatalf("catalog positions=%v", positions)
	}
}

func TestEmptyDefaultCityPreservesDefaultAndExplicitScopeSemantics(t *testing.T) {
	data := testDataset()
	service, err := NewService(newTestSource(data), ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	timeline, err := service.Timeline(TimelineQuery{Date: "2026-08-15", Language: LanguageAll})
	if err != nil || len(timeline.Theaters) != 0 {
		t.Fatalf("empty default timeline=%+v err=%v", timeline, err)
	}
	all, err := service.selectTheaters(service.source.Snapshot(), nil, "", false)
	if err != nil || len(all) != len(data.Theaters) {
		t.Fatalf("explicit empty city positions=%v err=%v", all, err)
	}

	data.Theaters[0].City = ""
	service, err = NewService(newTestSource(data), ServiceOptions{CityAliases: map[string][]string{"": {"Lyon"}}})
	if err != nil {
		t.Fatal(err)
	}
	timeline, err = service.Timeline(TimelineQuery{Date: "2026-08-15", Language: LanguageAll})
	if err != nil || len(timeline.Theaters) != 2 || timeline.Theaters[0].ID != "ugc-25" || timeline.Theaters[1].ID != "ugc-99" {
		t.Fatalf("empty default direct/alias timeline=%+v err=%v", timeline, err)
	}
}

func TestMoviesCatalogSearchPreservesSharedSlugVariants(t *testing.T) {
	data := testDataset()
	base := data.Showtimes[0]
	record := func(id, title, slug string, tmdbID int64) ShowtimeRecord {
		value := base
		value.ID = id
		value.ProviderShowingID = strings.TrimPrefix(id, "ugc-showing-")
		value.Movie.Title = title
		value.Movie.Slug = slug
		value.Movie.ProviderID = strings.TrimPrefix(slug, "ugc-film-")
		value.BookingURL = "https://www.ugc.fr/reservationSeances.html?id=" + value.ProviderShowingID
		if tmdbID > 0 {
			value.Movie.Enrichment = &MovieEnrichment{TMDBID: tmdbID}
		} else {
			value.Movie.Enrichment = nil
		}
		return value
	}
	data.Showtimes = []ShowtimeRecord{
		record("ugc-showing-501", "Alpha", "ugc-film-10", 42),
		record("ugc-showing-502", "Bêta", "ugc-film-11", 42),
		record("ugc-showing-503", "Bêta", "ugc-film-12", 42),
		record("ugc-showing-504", "Gamma", "ugc-film-20", 0),
		record("ugc-showing-505", "Gamma", "ugc-film-20", 0),
	}
	service, err := NewService(newTestSource(data), ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	all, err := service.Movies(MovieCatalogQuery{Sort: MovieCatalogSortShowtimesDesc, PageSize: 1})
	if err != nil || all.Total != 2 || len(all.Items) != 1 || all.Items[0].Slug != "tmdb-film-42" || all.Items[0].Title != "Alpha" {
		t.Fatalf("all=%+v err=%v", all, err)
	}
	second, err := service.Movies(MovieCatalogQuery{Sort: MovieCatalogSortShowtimesDesc, Page: 2, PageSize: 1})
	if err != nil || second.Total != 2 || len(second.Items) != 1 || second.Items[0].Title != "Gamma" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	filtered, err := service.Movies(MovieCatalogQuery{Search: " bêta ", Sort: MovieCatalogSortShowtimesDesc, PageSize: 10})
	if err != nil || filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].Slug != "tmdb-film-42" || filtered.Items[0].Title != "Bêta" {
		t.Fatalf("filtered=%+v err=%v", filtered, err)
	}
	variants := service.source.Snapshot().movieBySlug["tmdb-film-42"].variants
	if len(variants) != 2 || variants[0].count != 1 || variants[1].count != 2 {
		t.Fatalf("variants=%+v", variants)
	}
}
