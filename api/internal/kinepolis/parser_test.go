package kinepolis

import (
	"context"
	"encoding/json"
	"os"
	"strings"
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
	data, _, err := parseSchedule(namedFixture(t, "schedule-jquery-extend.html"), "2026-08-15", time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
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
	if wrapperShowing == nil || wrapperShowing.Language != schedule.LanguageVOSTFR || wrapperShowing.Format != "IMAX" || wrapperShowing.Room != "7" || wrapperShowing.BookingURL != "https://kinepolis.fr/direct-vista-redirect/VS-WRAPPER-1/0/KLOM/0" {
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
			_, _, err := parseSchedule(body, "2026-08-15", time.Now())
			if err == nil || err.Error() != "Drupal settings JSON not found or malformed" {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestParseEmbeddedSchedule(t *testing.T) {
	generated := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	data, inventory, err := parseSchedule(fixture(t), "2026-08-15", generated)
	if err != nil {
		t.Fatal(err)
	}
	if data.Provider != schedule.ProviderKinepolis || len(data.Theaters) != 2 || len(data.Showtimes) != 2 || len(inventory) != 17 {
		t.Fatalf("dataset=%+v", data)
	}
	if data.Theaters[0].ID != "kinepolis-KLOM" || data.Theaters[0].City != "Lomme" || data.Theaters[0].Address != "" || len(data.Theaters[0].AcceptedPasses) != 0 {
		t.Fatalf("theater=%+v", data.Theaters[0])
	}
	if data.Theaters[1].City != "Metz" {
		t.Fatalf("exceptional city=%q", data.Theaters[1].City)
	}
	showing := data.Showtimes[0]
	if showing.ID != "kinepolis-showing-VS-100" || showing.Language != schedule.LanguageVOSTFR || showing.Format != "IMAX" || showing.Room != "7" || showing.BookingURL != "https://kinepolis.fr/direct-vista-redirect/VS-100/0/KLOM/0" {
		t.Fatalf("showing=%+v", showing)
	}
	if showing.Movie.ProviderID != "HO123" || showing.Movie.PosterURL != "https://cdn.kinepolis.fr/images/films/ho123.jpg" || showing.Movie.Overview != "Résumé source" || showing.Movie.ReleaseDate != "2026-08-01" || len(showing.Movie.Genres) != 1 {
		t.Fatalf("movie=%+v", showing.Movie)
	}
	if data.Showtimes[1].Language != schedule.LanguageVFSME || data.Showtimes[1].Format != "3D" {
		t.Fatalf("normalized=%+v", data.Showtimes[1])
	}
}

func TestParseLiveStructuredSessionAttributesAsVOSTFR(t *testing.T) {
	body := []byte(`Drupal.settings.variables = {"complexes":[{"id":"KLOM","name":"Kinepolis Lomme"}],"current_movies":{"films":[{"id":"HO00016287","title":"Kultissime Dunkerque","duration":107}],"sessions":[{"complexOperator":"KLOM","showtime":"2026-09-07T18:30:00+00:00","vistaSessionId":"430602","film":{"id":"HO00016287","corporateId":4142,"event":{"code":"0000000035"},"format":{"name":"2D"}},"language":"FR","rawSessionAttributes":"2D,AE,Ciné K,English,fr","sessionAttributes":[{"name":"Sous-tîtres : Français","shortName":"fr"},{"name":"Version Anglaise","shortName":"English"}],"sessionSubtitles":[{"id":"28"}],"isPublicScreening":true,"isSoldOut":false,"hall":22}]}};`)
	body = withCatalogComplexes(t, body)

	data, _, err := parseSchedule(body, "2026-09-07", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, showing := range data.Showtimes {
		if showing.ProviderShowingID == "430602" {
			if showing.Language != schedule.LanguageVOSTFR {
				t.Fatalf("language=%q provider_version=%q", showing.Language, showing.ProviderVersion)
			}
			return
		}
	}
	t.Fatal("target showing not found")
}

func TestParseRequiresExactRawCinemaInventory(t *testing.T) {
	valid := catalogComplexObjects()
	tests := []struct {
		name      string
		complexes any
	}{
		{name: "sixteen", complexes: append([]any(nil), valid[:16]...)},
		{name: "eighteen valid", complexes: append(append([]any(nil), valid...), map[string]any{"id": "EXTRA", "name": "Kinepolis Extra"})},
		{name: "non object", complexes: replaceComplex(valid, 4, "invalid")},
		{name: "missing ID", complexes: replaceComplex(valid, 4, map[string]any{"name": "Kinepolis Bourgoin-Jallieu"})},
		{name: "missing name", complexes: replaceComplex(valid, 4, map[string]any{"id": "KBOUR"})},
		{name: "numeric ID", complexes: replaceComplex(valid, 4, map[string]any{"id": 4, "name": "Kinepolis Bourgoin-Jallieu"})},
		{name: "boolean ID", complexes: replaceComplex(valid, 4, map[string]any{"id": true, "name": "Kinepolis Bourgoin-Jallieu"})},
		{name: "null ID", complexes: replaceComplex(valid, 4, map[string]any{"id": nil, "name": "Kinepolis Bourgoin-Jallieu"})},
		{name: "object ID", complexes: replaceComplex(valid, 4, map[string]any{"id": map[string]any{"value": "KBOUR"}, "name": "Kinepolis Bourgoin-Jallieu"})},
		{name: "array name", complexes: replaceComplex(valid, 4, map[string]any{"id": "KBOUR", "name": []any{"Kinepolis Bourgoin-Jallieu"}})},
		{name: "numeric name", complexes: replaceComplex(valid, 4, map[string]any{"id": "KBOUR", "name": 4})},
		{name: "boolean name", complexes: replaceComplex(valid, 4, map[string]any{"id": "KBOUR", "name": false})},
		{name: "null name", complexes: replaceComplex(valid, 4, map[string]any{"id": "KBOUR", "name": nil})},
		{name: "object name", complexes: replaceComplex(valid, 4, map[string]any{"id": "KBOUR", "name": map[string]any{"value": "Kinepolis Bourgoin-Jallieu"}})},
		{name: "empty ID", complexes: replaceComplex(valid, 4, map[string]any{"id": " ", "name": "Kinepolis Bourgoin-Jallieu"})},
		{name: "empty name", complexes: replaceComplex(valid, 4, map[string]any{"id": "KBOUR", "name": "\t"})},
		{name: "eighteenth malformed", complexes: append(append([]any(nil), valid...), nil)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if data, inventory, err := parseSchedule(scheduleBody(t, test.complexes), "2026-08-15", time.Now()); err == nil {
				t.Fatalf("accepted data=%+v inventory=%+v", data, inventory)
			}
		})
	}
}

func TestParseIncludesPublicSessionBeyondFormerLimit(t *testing.T) {
	body := []byte(`Drupal.settings.variables = {"complexes":[{"id":"LOM","name":"Kinepolis Lomme"}],"current_movies":{"films":[{"id":"F1","title":"Film","duration":90}],"sessions":[{"complexOperator":"LOM","showtime":"2026-08-14T18:00:00+02:00","vistaSessionId":"PAST","public":true,"sold":false,"film":{"id":"F1"}},{"complexOperator":"LOM","showtime":"2026-08-15T18:00:00+02:00","vistaSessionId":"CURRENT","public":true,"sold":false,"film":{"id":"F1"}},{"complexOperator":"LOM","showtime":"2027-02-14T18:00:00+01:00","vistaSessionId":"FAR","public":true,"sold":false,"film":{"id":"F1"}},{"complexOperator":"LOM","showtime":"2027-03-01T18:00:00+01:00","vistaSessionId":"PRIVATE","public":false,"sold":false,"film":{"id":"F1"}},{"complexOperator":"LOM","showtime":"2027-03-02T18:00:00+01:00","vistaSessionId":"SOLD","public":true,"sold":true,"film":{"id":"F1"}}]}};`)
	body = withCatalogComplexes(t, body)
	body = []byte(strings.ReplaceAll(string(body), `"LOM"`, `"KLOM"`))
	data, _, err := parseSchedule(body, "2026-08-15", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if data.Window.Through != "2027-02-14" || len(data.Showtimes) != 2 || len(data.Theaters) != 1 || len(data.Theaters[0].AvailableDates) != 2 {
		t.Fatalf("dataset=%+v", data)
	}
}

func TestParsePreservesMarathonRuntime(t *testing.T) {
	body := []byte(`Drupal.settings.variables = {"complexes":[{"id":"LOM","name":"Kinepolis Lomme"}],"current_movies":{"films":[{"id":"MARATHON","title":"Marathon","duration":721}],"sessions":[{"complexOperator":"LOM","showtime":"2026-08-15T18:00:00Z","vistaSessionId":"MARATHON-1","film":{"id":"MARATHON"}}]}};`)
	body = withCatalogComplexes(t, body)
	body = []byte(strings.ReplaceAll(string(body), `"LOM"`, `"KLOM"`))
	data, _, err := parseSchedule(body, "2026-08-15", time.Now())
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
	data, _, err := parseSchedule(namedFixture(t, "schedule-screenx.html"), "2026-08-15", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Showtimes) != 1 || data.Showtimes[0].Format != schedule.FormatScreenX {
		t.Fatalf("showtimes=%+v", data.Showtimes)
	}
}

func TestParseLaserUltraFromSessionMetadata(t *testing.T) {
	data, _, err := parseSchedule(namedFixture(t, "schedule-laser-ultra.html"), "2026-08-15", time.Now())
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
	data, _, err := parseSchedule(namedFixture(t, "schedule-scoping-dedup.html"), "2026-08-15", time.Now())
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
	data, _, err := parseSchedule(namedFixture(t, "schedule-scoping-dedup.html"), "2026-08-15", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	duplicates := 0
	for _, showing := range data.Showtimes {
		if showing.ProviderShowingID == "DUPLICATE" {
			duplicates++
			if showing.TheaterID != "kinepolis-KLOM" || showing.Movie.ProviderID != "HO1" {
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
			if _, _, err := parseSchedule(test.body, "2026-08-15", time.Now()); err == nil {
				t.Fatal("invalid page accepted")
			}
		})
	}
}

func TestParseSeparateDrupalAssignments(t *testing.T) {
	body := []byte(`Drupal.settings.variables.complexes=[{"id":"LOM","name":"Kinepolis Lomme"}];Drupal.settings.variables.current_movies.films=[{"id":"HO1","title":"Film","duration":90}];Drupal.settings.variables.current_movies.sessions=[{"complexOperator":"LOM","showtime":"2026-08-15T18:00:00Z","vistaSessionId":"S1","film":{"id":"HO1","format":{"name":"2D"}}}];`)
	body = withCatalogComplexes(t, body)
	body = []byte(strings.ReplaceAll(string(body), `"LOM"`, `"KLOM"`))
	data, _, err := parseSchedule(body, "2026-08-15", time.Now())
	if err != nil || len(data.Showtimes) != 1 {
		t.Fatalf("data=%+v err=%v", data, err)
	}
}

type staticFetcher struct {
	body        []byte
	err         error
	details     map[string][]byte
	detailErr   error
	detailCalls []string
}

func (f *staticFetcher) Fetch(context.Context) ([]byte, error) { return f.body, f.err }
func (f *staticFetcher) FetchCinema(_ context.Context, target string) ([]byte, error) {
	f.detailCalls = append(f.detailCalls, target)
	if f.detailErr != nil {
		return nil, f.detailErr
	}
	return f.details[target], nil
}
func TestSyncUsesFetcherWithoutNetwork(t *testing.T) {
	fetcher := &staticFetcher{body: fixture(t), details: map[string][]byte{
		"/cinemas/kinepolis-lomme/infos/": cinemaDetailBody(t, "Kinepolis Lomme", "1 rue du Cinéma", "Lille", "59000"),
		"/cinémas/kinepolis-waves/info/":  cinemaDetailBody(t, "Kinepolis Waves", "1 rue du Cinéma", "Lille", "59000"),
	}}
	data, summary, err := Sync(context.Background(), fetcher, SyncOptions{From: "2026-08-15", Now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)})
	if err != nil || summary.Cinemas != 2 || summary.Showtimes != 2 || len(data.Showtimes) != 2 || len(fetcher.detailCalls) != 2 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	for _, theater := range data.Theaters {
		if theater.Address != "1 rue du Cinéma" || theater.City != "Lille" || theater.PostalCode != "59000" {
			t.Fatalf("theater=%+v", theater)
		}
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

func catalogComplexObjects() []any {
	complexes := make([]any, 0, len(cinemaDefinitions))
	for _, definition := range cinemaDefinitions {
		complexes = append(complexes, map[string]any{"id": definition.providerID, "name": definition.scheduleName})
	}
	return complexes
}

func replaceComplex(source []any, index int, replacement any) []any {
	result := append([]any(nil), source...)
	result[index] = replacement
	return result
}

func scheduleBody(t *testing.T, complexes any) []byte {
	t.Helper()
	settings := map[string]any{
		"complexes": complexes,
		"current_movies": map[string]any{
			"films":    []any{map[string]any{"id": "F1", "title": "Film", "duration": 90}},
			"sessions": []any{map[string]any{"complexOperator": "KLOM", "showtime": "2026-08-15T18:00:00Z", "vistaSessionId": "S1", "film": map[string]any{"id": "F1"}}},
		},
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	return append(append([]byte("Drupal.settings.variables = "), encoded...), ';')
}

func withCatalogComplexes(t *testing.T, body []byte) []byte {
	t.Helper()
	encoded, err := json.Marshal(catalogComplexObjects())
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(body), `"complexes"`)
	if start < 0 {
		start = strings.Index(string(body), "complexes=")
	}
	if start < 0 {
		t.Fatal("complexes marker missing")
	}
	relative := strings.IndexByte(string(body[start:]), '[')
	if relative < 0 {
		t.Fatal("complexes array missing")
	}
	start += relative
	depth, end := 0, -1
	for index := start; index < len(body); index++ {
		switch body[index] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				end = index + 1
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		t.Fatal("complexes array unterminated")
	}
	result := append([]byte(nil), body[:start]...)
	result = append(result, encoded...)
	return append(result, body[end:]...)
}
