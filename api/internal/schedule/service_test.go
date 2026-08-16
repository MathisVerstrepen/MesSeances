package schedule

import (
	"errors"
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

type testSource struct{ data Dataset }

func (s testSource) Snapshot() Dataset { return cloneDataset(s.data) }

func testService(t *testing.T) *Service {
	t.Helper()
	source := testSource{data: testDataset()}
	if err := ValidateDataset(source.data, true); err != nil {
		t.Fatal(err)
	}
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
