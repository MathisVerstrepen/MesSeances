package cgr

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestParseScheduleAllowsOnlyExpiredCurrentServiceDateToDisappear(t *testing.T) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	theater := cinema{id: "W8010", timeZone: schedule.Timezone}
	movies := map[string]movie{"1001": {id: "1001", title: "Synthetic", runtime: 90}}
	program := map[string][]string{"1001": {"2026-08-24"}}
	records, err := parseSchedule(fixture(t, "schedule_w8010.json"), theater, program, movies, location, "2026-08-24")
	if err != nil || len(records) != 0 {
		t.Fatalf("records=%d err=%v", len(records), err)
	}
	if _, err = parseSchedule(fixture(t, "schedule_w8010.json"), theater, program, movies, location, ""); !errors.Is(err, errProviderSnapshotChanged) {
		t.Fatalf("future missing date error=%v", err)
	}
}

func TestParseScheduleKeepsCollidingSourceIDsForDistinctBookings(t *testing.T) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"W8010":{"schedule":{"1001":{"2026-08-25":[{"id":"shared","startsAt":"2026-08-25T20:00:00","tags":["Localization.Language.French"],"screen":{"name":"Salle 1"},"data":{"ticketing":[{"provider":"default","type":"DESKTOP","urls":["https://achat.cgrcinemas.fr/synthetic/r/101"]}]}},{"id":"shared","startsAt":"2026-08-25T20:00:00","tags":["Localization.Language.French"],"screen":{"name":"Salle 2"},"data":{"ticketing":[{"provider":"default","type":"DESKTOP","urls":["https://achat.cgrcinemas.fr/synthetic/r/102"]}]}}]}}}}`)
	records, err := parseSchedule(body, cinema{id: "W8010", timeZone: schedule.Timezone}, map[string][]string{"1001": {"2026-08-25"}}, map[string]movie{"1001": {id: "1001", title: "Synthetic", runtime: 90}}, location, "")
	if err != nil || len(records) != 2 || records[0].ProviderShowingID == records[1].ProviderShowingID {
		t.Fatalf("records=%+v err=%v", records, err)
	}
}

func TestParseShowtimeAddsCGRSlotPadding(t *testing.T) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		title        string
		runtime      int
		start        string
		wantEnd      string
		wantDuration time.Duration
	}{
		{name: "reported W8010 session", title: "La Bataille de Gaulle: L'âge de fer", runtime: 160, start: "2026-08-27T10:15:00", wantEnd: "2026-08-27T13:10:00", wantDuration: 175 * time.Minute},
		{name: "another known runtime", title: "Synthetic", runtime: 102, start: "2026-08-27T20:00:00", wantEnd: "2026-08-27T21:57:00", wantDuration: 117 * time.Minute},
		{name: "unknown runtime", title: "Unknown", runtime: 0, start: "2026-08-27T18:30:00", wantEnd: "2026-08-27T18:30:00", wantDuration: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record, err := parseShowtime(
				showtimeResponse{
					ID:       "synthetic",
					StartsAt: test.start,
					Tags:     []string{"Localization.Language.French"},
					Data:     showtimeData{Ticketing: []byte(`[{"provider":"default","type":"DESKTOP","urls":["https://achat.cgrcinemas.fr/synthetic/r/1"]}]`)},
				},
				cinema{id: "W8010", timeZone: schedule.Timezone},
				movie{id: "1001", title: test.title, runtime: test.runtime},
				"2026-08-27",
				location,
			)
			if err != nil {
				t.Fatal(err)
			}
			wantEnd, err := time.ParseInLocation("2006-01-02T15:04:05", test.wantEnd, location)
			if err != nil {
				t.Fatal(err)
			}
			if !record.EndTime.Equal(wantEnd) || record.EndTime.Sub(record.StartTime) != test.wantDuration || record.Movie.RuntimeMinutes != test.runtime {
				t.Fatalf("start=%s end=%s duration=%s runtime=%d", record.StartTime.Format("15:04"), record.EndTime.Format("15:04"), record.EndTime.Sub(record.StartTime), record.Movie.RuntimeMinutes)
			}
		})
	}
}

func TestParseSyntheticCGRFixtures(t *testing.T) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	cinemas, err := parseCinemas(fixture(t, "cinemas.json"))
	if err != nil || len(cinemas) != 2 || cinemas[0].id != "P0867" || cinemas[1].id != "W8010" {
		t.Fatalf("cinemas=%+v err=%v", cinemas, err)
	}
	from := time.Date(2026, 8, 25, 0, 0, 0, 0, location)
	program, err := parseProgram(fixture(t, "program_w8010.json"), from, location)
	if err != nil || len(program) != 2 {
		t.Fatalf("program=%+v err=%v", program, err)
	}
	movies, err := parseMovies(fixture(t, "movies.json"))
	if err != nil || movies["1001"].runtime != 110 || !reflect.DeepEqual(movies["1001"].genres, []string{"Drame", "Aventure"}) || movies["1002"].runtime != 0 || !reflect.DeepEqual(movies["1002"].genres, []string{"Documentaire"}) {
		t.Fatalf("movies=%+v err=%v", movies, err)
	}
	records, err := parseSchedule(fixture(t, "schedule_w8010.json"), cinemas[1], program, movies, location, "")
	if err != nil || len(records) != 2 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	byMovie := make(map[string]schedule.ShowtimeRecord, len(records))
	for _, record := range records {
		byMovie[record.Movie.ProviderID] = record
	}
	first, unknownRuntime := byMovie["1001"], byMovie["1002"]
	if first.Provider != schedule.ProviderCGR || first.Language != schedule.LanguageVF || first.Format != schedule.FormatICE || first.Room != "Salle 08" || first.Movie.PosterURL != "https://images.acsta.net/posters/1001.jpg" || first.EndTime.Sub(first.StartTime) != 125*time.Minute {
		t.Fatalf("first=%+v", first)
	}
	if unknownRuntime.Language != schedule.LanguageVOSTFR || unknownRuntime.Format != schedule.Format3D || unknownRuntime.Room != "" || unknownRuntime.Movie.RuntimeMinutes != 0 || !unknownRuntime.EndTime.Equal(unknownRuntime.StartTime) {
		t.Fatalf("unknown runtime=%+v", unknownRuntime)
	}
	if first.ProviderShowingID == unknownRuntime.ProviderShowingID || len(first.ProviderShowingID) != len("W8010-")+64 {
		t.Fatalf("showing identities=%q %q", first.ProviderShowingID, unknownRuntime.ProviderShowingID)
	}
}

func TestParseMoviesNormalizesDuplicateAndEmptyGenres(t *testing.T) {
	tests := []struct {
		genres string
		want   []string
	}{
		{genres: `"Concert, Concert"`, want: []string{"Concert"}},
		{genres: `" Drame, , Drame, Aventure "`, want: []string{"Drame", "Aventure"}},
		{genres: `["Comédie",""," Comédie ","Drame"]`, want: []string{"Comédie", "Drame"}},
	}
	for _, test := range tests {
		body := []byte(`[{"id":"1001","title":"Film","runtime":5400,"poster":null,"genres":` + test.genres + `}]`)
		movies, err := parseMovies(body)
		if err != nil || !reflect.DeepEqual(movies["1001"].genres, test.want) {
			t.Fatalf("genres=%s movies=%+v err=%v", test.genres, movies, err)
		}
	}
}

func TestParseMoviesRejectsInvalidGenreShape(t *testing.T) {
	for _, genres := range []string{`42`, `{}`} {
		body := []byte(`[{"id":"1001","title":"Film","runtime":5400,"poster":null,"genres":` + genres + `}]`)
		if movies, err := parseMovies(body); err == nil || movies != nil {
			t.Fatalf("genres=%s movies=%+v err=%v", genres, movies, err)
		}
	}
}

func TestNormalizeCGRTags(t *testing.T) {
	tests := []struct {
		tags     []string
		language schedule.Language
		format   schedule.Format
	}{
		{[]string{"Localization.Language.French"}, schedule.LanguageVF, schedule.Format2D},
		{[]string{"Localization.Version.Original", "Showtime.Accessibility.Subtitled"}, schedule.LanguageVOSTFR, schedule.Format2D},
		{[]string{"Localization.Version.Original", "Auditorium.Experience.DolbyAtmos"}, schedule.LanguageVO, schedule.FormatDolby},
		{[]string{"Localization.Language.Spanish", "Auditorium.Experience.Ice"}, schedule.Language("SPANISH"), schedule.FormatICE},
		{[]string{"Localization.Language.French", "Format.Projection.3d", "Auditorium.Experience.Ice"}, schedule.LanguageVF, schedule.FormatICE},
		{[]string{"Localization.Language.French", "Format.Projection.3d", "Auditorium.Experience.DolbyAtmos"}, schedule.LanguageVF, schedule.FormatDolby},
	}
	for _, test := range tests {
		language, _, err := normalizeVersion(test.tags)
		if err != nil || language != test.language || normalizeFormat(test.tags) != test.format {
			t.Fatalf("tags=%v language=%q format=%q err=%v", test.tags, language, normalizeFormat(test.tags), err)
		}
	}
}

func TestCGRParserRejectsUnsafeURLs(t *testing.T) {
	for _, raw := range []string{"http://images.acsta.net/a.jpg", "https://evil.example/a.jpg", "https://images.acsta.net/../secret"} {
		if got := safePosterURL(raw); got != "" {
			t.Fatalf("safePosterURL(%q)=%q", raw, got)
		}
	}
	validTicketing := `[{"provider":"relay","type":"DESKTOP","urls":["https://relay.invalid/fixture"]},{"provider":"default","type":"DESKTOP","urls":["https://achat.cgrcinemas.fr/lille/r/123"]}]`
	if got, err := ticketingURL([]byte(validTicketing)); err != nil || got != "https://achat.cgrcinemas.fr/lille/r/123" {
		t.Fatalf("ticketingURL valid=%q err=%v", got, err)
	}
	for _, raw := range []string{
		`[{"provider":"relay","type":"DESKTOP","urls":["https://relay.invalid/fixture"]}]`,
		`[{"provider":"default","type":"MOBILE","urls":["https://achat.cgrcinemas.fr/lille/r/123"]}]`,
		`[{"provider":"default","type":"DESKTOP","urls":["https://achat.cgrcinemas.fr/lille/r/123","https://achat.cgrcinemas.fr/lille/r/124"]}]`,
		`[{"provider":"default","type":"DESKTOP","urls":["https://achat.cgrcinemas.fr/lille/r/123"]},{"provider":"default","type":"DESKTOP","urls":["https://achat.cgrcinemas.fr/lille/r/124"]}]`,
		`[{"provider":"default","type":"DESKTOP","urls":["http://achat.cgrcinemas.fr/lille/r/123"]}]`,
		`[{"provider":"default","type":"DESKTOP","urls":["https://evil.example/lille/r/123"]}]`,
		`[{"provider":"default","type":"DESKTOP","urls":["https://achat.cgrcinemas.fr/lille/r/0"]}]`,
		`[{"provider":"default","type":"DESKTOP","urls":["https://achat.cgrcinemas.fr/lille/r/123?source=x"]}]`,
	} {
		if got, err := ticketingURL([]byte(raw)); err == nil || got != "" {
			t.Fatalf("ticketingURL(%q)=%q err=%v", raw, got, err)
		}
	}
	if language, _, err := normalizeVersion([]string{"Localization.Language.All"}); err == nil || language != "" {
		t.Fatalf("language=%q err=%v", language, err)
	}
}
