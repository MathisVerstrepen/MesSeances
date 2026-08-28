package schedule

import (
	"testing"
	"time"
)

func TestNewSnapshotViewDetachesNestedDataAndBuildsIndexes(t *testing.T) {
	data := testDataset()
	data.Showtimes[0].Movie.Genres = []string{"Source"}
	data.Showtimes[0].Movie.Enrichment = &MovieEnrichment{TMDBID: 42, Genres: []string{"Enriched"}}
	view := NewSnapshotView(data)
	data.Theaters[0].AvailableDates[0] = "mutated"
	data.Theaters[0].AcceptedPasses[0] = "mutated"
	data.Showtimes[0].Movie.Genres[0] = "mutated"
	data.Showtimes[0].Movie.Enrichment.Genres[0] = "mutated"
	data.Showtimes[0].Movie.Enrichment.TMDBID = 99
	if view.data.Theaters[0].AvailableDates[0] != "2026-08-15" || view.data.Theaters[0].AcceptedPasses[0] != "UGC_ILLIMITE" || view.data.Showtimes[0].Movie.Genres[0] != "Source" || view.data.Showtimes[0].Movie.Enrichment.Genres[0] != "Enriched" || view.data.Showtimes[0].Movie.Enrichment.TMDBID != 42 {
		t.Fatal("view retained nested caller memory")
	}
	if view.theaterByID["ugc-25"] != 0 || len(view.theaterDate[theaterDateKey{theaterID: "ugc-25", date: "2026-08-15"}]) != 2 || view.movieBySlug["tmdb-film-42"].firstShowtime != 0 || len(view.movieDate[movieDateKey{slug: "tmdb-film-42", date: "2026-08-15"}]) != 1 {
		t.Fatalf("indexes=%+v %+v", view.theaterDate, view.movieDate)
	}
}

func TestSnapshotViewReadyAt(t *testing.T) {
	baseNow := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		now    time.Time
		mutate func(*Dataset)
		ready  bool
	}{
		{name: "exact maximum age", now: baseNow, ready: true},
		{name: "future generation", now: baseNow, mutate: func(data *Dataset) { data.GeneratedAt = baseNow.Add(time.Nanosecond) }},
		{name: "older than maximum age", now: baseNow, mutate: func(data *Dataset) { data.GeneratedAt = baseNow.Add(-maxScheduleAge - time.Nanosecond) }},
		{name: "before Paris window", now: time.Date(2026, 8, 14, 21, 0, 0, 0, time.UTC)},
		{name: "after Paris window", now: time.Date(2026, 8, 15, 22, 0, 0, 0, time.UTC)},
		{name: "inclusive through date", now: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC), mutate: func(data *Dataset) { data.Window.Through = "2026-08-16" }, ready: true},
		{name: "incomplete dataset", now: baseNow, mutate: func(data *Dataset) { data.Showtimes = nil }},
		{name: "malformed window", now: baseNow, mutate: func(data *Dataset) { data.Window.From = "invalid" }},
		{name: "zero clock", now: time.Time{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := testDataset()
			data.GeneratedAt = test.now.Add(-maxScheduleAge)
			if test.mutate != nil {
				test.mutate(&data)
			}
			if got := NewSnapshotView(data).ReadyAt(test.now); got != test.ready {
				t.Fatalf("ReadyAt()=%t want %t", got, test.ready)
			}
		})
	}

	var nilView *SnapshotView
	if nilView.ReadyAt(baseNow) {
		t.Fatal("nil view is ready")
	}
}
