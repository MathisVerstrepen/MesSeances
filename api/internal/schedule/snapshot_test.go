package schedule

import "testing"

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
