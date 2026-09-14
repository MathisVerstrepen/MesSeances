package grandecran

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func testLocation(t *testing.T) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	return l
}
func testCinema(id string) cinema {
	c := cinema{ID: id, Name: "Grand Ecran Test", TimeZone: schedule.Timezone}
	c.PracticalInfo.Location.Address, c.PracticalInfo.Location.City, c.PracticalInfo.Location.Zip = "1 rue Test", "Paris", "75001"
	return c
}
func testJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func testSession(t *testing.T, rawID, start, booking string) showtimeResponse {
	t.Helper()
	var s showtimeResponse
	b := testJSON(t, map[string]any{"id": rawID, "startsAt": start, "tags": []string{"Localization.Version.Original"}, "data": map[string]any{"ticketing": []any{map[string]any{"provider": "default", "type": "DESKTOP", "urls": []string{booking}}}}})
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestMoviesProgramAndNumericPrecision(t *testing.T) {
	b := []byte(`[{"id":9007199254740993,"title":"Numeric","runtime":7201,"poster":"https://fr.web.img6.acsta.net/test.jpg","genres":"Action, Drame,Action"},{"id":"cEvent_1-x","title":"Event","runtime":null,"poster":"https://unsupported.example/test.jpg"}]`)
	movies, err := parseMovies(b)
	if err != nil || len(movies) != 2 || movies["9007199254740993"].RuntimeMinutes != 120 || len(movies["9007199254740993"].Genres) != 2 || movies["cEvent_1-x"].PosterURL != "" || movies["cEvent_1-x"].RuntimeMinutes != 0 {
		t.Fatalf("movies=%+v err=%v", movies, err)
	}
	for _, body := range []string{`null`, `[] {}`, `[{"id":"01","title":"X"}]`, `[{"id":"c","title":"X"}]`, `[{"id":"1","title":"X","runtime":0}]`, `[{"id":"1","title":"X","runtime":-60}]`, `[{"id":"1","title":"X","runtime":59}]`, `[{"id":"1","title":"X","poster":"https://acsta.net/../x"}]`} {
		if _, err := parseMovies([]byte(body)); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
	l := testLocation(t)
	p, err := parseProgram([]byte(`{"movieIds":{"titleAsc":[9007199254740993,"cEvent_1-x"],"releaseAsc":["cEvent_1-x",9007199254740993]},"scheduledDays":{"9007199254740993":["2026-01-01","2026-09-15"],"cEvent_1-x":["2029-07-01","2029-07-01"]}}`), "2026-09-14", l)
	if err != nil || !reflect.DeepEqual(p["cEvent_1-x"], []string{"2029-07-01"}) || !reflect.DeepEqual(p["9007199254740993"], []string{"2026-09-15"}) {
		t.Fatalf("program=%v err=%v", p, err)
	}
	for _, body := range []string{`{}`, `{"scheduledDays":null}`, `{"movieIds":{"titleAsc":[]},"scheduledDays":{"1":["2027-01-01"]}}`, `{"scheduledDays":{"1":["2027-02-30"]}}`} {
		if _, err := parseProgram([]byte(body), "2026-09-14", l); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}
func TestShowingIdentityRoomsDuplicatesAndTime(t *testing.T) {
	c, l := testCinema("G028P"), testLocation(t)
	m := schedule.MovieRecord{Provider: schedule.ProviderGrandEcran, ProviderID: "cEvent_1", Slug: "grandecran-film-cEvent_1", Title: "Event"}
	s := testSession(t, "shared-raw-id", "2026-09-15T01:30:00", "https://achat.grandecran.fr/test/r/123")
	r, err := parseShowtime(s, c, m, "2026-09-14", l)
	if err != nil || r.ServiceDate != "2026-09-14" || !r.StartTime.Equal(r.EndTime) || r.Room != "" || r.Language != schedule.LanguageVOSTFR {
		t.Fatalf("record=%+v err=%v", r, err)
	}
	s.Screen = &struct {
		Name string `json:"name"`
	}{" Salle 2 "}
	r2, err := parseShowtime(s, c, m, "2026-09-14", l)
	if err != nil || r2.ID != r.ID || r2.Room != "Salle 2" {
		t.Fatal("room changed identity")
	}
	r3, err := parseShowtime(s, testCinema("P9488"), m, "2026-09-14", l)
	if err != nil || r3.ID == r.ID {
		t.Fatal("cross-cinema collision")
	}
	s.ID = "bad\x00id"
	if _, err := parseShowtime(s, c, m, "2026-09-14", l); err == nil {
		t.Fatal("NUL accepted")
	}
	for _, tc := range []struct {
		raw   string
		valid bool
	}{{"2026-03-29T02:30:00", false}, {"2026-03-29T03:30:00", true}, {"2026-10-25T02:30:00+02:00", true}, {"2026-10-25T02:30:00+01:00", true}, {"2026-09-14T03:30:00+01:00", false}, {"2026-09-14T07:30:00", true}} {
		if _, ok := parseStartTime(tc.raw, l); ok != tc.valid {
			t.Errorf("time=%s valid=%v", tc.raw, ok)
		}
	}
	s = testSession(t, "same", "2026-09-14T20:00:00", "https://achat.grandecran.fr/test/r/123")
	response := scheduleResponse{c.ID: {Schedule: map[string]map[string][]showtimeResponse{m.ProviderID: {"2026-09-14": {s, s}}}}}
	p := map[string][]string{m.ProviderID: {"2026-09-14"}}
	rows, err := parseSchedule(testJSON(t, response), c, p, map[string]schedule.MovieRecord{m.ProviderID: m}, l, "")
	if err != nil || len(rows) != 1 {
		t.Fatal("identical duplicates not collapsed")
	}
	response[c.ID].Schedule[m.ProviderID]["2026-09-14"][1].Tags = []string{"Localization.Language.French"}
	if _, err := parseSchedule(testJSON(t, response), c, p, map[string]schedule.MovieRecord{m.ProviderID: m}, l, ""); err == nil {
		t.Fatal("conflicting duplicate accepted")
	}
}
func TestBatchBoundsAndVersionFormat(t *testing.T) {
	ids := make([]string, 123)
	for i := range ids {
		ids[i] = "c" + strings.Repeat("a", 107) + string(rune('A'+i/26)) + string(rune('A'+i%26))
	}
	batches := batchMovieIDs(ids)
	n := 0
	for _, batch := range batches {
		n += len(batch)
		if len(batch) > 50 || len(moviesURL(batch)) > MaxRequestURLBytes {
			t.Fatal("unbounded batch")
		}
	}
	if n != len(ids) || len(batches) < 3 {
		t.Fatal("lost batch IDs")
	}
	l, v, err := normalizeVersion([]string{"Localization.Language.French", "Localization.Version.Original"})
	if err != nil || l != schedule.LanguageVOSTFR || v != "VOSTFR" {
		t.Fatal("original precedence")
	}
	if normalizeFormat([]string{"Format.Projection.3d", "Auditorium.Experience.DolbyAtmos"}) != schedule.FormatDolby {
		t.Fatal("format precedence")
	}
}
