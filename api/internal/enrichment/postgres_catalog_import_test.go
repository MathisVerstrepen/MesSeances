package enrichment

import (
	"errors"
	"testing"
	"time"

	"messeances/api/internal/tmdb"
)

func TestImportCatalogMovieRejectsUnverifiedDetailsBeforePersistence(t *testing.T) {
	adult, nonAdult := true, false
	for _, details := range []tmdb.Details{
		{ID: 42, Title: "Unknown"},
		{ID: 42, Title: "Adult", Adult: &adult},
		{ID: 0, Title: "Invalid", Adult: &nonAdult},
		{ID: 42, Title: "Invalid date", Adult: &nonAdult, ReleaseDate: "2026-02-30"},
		{ID: 42, Title: "Invalid image", Adult: &nonAdult, PosterURL: "https://evil.example/a.jpg"},
	} {
		if id, err := ImportCatalogMovie(t.Context(), nil, details, time.Now()); id != 0 || !errors.Is(err, ErrMovieNotImportable) {
			t.Fatalf("unverified details accepted: %d %v", id, err)
		}
	}
}
