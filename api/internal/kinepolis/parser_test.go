package kinepolis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"messeances/api/internal/schedule"
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
	data, err := Parse(namedFixture(t, "schedule-jquery-extend.html"), "2026-08-15", time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
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
			_, err := Parse(body, "2026-08-15", time.Now())
			if err == nil || err.Error() != "Drupal settings JSON not found or malformed" {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestParseEmbeddedSchedule(t *testing.T) {
	generated := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	data, err := Parse(fixture(t), "2026-08-15", generated)
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

func TestParseIncludesPublicSessionBeyondFormerLimit(t *testing.T) {
	body := []byte(`Drupal.settings.variables = {"complexes":[{"id":"LOM","name":"Kinepolis Lomme"}],"current_movies":{"films":[{"id":"F1","title":"Film","duration":90}],"sessions":[{"complexOperator":"LOM","showtime":"2026-08-14T18:00:00+02:00","vistaSessionId":"PAST","public":true,"sold":false,"film":{"id":"F1"}},{"complexOperator":"LOM","showtime":"2026-08-15T18:00:00+02:00","vistaSessionId":"CURRENT","public":true,"sold":false,"film":{"id":"F1"}},{"complexOperator":"LOM","showtime":"2027-02-14T18:00:00+01:00","vistaSessionId":"FAR","public":true,"sold":false,"film":{"id":"F1"}},{"complexOperator":"LOM","showtime":"2027-03-01T18:00:00+01:00","vistaSessionId":"PRIVATE","public":false,"sold":false,"film":{"id":"F1"}},{"complexOperator":"LOM","showtime":"2027-03-02T18:00:00+01:00","vistaSessionId":"SOLD","public":true,"sold":true,"film":{"id":"F1"}}]}};`)
	data, err := Parse(body, "2026-08-15", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if data.Window.Through != "2027-02-14" || len(data.Showtimes) != 2 || len(data.Theaters) != 1 || len(data.Theaters[0].AvailableDates) != 2 {
		t.Fatalf("dataset=%+v", data)
	}
}

func TestParseAcceptsTheatersAboveFormerSharedLimit(t *testing.T) {
	complexes := make([]map[string]any, 257)
	sessions := make([]map[string]any, 257)
	for index := range complexes {
		id := fmt.Sprintf("C%03d", index)
		complexes[index] = map[string]any{"id": id, "name": "Kinepolis " + id}
		sessions[index] = map[string]any{"complexOperator": id, "showtime": "2026-08-15T18:00:00+02:00", "vistaSessionId": fmt.Sprintf("S%03d", index), "film": map[string]any{"id": "F1"}}
	}
	settings := map[string]any{"complexes": complexes, "current_movies": map[string]any{"films": []map[string]any{{"id": "F1", "title": "Film", "duration": 90}}, "sessions": sessions}}
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	body := append([]byte("Drupal.settings.variables = "), encoded...)
	body = append(body, ';')
	data, err := Parse(body, "2026-08-15", time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
	if err != nil || len(data.Theaters) != 257 || len(data.Showtimes) != 257 {
		t.Fatalf("theaters=%d showtimes=%d err=%v", len(data.Theaters), len(data.Showtimes), err)
	}
}

func TestParsePreservesMarathonRuntime(t *testing.T) {
	body := []byte(`Drupal.settings.variables = {"complexes":[{"id":"LOM","name":"Kinepolis Lomme"}],"current_movies":{"films":[{"id":"MARATHON","title":"Marathon","duration":721}],"sessions":[{"complexOperator":"LOM","showtime":"2026-08-15T18:00:00Z","vistaSessionId":"MARATHON-1","film":{"id":"MARATHON"}}]}};`)
	data, err := Parse(body, "2026-08-15", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Showtimes) != 1 {
		t.Fatalf("showtimes=%d", len(data.Showtimes))
	}
	showing := data.Showtimes[0]
	if showing.Movie.RuntimeMinutes != 721 || showing.EndTime.Sub(showing.StartTime) != 721*time.Minute {
		t.Fatalf("showing=%+v", showing)
	}
}

func TestFormatCanonicalTechnologies(t *testing.T) {
	tests := map[string]schedule.Format{
		"2D":                       schedule.Format2D,
		"3D":                       schedule.Format3D,
		"ScreenX":                  schedule.FormatScreenX,
		"Screen X":                 schedule.FormatScreenX,
		"Screen-X":                 schedule.FormatScreenX,
		"Laser Ultra by Kinepolis": schedule.FormatLaserUltra,
		"Laser Ultra Screen X":     schedule.FormatLaserUltra,
		"4DX":                      schedule.Format4DX,
		"IMAX Laser":               schedule.FormatIMAX,
		"Dolby Cinema":             schedule.FormatDolby,
	}
	for source, expected := range tests {
		if actual := format(source); actual != expected {
			t.Errorf("format(%q)=%q want=%q", source, actual, expected)
		}
	}
}

func TestParseScreenXFromSessionMetadata(t *testing.T) {
	data, err := Parse(namedFixture(t, "schedule-screenx.html"), "2026-08-15", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Showtimes) != 1 || data.Showtimes[0].Format != schedule.FormatScreenX {
		t.Fatalf("showtimes=%+v", data.Showtimes)
	}
}

func TestParseLaserUltraFromSessionMetadata(t *testing.T) {
	data, err := Parse(namedFixture(t, "schedule-laser-ultra.html"), "2026-08-15", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Showtimes) != 1 || data.Showtimes[0].Format != schedule.FormatLaserUltra {
		t.Fatalf("showtimes=%+v", data.Showtimes)
	}
	if data.Showtimes[0].Language != schedule.LanguageVF {
		t.Fatalf("language=%q", data.Showtimes[0].Language)
	}
}

func TestSessionFormatUsesSafeSessionSources(t *testing.T) {
	film := map[string]any{"format": map[string]any{"name": "3D"}}
	tests := map[string]map[string]any{
		"attributes": {"attributes": []any{map[string]any{"name": "Laser Ultra"}}},
		"version":    {"version": map[string]any{"label": "Laser Ultra"}},
		"hall":       {"hall": "Laser Ultra"},
		"room":       {"room": map[string]any{"name": "Laser Ultra"}},
		"screen":     {"screen": []any{"Laser Ultra"}},
		"technology": {"technology": map[string]any{"value": "Laser Ultra"}},
	}
	for name, session := range tests {
		t.Run(name, func(t *testing.T) {
			if actual := sessionFormat(session, film, sessionAttributes(session)); actual != schedule.FormatLaserUltra {
				t.Fatalf("format=%q", actual)
			}
		})
	}
}

func TestParseIgnoresFutureMovieSessions(t *testing.T) {
	data, err := Parse(namedFixture(t, "schedule-scoping-dedup.html"), "2026-08-15", time.Now())
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
	data, err := Parse(namedFixture(t, "schedule-scoping-dedup.html"), "2026-08-15", time.Now())
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
			if _, err := Parse(test.body, "2026-08-15", time.Now()); err == nil {
				t.Fatal("invalid page accepted")
			}
		})
	}
}

func TestParseSeparateDrupalAssignments(t *testing.T) {
	body := []byte(`Drupal.settings.variables.complexes=[{"id":"LOM","name":"Kinepolis Lomme"}];Drupal.settings.variables.current_movies.films=[{"id":"HO1","title":"Film","duration":90}];Drupal.settings.variables.current_movies.sessions=[{"complexOperator":"LOM","showtime":"2026-08-15T18:00:00Z","vistaSessionId":"S1","film":{"id":"HO1","format":{"name":"2D"}}}];`)
	data, err := Parse(body, "2026-08-15", time.Now())
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
	data, summary, err := Sync(context.Background(), staticFetcher{body: fixture(t)}, SyncOptions{From: "2026-08-15", Now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)})
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
