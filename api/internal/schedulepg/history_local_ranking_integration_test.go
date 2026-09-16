package schedulepg

import (
	"fmt"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestHistoryLocalShowtimeRankingBeforeLimitIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	data := testDataset()
	baseTheater, baseShowing, secondMovie := data.Theaters[0], data.Showtimes[0], data.Showtimes[2].Movie
	data.Theaters, data.Showtimes = nil, nil
	for n := 1; n <= 101; n++ {
		theater := baseTheater
		theater.ID, theater.ProviderID = fmt.Sprintf("ugc-%d", n), fmt.Sprint(n)
		theater.Slug, theater.Name, theater.City = theater.ID, fmt.Sprintf("Cinéma %03d", n), fmt.Sprintf("Ville %03d", n)
		data.Theaters = append(data.Theaters, theater)
		count := 2
		if n == 99 || n == 101 {
			count = 3
		}
		for j := range count {
			showing := baseShowing
			showing.ProviderShowingID = fmt.Sprint(n*10 + j)
			showing.ID, showing.TheaterID = "ugc-showing-"+showing.ProviderShowingID, theater.ID
			showing.BookingURL = "https://www.ugc.fr/reservationSeances.html?id=" + showing.ProviderShowingID
			showing.StartTime = showing.StartTime.Add(time.Duration(j) * time.Hour)
			showing.EndTime = showing.EndTime.Add(time.Duration(j) * time.Hour)
			if n != 101 && j == 1 {
				showing.Movie = secondMovie
			}
			data.Showtimes = append(data.Showtimes, showing)
		}
	}
	historyPublish(t, store, data)
	result := historyGet(t, store, schedule.StatisticsQuery{})
	if result.Totals != (schedule.StatisticsTotals{Showtimes: 204, Movies: 2, Cities: 101, Theaters: 101}) {
		t.Fatalf("full totals=%+v", result.Totals)
	}
	if len(result.Local.Cities) != 100 || len(result.Local.Theaters) != 100 || !result.Limits.Local.Cities || !result.Limits.Local.Theaters {
		t.Fatalf("top100 lengths=%d/%d limits=%+v", len(result.Local.Cities), len(result.Local.Theaters), result.Limits.Local)
	}
	// 101 has fewer films but more screenings than 100: sorting after LIMIT
	// would lose it. 99 wins their showtime tie on movie count, then names sort.
	want := []int{99, 101}
	for n := 1; n <= 98; n++ {
		want = append(want, n)
	}
	for i, n := range want {
		city, theater := result.Local.Cities[i], result.Local.Theaters[i]
		if city.Slug != fmt.Sprintf("ville-%03d", n) || theater.ID != fmt.Sprintf("ugc-%d", n) {
			t.Fatalf("position %d: city=%+v theater=%+v want=%d", i, city, theater, n)
		}
		showtimes, movies := 2, 2
		if n == 99 || n == 101 {
			showtimes = 3
		}
		if n == 101 {
			movies = 1
		}
		if city.ShowtimeCount != showtimes || theater.ShowtimeCount != showtimes || city.MovieCount != movies || theater.MovieCount != movies || city.TheaterCount != 1 {
			t.Fatalf("counts changed: city=%+v theater=%+v", city, theater)
		}
	}
	assertHistorySums(t, result)
}
