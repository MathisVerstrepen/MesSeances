package schedule

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMetacriticDatasetAndMaterialization(t *testing.T) {
	for _, tc := range []struct {
		id    string
		tmdb  int64
		valid bool
	}{
		{"", 0, true}, {"movie/a", 42, true}, {"movie/" + strings.Repeat("a", 249), 42, true}, {"movie/a", 0, false}, {"movie/a", -1, false}, {"movie/A", 42, false}, {"movie/a\n", 42, false}, {"game/a", 42, false}, {"movie/a/b", 42, false},
	} {
		data := testDataset()
		data.Showtimes[0].Movie.Enrichment = &MovieEnrichment{TMDBID: tc.tmdb, MetacriticID: tc.id}
		if err := ValidateDataset(data, true); (err == nil) != tc.valid {
			t.Fatalf("id=%q tmdb=%d err=%v", tc.id, tc.tmdb, err)
		}
		pub := PublicMovieRecord{ID: 1, IdentityAnchorProvider: ProviderUGC, IdentityAnchorSourceID: "200", Title: "Film", RuntimeMinutes: 90, TMDBID: tc.tmdb, MetacriticID: tc.id, UpdatedAt: time.Now().UTC()}
		if err := validatePublicMovieCatalog(Dataset{PublicMovies: []PublicMovieRecord{pub}}); (err == nil) != tc.valid {
			t.Fatalf("public id=%q tmdb=%d err=%v", tc.id, tc.tmdb, err)
		}
		view := &SnapshotView{publicMovieByID: map[int64]int{}}
		for _, movie := range []MovieCatalogItem{materializePublicMovie(pub), materializeCatalogMovie(view, data.Showtimes[0].Movie)} {
			body, err := json.Marshal(movie)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if json.Unmarshal(body, &fields) != nil {
				t.Fatal("invalid JSON")
			}
			value, exists := fields["metacritic_id"]
			if !exists || tc.valid && tc.id != "" && value != tc.id || (tc.id == "" || !tc.valid) && value != nil {
				t.Fatalf("wire=%s", body)
			}
		}
	}
	body, _ := json.Marshal(Movie{Slug: "film-1", Title: "Film"})
	if strings.Contains(string(body), "metacritic") {
		t.Fatal("lightweight movie changed")
	}
}
