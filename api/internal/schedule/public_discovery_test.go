package schedule

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func publicPageFixture() Dataset {
	data := testDataset()
	seen := map[string]int64{}
	for i := range data.Showtimes {
		movie := &data.Showtimes[i].Movie
		id, ok := seen[movie.ProviderID]
		if !ok {
			id = int64(len(seen) + 1)
			seen[movie.ProviderID] = id
			data.PublicMovies = append(data.PublicMovies, PublicMovieRecord{ID: id, IdentityAnchorProvider: ProviderUGC, IdentityAnchorSourceID: movie.ProviderID, Title: movie.Title, RuntimeMinutes: movie.RuntimeMinutes, PosterURL: movie.PosterURL, UpdatedAt: data.GeneratedAt})
			data.MovieSources = append(data.MovieSources, PublicMovieSourceRecord{Provider: ProviderUGC, SourceMovieID: movie.ProviderID, SourceSlug: movie.Slug, PublicMovieID: id, Title: movie.Title, RuntimeMinutes: movie.RuntimeMinutes})
		}
		movie.PublicMovieID = id
	}
	data.PublicMovies = append(data.PublicMovies, PublicMovieRecord{ID: 5, IdentityAnchorTMDBID: 50, TMDBID: 50, Title: "Ended", Overview: "An ended catalogue film", UpdatedAt: data.GeneratedAt}, PublicMovieRecord{ID: 6, IdentityAnchorTMDBID: 60, TMDBID: 60, Title: "Upcoming", HasUpcomingRelease: true, UpcomingActive: true, FrenchReleaseDate: "2026-08-16", UpdatedAt: data.GeneratedAt})
	return data
}

func pageNow() time.Time { return time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC) }

func TestPublicDiscoveryScopeAndRealCounts(t *testing.T) {
	data := publicPageFixture()
	data.Theaters[1].City = "Lille"
	service, err := NewService(newTestSource(data), ServiceOptions{Now: pageNow})
	if err != nil {
		t.Fatal(err)
	}
	nationwide, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: "film-1", Date: "2026-08-15"})
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []MovieShowtimesQuery{
		{Slug: "film-1", Date: "2026-08-15", TheaterIDs: []string{"ugc-25"}, Language: LanguageVF, Format: Format3D, Page: 99},
		{Slug: "film-1", Date: "2026-08-16", City: "Lyon"},
	} {
		result, err := service.MovieShowtimes(query)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Discovery, nationwide.Discovery) {
			t.Fatalf("request scope changed discovery: %+v", result.Discovery)
		}
	}
	scoped, bundle, err := service.MovieShowtimesBundle(MovieShowtimesQuery{Slug: "film-1", Date: "2026-08-15", TheaterIDs: []string{"ugc-25"}})
	if err != nil || !reflect.DeepEqual(scoped.Discovery, bundle.Discovery) {
		t.Fatalf("bundle mismatch: %v", err)
	}
	if want := []DiscoveryCity{{Name: "Lille", Slug: "lille", TheaterCount: 2, ShowtimeCount: 2}}; !reflect.DeepEqual(nationwide.Discovery.Cities, want) {
		t.Fatalf("cities=%+v", nationwide.Discovery.Cities)
	}
	theater, err := service.TheaterShowtimes(TheaterShowtimesQuery{Slug: "ugc-25", Date: "2026-08-16"})
	if err != nil {
		t.Fatal(err)
	}
	if len(theater.Discovery.Movies) != 2 || theater.Discovery.Movies[0].Slug != "film-1" || theater.Discovery.Movies[0].ShowtimeCount != 1 || len(theater.Discovery.OtherTheaters) != 1 || theater.Discovery.OtherTheaters[0].ID != "ugc-26" || theater.Discovery.OtherTheaters[0].ShowtimeCount != 2 {
		t.Fatalf("theater=%+v", theater.Discovery)
	}
	city, err := service.City("lille")
	if err != nil {
		t.Fatal(err)
	}
	if len(city.Discovery.Theaters) != len(city.Theaters) {
		t.Fatal("incomplete city summaries")
	}
	for i, summary := range city.Discovery.Theaters {
		if summary.ID != city.Theaters[i].ID || summary.ShowtimeCount != 2 || summary.MovieCount != 2 {
			t.Fatalf("summary=%+v", summary)
		}
	}
	ended, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: "film-5", Date: "2026-08-15"})
	if err != nil || len(ended.Discovery.Cities) != 0 {
		t.Fatal("ended film got fallback", err)
	}
	encoded, err := json.Marshal(ended.Discovery)
	if err != nil || !strings.Contains(string(encoded), `"cities":[]`) {
		t.Fatal("empty arrays must be arrays", err)
	}
}

