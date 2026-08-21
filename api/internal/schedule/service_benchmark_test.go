package schedule

import (
	"fmt"
	"testing"
)

func benchmarkService(b *testing.B) (*PostgresSource, *Service) {
	b.Helper()
	data := testDataset()
	theaterTemplate := data.Theaters[0]
	showingTemplate := data.Showtimes[0]
	data.Theaters = make([]TheaterRecord, 100)
	data.Showtimes = make([]ShowtimeRecord, 0, 10000)
	for theaterIndex := range data.Theaters {
		theaterID := fmt.Sprintf("ugc-%d", theaterIndex+1)
		theater := theaterTemplate
		theater.ID = theaterID
		theater.ProviderID = fmt.Sprintf("%d", theaterIndex+1)
		theater.Slug = theaterID
		theater.City = fmt.Sprintf("City %02d", theaterIndex%10)
		data.Theaters[theaterIndex] = theater
		for showingIndex := range 100 {
			showing := showingTemplate
			showing.ProviderShowingID = fmt.Sprintf("%d", theaterIndex*100+showingIndex+1)
			showing.ID = "ugc-showing-" + showing.ProviderShowingID
			showing.BookingURL = "https://www.ugc.fr/reservationSeances.html?id=" + showing.ProviderShowingID
			showing.TheaterID = theaterID
			showing.Movie.ProviderID = fmt.Sprintf("%d", showingIndex+1)
			showing.Movie.Slug = "ugc-film-" + showing.Movie.ProviderID
			showing.Movie.Title = fmt.Sprintf("Film %03d", showingIndex+1)
			data.Showtimes = append(data.Showtimes, showing)
		}
	}
	reader := &fakeSnapshotReader{version: 1, data: data}
	source, err := NewPostgresSource(b.Context(), reader)
	if err != nil {
		b.Fatal(err)
	}
	service, err := NewService(source, ServiceOptions{DefaultCity: "City 00", CityAliases: map[string][]string{"Metro": {"City 01", "City 02"}}})
	if err != nil {
		b.Fatal(err)
	}
	return source, service
}

func BenchmarkSnapshotAccess(b *testing.B) {
	source, _ := benchmarkService(b)
	b.ReportAllocs()
	for b.Loop() {
		_ = source.Snapshot()
	}
}

func BenchmarkServiceTimeline(b *testing.B) {
	_, service := benchmarkService(b)
	query := TimelineQuery{Date: "2026-08-15", TheaterIDs: []string{"ugc-1"}, Language: LanguageAll}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = service.Timeline(query)
	}
}

func BenchmarkServiceTheaters(b *testing.B) {
	_, service := benchmarkService(b)
	benchmarks := []struct {
		name  string
		query TheaterCatalogQuery
	}{
		{name: "Unfiltered"},
		{name: "Chain", query: TheaterCatalogQuery{Chain: ProviderUGC}},
		{name: "SelectiveCityAlias", query: TheaterCatalogQuery{City: "Metro"}},
	}
	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = service.Theaters(benchmark.query)
			}
		})
	}
}

func BenchmarkServiceMovies(b *testing.B) {
	_, service := benchmarkService(b)
	query := MovieCatalogQuery{PageSize: 24}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = service.Movies(query)
	}
}

func BenchmarkServiceMovieShowtimes(b *testing.B) {
	_, service := benchmarkService(b)
	query := MovieShowtimesQuery{Slug: "ugc-film-1", Date: "2026-08-15"}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = service.MovieShowtimes(query)
	}
}

func BenchmarkServiceSearchSlot(b *testing.B) {
	_, service := benchmarkService(b)
	query := SlotQuery{TheaterIDs: []string{"ugc-1"}, Date: "2026-08-15", StartAfter: "08:00", FinishBefore: "02:00", Language: LanguageAll}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = service.SearchSlot(query)
	}
}
