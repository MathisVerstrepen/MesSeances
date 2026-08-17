package kinepolis

import (
	"context"
	"os"
	"testing"
	"time"

	"movieflow/api/internal/schedule"
)

func fixture(t *testing.T) []byte {
	return namedFixture(t, "schedule.html")
}

func namedFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestParseJQueryExtendDrupalSettings(t *testing.T) {
	data, err := Parse(namedFixture(t, "schedule-jquery-extend.html"), "2026-08-15", "2026-08-15", time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	films := map[string]bool{}
	for _, showing := range data.Showtimes {
		films[showing.Movie.ProviderID] = true
	}
	if len(data.Theaters) != 2 || len(films) != 2 || len(data.Showtimes) != 2 {
		t.Fatalf("theaters=%d films=%d sessions=%d", len(data.Theaters), len(films), len(data.Showtimes))
	}
	var wrapperShowing *schedule.ShowtimeRecord
	for index := range data.Showtimes {
		if data.Showtimes[index].ID == "kinepolis-showing-VS-WRAPPER-1" {
			wrapperShowing = &data.Showtimes[index]
			break
		}
	}
	if wrapperShowing == nil || wrapperShowing.Language != schedule.LanguageVOSTFR || wrapperShowing.Format != "IMAX" || wrapperShowing.Room != "7" || wrapperShowing.BookingURL != "https://kinepolis.fr/direct-vista-redirect/VS-WRAPPER-1/0/LOM/0" {
		t.Fatalf("showing=%+v", wrapperShowing)
	}
	if wrapperShowing.Movie.PosterURL != "https://cdn.kinepolis.fr/images/films/ho123.jpg" || wrapperShowing.Movie.Overview != "Résumé source" || wrapperShowing.Movie.ReleaseDate != "2026-08-01" || len(wrapperShowing.Movie.Genres) != 1 {
		t.Fatalf("movie=%+v", wrapperShowing.Movie)
	}
}

func TestParseJQueryExtendDrupalSettingsRejectsMalformedWrappers(t *testing.T) {
	tests := map[string][]byte{
		"truncated":         []byte(`jQuery.extend(Drupal.settings, {"variables":{"complexes":[]`),
		"missing variables": []byte(`jQuery.extend(Drupal.settings, {"basePath":"/"});`),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(body, "2026-08-15", "2026-08-15", time.Now())
			if err == nil || err.Error() != "Drupal settings JSON not found or malformed" {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestParseEmbeddedSchedule(t *testing.T) {
	generated := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	data, err := Parse(fixture(t), "2026-08-15", "2026-08-16", generated)
	if err != nil {
		t.Fatal(err)
	}
	if data.Provider != schedule.ProviderKinepolis || len(data.Theaters) != 2 || len(data.Showtimes) != 2 {
		t.Fatalf("dataset=%+v", data)
	}
	if data.Theaters[0].ID != "kinepolis-LOM" || data.Theaters[0].City != "Lomme" || data.Theaters[0].Address != "" || len(data.Theaters[0].AcceptedPasses) != 0 {
		t.Fatalf("theater=%+v", data.Theaters[0])
	}
	if data.Theaters[1].City != "Metz" {
		t.Fatalf("exceptional city=%q", data.Theaters[1].City)
	}
	showing := data.Showtimes[0]
	if showing.ID != "kinepolis-showing-VS-100" || showing.Language != schedule.LanguageVOSTFR || showing.Format != "IMAX" || showing.Room != "7" || showing.BookingURL != "https://kinepolis.fr/direct-vista-redirect/VS-100/0/LOM/0" {
		t.Fatalf("showing=%+v", showing)
	}
	if showing.Movie.ProviderID != "HO123" || showing.Movie.PosterURL != "https://cdn.kinepolis.fr/images/films/ho123.jpg" || showing.Movie.Overview != "Résumé source" || showing.Movie.ReleaseDate != "2026-08-01" || len(showing.Movie.Genres) != 1 {
		t.Fatalf("movie=%+v", showing.Movie)
	}
	if data.Showtimes[1].Language != schedule.LanguageVFSME || data.Showtimes[1].Format != "3D" {
		t.Fatalf("normalized=%+v", data.Showtimes[1])
	}
}

func TestParseIgnoresFutureMovieSessions(t *testing.T) {
	data, err := Parse(namedFixture(t, "schedule-scoping-dedup.html"), "2026-08-15", "2026-08-15", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, showing := range data.Showtimes {
		if showing.ProviderShowingID == "FUTURE-ONLY" {
			t.Fatal("future_movies session was parsed")
		}
	}
	if len(data.Showtimes) != 2 {
		t.Fatalf("showtimes=%d", len(data.Showtimes))
	}
}

func TestParseDeduplicatesVistaSessionID(t *testing.T) {
	data, err := Parse(namedFixture(t, "schedule-scoping-dedup.html"), "2026-08-15", "2026-08-15", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	duplicates := 0
	for _, showing := range data.Showtimes {
		if showing.ProviderShowingID == "DUPLICATE" {
			duplicates++
			if showing.TheaterID != "kinepolis-LOM" || showing.Movie.ProviderID != "HO1" {
				t.Fatalf("first duplicate did not win: %+v", showing)
			}
		}
	}
	if duplicates != 1 {
		t.Fatalf("duplicate showtimes=%d", duplicates)
	}
}

func TestParseFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{{"missing settings", []byte("<html></html>")}, {"malformed settings", []byte("Drupal.settings.variables = {")}, {"oversized", make([]byte, MaxBodySize+1)}, {"unknown film", []byte(`Drupal.settings.variables = {"complexes":[{"id":"LOM","name":"Kinepolis Lomme"}],"current_movies":{"films":[{"id":"HO1","title":"Film","duration":90}],"sessions":[{"complexOperator":"LOM","showtime":"2026-08-15T18:00:00Z","vistaSessionId":"S1","film":{"id":"MISSING"}}]}};`)}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse(test.body, "2026-08-15", "2026-08-16", time.Now()); err == nil {
				t.Fatal("invalid page accepted")
			}
		})
	}
}

func TestParseSeparateDrupalAssignments(t *testing.T) {
	body := []byte(`Drupal.settings.variables.complexes=[{"id":"LOM","name":"Kinepolis Lomme"}];Drupal.settings.variables.current_movies.films=[{"id":"HO1","title":"Film","duration":90}];Drupal.settings.variables.current_movies.sessions=[{"complexOperator":"LOM","showtime":"2026-08-15T18:00:00Z","vistaSessionId":"S1","film":{"id":"HO1","format":{"name":"2D"}}}];`)
	data, err := Parse(body, "2026-08-15", "2026-08-15", time.Now())
	if err != nil || len(data.Showtimes) != 1 {
		t.Fatalf("data=%+v err=%v", data, err)
	}
}

type staticFetcher struct {
	body []byte
	err  error
}

func (f staticFetcher) Fetch(context.Context) ([]byte, error) { return f.body, f.err }
func TestSyncUsesFetcherWithoutNetwork(t *testing.T) {
	data, summary, err := Sync(context.Background(), staticFetcher{body: fixture(t)}, SyncOptions{From: "2026-08-15", Through: "2026-08-16", Now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)})
	if err != nil || summary.Cinemas != 2 || summary.Showtimes != 2 || len(data.Showtimes) != 2 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestImageAndCitySafetyHelpers(t *testing.T) {
	if got := imageURL("https://evil.example/a.jpg"); got != "" {
		t.Fatalf("image=%q", got)
	}
	if complexCity("Kinepolis Saint-Julien-lès-Metz") != "Metz" {
		t.Fatal("Metz mapping missing")
	}
}