func TestPublicDiscoveryBoundsTieDedupeAndExcludedCurrent(t *testing.T) {
	data := publicPageFixture()
	for i := 0; i < 8; i++ {
		theater := data.Theaters[0]
		theater.ID = fmt.Sprintf("ugc-%d", 300+i)
		theater.ProviderID = fmt.Sprint(300 + i)
		theater.Slug = theater.ID
		theater.Name = fmt.Sprintf("Alternative %02d", i)
		data.Theaters = append(data.Theaters, theater)
		showing := data.Showtimes[0]
		showing.ID = fmt.Sprintf("ugc-showing-%d", 300+i)
		showing.ProviderShowingID = fmt.Sprint(300 + i)
		showing.BookingURL = "https://www.ugc.fr/reservationSeances.html?id=" + showing.ProviderShowingID
		showing.TheaterID = theater.ID
		data.Showtimes = append(data.Showtimes, showing)
	}
	view := NewSnapshotView(data)
	// Aggregate is independently deletion/dedupe safe even if a fixture repeats a receipt.
	view.data.Showtimes = append(view.data.Showtimes, view.data.Showtimes[len(view.data.Showtimes)-1])
	aggregate := aggregateProgramme(view, pageNow())
	other := aggregate.theater(view, 0).OtherTheaters
	if len(other) != 6 {
		t.Fatalf("alternatives=%+v", other)
	}
	for i, entry := range other {
		if entry.ID != fmt.Sprintf("ugc-%d", 300+i) || entry.ShowtimeCount != 1 || entry.ID == data.Theaters[0].ID {
			t.Fatalf("alternative=%+v", entry)
		}
	}
	if city := aggregate.movie(view, "film-1").Cities[0]; city.ShowtimeCount != 9 || city.TheaterCount != 9 {
		t.Fatalf("dedupe=%+v", city)
	}
	data = publicPageFixture()
	for i := 0; i < 8; i++ {
		theater := data.Theaters[0]
		theater.ID = fmt.Sprintf("ugc-%d", 400+i)
		theater.ProviderID = fmt.Sprint(400 + i)
		theater.Slug = theater.ID
		theater.City = fmt.Sprintf("City %02d", i)
		data.Theaters = append(data.Theaters, theater)
		showing := data.Showtimes[0]
		showing.ID = fmt.Sprintf("ugc-showing-%d", 400+i)
		showing.ProviderShowingID = fmt.Sprint(400 + i)
		showing.BookingURL = "https://www.ugc.fr/reservationSeances.html?id=" + showing.ProviderShowingID
		showing.TheaterID = theater.ID
		data.Showtimes = append(data.Showtimes, showing)
	}
	view = NewSnapshotView(data)
	cities := aggregateProgramme(view, pageNow()).movie(view, "film-1").Cities
	if len(cities) != 6 || cities[0].Name != "City 00" || cities[5].Name != "City 05" {
		t.Fatalf("bounded cities=%+v", cities)
	}
	// Empty same-city cinema still gets one genuine zero summary, never another city's alternative.
	data = publicPageFixture()
	data.Showtimes = data.Showtimes[:4]
	view = NewSnapshotView(data)
	if alternatives := aggregateProgramme(view, pageNow()).theater(view, 2).OtherTheaters; len(alternatives) != 0 {
		t.Fatal("broadened city")
	}
}

func TestPublicDiscoveryCalendarWindows(t *testing.T) {
	location, _ := time.LoadLocation(Timezone)
	for _, start := range []string{"2026-03-27", "2026-10-23"} {
		t.Run(start, func(t *testing.T) {
			date, _ := time.ParseInLocation(time.DateOnly, start, location)
			data := testDataset()
			data.Window = Window{From: start, Through: date.AddDate(0, 0, 10).Format(time.DateOnly)}
			data.GeneratedAt = date.UTC()
			for i := range data.Theaters {
				data.Theaters[i].AvailableDates = []string{start}
			}
			for i := range data.Showtimes {
				data.Showtimes[i].ServiceDate = start
				data.Showtimes[i].StartTime = date.Add(12 * time.Hour)
				data.Showtimes[i].EndTime = date.Add(14 * time.Hour)
			}
			view := NewSnapshotView(data)
			window := aggregateProgramme(view, date).window
			if window == nil || window.From != start || window.Through != date.AddDate(0, 0, 6).Format(time.DateOnly) {
				t.Fatalf("DST window=%+v", window)
			}
			if window := aggregateProgramme(view, date.AddDate(0, 0, 12)).window; window != nil {
				t.Fatal("expired coverage window")
			}
		})
	}
	data := testDataset()
	view := NewSnapshotView(data)
	if window := aggregateProgramme(view, pageNow().AddDate(0, 0, -1)).window; window == nil || window.From != data.Window.From {
		t.Fatalf("coverage intersection=%+v", window)
	}
}

func TestPublicDiscoveryMovieCardLimitCanonicalAndZeroRuntime(t *testing.T) {
	data := publicPageFixture()
	for i := 7; i < 15; i++ {
		data.PublicMovies = append(data.PublicMovies, PublicMovieRecord{ID: int64(i), IdentityAnchorTMDBID: int64(i), TMDBID: int64(i), Title: fmt.Sprintf("Film %02d", i), UpdatedAt: data.GeneratedAt})
		showing := data.Showtimes[0]
		showing.ID = fmt.Sprintf("ugc-showing-%d", 500+i)
		showing.ProviderShowingID = fmt.Sprint(500 + i)
		showing.BookingURL = "https://www.ugc.fr/reservationSeances.html?id=" + showing.ProviderShowingID
		showing.Movie.PublicMovieID = int64(i)
		showing.Movie.ProviderID = fmt.Sprint(500 + i)
		showing.Movie.Slug = "ugc-film-" + showing.Movie.ProviderID
		data.MovieSources = append(data.MovieSources, PublicMovieSourceRecord{Provider: ProviderUGC, SourceMovieID: showing.Movie.ProviderID, SourceSlug: showing.Movie.Slug, PublicMovieID: int64(i), Title: showing.Movie.Title, RuntimeMinutes: 100})
		data.Showtimes = append(data.Showtimes, showing)
	}
	view := NewSnapshotView(data)
	cards := aggregateProgramme(view, pageNow()).theater(view, 0).Movies
	if err := ValidateDataset(data, true); err != nil {
		t.Fatal(err)
	}
	if len(cards) != 6 || cards[0].Slug != "film-7" || cards[0].RuntimeMinutes != 0 || cards[0].ShowtimeCount != 1 {
		t.Fatalf("cards=%+v", cards)
	}
	slugs := []string{}
	for _, card := range cards {
		slugs = append(slugs, card.Slug)
	}
	if slices.Contains(slugs, "ugc-film-507") {
		t.Fatal("source alias leaked")
	}
}
