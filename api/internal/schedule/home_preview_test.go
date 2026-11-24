package schedule

import (
	"slices"
	"testing"
	"time"
)

func TestNextScreeningsScopedDistinctAndChronological(t *testing.T) {
	data := testDataset()
	later := data.Showtimes[0]
	later.ID, later.ProviderShowingID = "ugc-showing-105", "105"
	later.ServiceDate = "2026-08-20"
	later.StartTime, later.EndTime = later.StartTime.AddDate(0, 0, 5), later.EndTime.AddDate(0, 0, 5)
	later.Movie.ProviderID, later.Movie.Slug, later.Movie.Title = "204", "ugc-film-204", "Later"
	data.Showtimes = append(data.Showtimes, later)
	data.Theaters[0].AvailableDates = append(data.Theaters[0].AvailableDates, later.ServiceDate)
	now := data.Showtimes[0].StartTime
	service, err := NewService(newTestSource(data), ServiceOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	movies := []string{"ugc-film-204", "ugc-film-203", "ugc-film-202", "ugc-film-200", "ugc-film-200", "ugc-film-201", "film-unknown"}
	selected := []string{"ugc-25", "ugc-26", "retired-theater"}
	got, err := service.NextScreenings(movies, selected)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, item := range got {
		order = append(order, item.MovieSlug)
	}
	if !slices.Equal(order, []string{"ugc-film-201", "ugc-film-200", "ugc-film-202", "ugc-film-204"}) {
		t.Fatal(order)
	}
	if got[1].Theater.ID != "ugc-26" || !got[1].StartTime.Equal(data.Showtimes[1].StartTime) || got[1].PosterURL == nil ||
		got[2].ServiceDate != "2026-08-15" || got[2].StartTime.In(service.location).Day() != 16 {
		t.Fatal(got)
	}
	now = later.StartTime
	if empty, err := service.NextScreenings(movies, selected); err != nil || empty == nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
}
