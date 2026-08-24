package pathe

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestParseCinemaAndShowMetadata(t *testing.T) {
	cinemas, err := parseCinemas(fixture(t, "cinemas.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cinemas) != 2 || cinemas[0].slug != "lille" || cinemas[0].address != "1 rue du Cinéma" || cinemas[1].slug != "zeta" {
		t.Fatalf("cinemas=%+v", cinemas)
	}
	shows, err := parseShows(fixture(t, "shows.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(shows) != 3 || shows["film-a"].poster != "https://www.pathe.fr/posters/film-a.jpg" || shows["event-a"].poster != "https://media.pathe.fr/posters/event-a.jpg" || shows["film-b"].poster != "" || shows["event-a"].isMovie {
		t.Fatalf("shows=%+v", shows)
	}
}

func TestMetadataAndProgramFailClosed(t *testing.T) {
	if _, err := parseCinemas([]byte(`[] trailing`)); err == nil {
		t.Fatal("trailing JSON data accepted")
	}
	for name, body := range map[string][]byte{
		"duplicate cinema": []byte(`[{"slug":"same","name":"A","theaters":[{"addressLine1":"1 rue","addressZip":"59000","addressCity":"Lille"}]},{"slug":"same","name":"B","theaters":[{"addressLine1":"2 rue","addressZip":"59000","addressCity":"Lille"}]}]`),
		"missing address":  fixture(t, "cinemas-malformed.json"),
		"invalid slug":     []byte(`[{"slug":"bad slug","name":"A","theaters":[{"addressLine1":"1 rue","addressZip":"59000","addressCity":"Lille"}]}]`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseCinemas(body); err == nil {
				t.Fatal("invalid cinema metadata accepted")
			}
		})
	}
	for name, body := range map[string][]byte{
		"top-level array":  []byte(`[{"slug":"film","title":"A","duration":90,"isMovie":true}]`),
		"missing shows":    []byte(`{}`),
		"null shows":       []byte(`{"shows":null}`),
		"empty shows":      []byte(`{"shows":[]}`),
		"malformed shows":  []byte(`{"shows":{}}`),
		"trailing wrapper": []byte(`{"shows":[{"slug":"film","title":"A","duration":90,"isMovie":true}]} trailing`),
		"duplicate show":   []byte(`{"shows":[{"slug":"same","title":"A","duration":90,"isMovie":true},{"slug":"same","title":"B","duration":90,"isMovie":true}]}`),
		"invalid slug":     []byte(`{"shows":[{"slug":"bad slug","title":"A","duration":90,"isMovie":true}]}`),
		"empty title":      []byte(`{"shows":[{"slug":"film","title":" ","duration":90,"isMovie":true}]}`),
		"missing kind":     []byte(`{"shows":[{"slug":"film","title":"A","duration":90}]}`),
		"bad runtime":      []byte(`{"shows":[{"slug":"film","title":"A","duration":0,"isMovie":true}]}`),
		"empty genre":      []byte(`{"shows":[{"slug":"film","title":"A","duration":90,"genres":[" "],"isMovie":true}]}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseShows(body); err == nil {
				t.Fatal("invalid show metadata accepted")
			}
		})
	}
	if shows, err := parseShows([]byte(`{"contentratings":[],"labels":[],"shows":[{"slug":"nullable-fields","title":"Film","duration":90,"posterPath":null,"genres":[],"isMovie":true,"type":null}]}`)); err != nil || shows["nullable-fields"].poster != "" {
		t.Fatalf("nullable optional fields rejected: shows=%+v err=%v", shows, err)
	}
	shows, err := parseShows(fixture(t, "shows.json"))
	if err != nil {
		t.Fatal(err)
	}
	location, _ := time.LoadLocation(schedule.Timezone)
	from, _ := time.ParseInLocation("2006-01-02", "2026-08-15", location)
	if _, err := parseProgram(fixture(t, "program-orphan.json"), shows, from, location); err == nil {
		t.Fatal("orphan show reference accepted")
	}
	if _, err := parseProgram([]byte(`{"shows":{"film-a":{"days":{"15-08-2026":{}}}}}`), shows, from, location); err == nil {
		t.Fatal("invalid advertised date accepted")
	}
}

func TestParseProgramAcceptsIndependentEmptyArraySentinels(t *testing.T) {
	shows, err := parseShows(fixture(t, "shows.json"))
	if err != nil {
		t.Fatal(err)
	}
	location, _ := time.LoadLocation(schedule.Timezone)
	from, _ := time.ParseInLocation("2006-01-02", "2026-08-15", location)
	for name, body := range map[string][]byte{
		"both empty":        []byte(`{"days":[],"shows":[]}`),
		"object then empty": []byte(`{"days":{"2026-08-15":{}},"shows":[]}`),
	} {
		t.Run(name, func(t *testing.T) {
			pairs, err := parseProgram(body, shows, from, location)
			if err != nil || pairs == nil || len(pairs) != 0 {
				t.Fatalf("pairs=%+v err=%v", pairs, err)
			}
		})
	}
	pairs, err := parseProgram([]byte(`{"days":[],"shows":{"film-a":{"days":{"2026-08-15":{"tags":[],"versions":[]}}}}}`), shows, from, location)
	if err != nil || len(pairs) != 1 || len(pairs["film-a"]) != 1 {
		t.Fatalf("mixed populated pairs=%+v err=%v", pairs, err)
	}
}

func TestSourceSlugDerivedIdentityBoundaries(t *testing.T) {
	boolTrue := true
	validCinemaSlug := strings.Repeat("c", maxCinemaSlugLength)
	validCinemas, _ := json.Marshal([]cinemaResponse{{Slug: validCinemaSlug, Name: "Pathé", Theaters: []cinemaTheater{{AddressLine1: "1 rue", AddressZip: "59000", AddressCity: "Lille"}}}})
	if cinemas, err := parseCinemas(validCinemas); err != nil || len("pathe-"+cinemas[0].slug) != maxDerivedIdentityLength {
		t.Fatalf("valid cinema boundary rejected: cinemas=%+v err=%v", cinemas, err)
	}
	invalidCinemas, _ := json.Marshal([]cinemaResponse{{Slug: strings.Repeat("c", maxCinemaSlugLength+1), Name: "Pathé", Theaters: []cinemaTheater{{AddressLine1: "1 rue", AddressZip: "59000", AddressCity: "Lille"}}}})
	if _, err := parseCinemas(invalidCinemas); err == nil {
		t.Fatal("oversized cinema source slug accepted")
	}

	validMovieSlug := strings.Repeat("m", maxMovieSlugLength)
	validShows, _ := json.Marshal(showsResponse{Shows: []showResponse{{Slug: validMovieSlug, Title: "Film", Duration: 90, IsMovie: &boolTrue}}})
	if shows, err := parseShows(validShows); err != nil || len("pathe-film-"+shows[validMovieSlug].slug) != maxDerivedIdentityLength {
		t.Fatalf("valid movie boundary rejected: shows=%+v err=%v", shows, err)
	}
	invalidShows, _ := json.Marshal(showsResponse{Shows: []showResponse{{Slug: strings.Repeat("m", maxMovieSlugLength+1), Title: "Film", Duration: 90, IsMovie: &boolTrue}}})
	if _, err := parseShows(invalidShows); err == nil {
		t.Fatal("oversized movie source slug accepted")
	}

	validShowingID := "V1S" + strings.Repeat("9", maxShowingIDLength-len("V1S"))
	bookingURL := fmt.Sprintf("https://s.pathe.fr/fr/%s/booking", validShowingID)
	if _, showingID, err := canonicalBookingURL(bookingURL); err != nil || showingID != validShowingID || len("pathe-showing-"+showingID) != maxDerivedIdentityLength {
		t.Fatalf("valid showing boundary rejected: showing=%q err=%v", showingID, err)
	}
	invalidShowingID := validShowingID + "9"
	if _, _, err := canonicalBookingURL(fmt.Sprintf("https://s.pathe.fr/fr/%s/booking", invalidShowingID)); err == nil {
		t.Fatal("oversized showing source identity accepted")
	}
}

func TestVersionAndFormatMappings(t *testing.T) {
	versions := map[string]schedule.Language{"VF": schedule.LanguageVF, "vost": schedule.LanguageVOSTFR, "Vo": schedule.LanguageVO, "VFST": schedule.LanguageVFSME}
	for source, expected := range versions {
		language, provider, err := normalizeVersion(source)
		if err != nil || language != expected || provider != strings.ToLower(source) {
			t.Fatalf("source=%q language=%q provider=%q err=%v", source, language, provider, err)
		}
	}
	for _, source := range []string{"", "vfi", "vostfr"} {
		if _, _, err := normalizeVersion(source); err == nil {
			t.Fatalf("unknown version %q accepted", source)
		}
	}
	formats := []struct {
		tags []string
		want schedule.Format
	}{
		{[]string{"3d", "screen x", "dolby", "ice", "4dx", "imax"}, schedule.FormatIMAX},
		{[]string{"screenx", "4DX"}, schedule.Format4DX},
		{[]string{"dolby", "ICE"}, schedule.FormatICE},
		{[]string{"Dolby Cinema"}, schedule.FormatDolby},
		{[]string{"screen-x"}, schedule.FormatScreenX},
		{[]string{"3D"}, schedule.Format3D},
		{[]string{"unknown"}, schedule.Format2D},
	}
	for _, test := range formats {
		if got := normalizeFormat(test.tags); got != test.want {
			t.Fatalf("tags=%v got=%q want=%q", test.tags, got, test.want)
		}
	}
}

func TestBookingAndPosterURLSafety(t *testing.T) {
	booking, showingID, err := canonicalBookingURL("https://s.pathe.fr/fr/V3308S135392/booking")
	if err != nil || booking != "https://s.pathe.fr/fr/V3308S135392/booking" || showingID != "V3308S135392" {
		t.Fatalf("booking=%q showing=%q err=%v", booking, showingID, err)
	}
	for _, raw := range []string{
		"http://s.pathe.fr/fr/V3308S135392/booking",
		"https://user@s.pathe.fr/fr/V3308S135392/booking",
		"https://s.pathe.fr/fr/S135392/booking",
		"https://s.pathe.fr/fr/V03308S135392/booking",
		"https://s.pathe.fr/fr/V3308S0135392/booking",
		"https://s.pathe.fr/fr/V3308S135392/booking?secret=value",
		"https://s.pathe.fr/fr/../booking",
		"https://evil.example/fr/V3308S135392/booking",
	} {
		if _, _, err := canonicalBookingURL(raw); err == nil {
			t.Fatalf("unsafe booking accepted: %q", raw)
		}
	}
	for raw, want := range map[string]string{
		"/poster.jpg":                               "https://www.pathe.fr/poster.jpg",
		"https://media.pathe.fr/poster.jpg":         "https://media.pathe.fr/poster.jpg",
		"https://pathe.fr/poster.jpg":               "https://pathe.fr/poster.jpg",
		"https://evil.example/poster.jpg":           "",
		"https://www.pathe.fr/poster.jpg?token=raw": "",
		"//evil.example/poster.jpg":                 "",
		"/../secret":                                "",
		`/images\secret.jpg`:                        "",
	} {
		if got := safePosterURL(raw); got != want {
			t.Fatalf("safePosterURL(%q)=%q want=%q", raw, got, want)
		}
	}
}

func TestSessionServiceDateAndStrictFields(t *testing.T) {
	location, _ := time.LoadLocation(schedule.Timezone)
	movie := show{slug: "film", title: "Film", runtime: 90, genres: []string{"Drame"}, isMovie: true}
	theater := cinema{slug: "lille", name: "Pathé Lille", address: "1 rue", city: "Lille", postalCode: "59000"}
	session := sessionResponse{Time: "2026-08-16 02:59:00", Version: "vf", Tags: []string{"ice"}, RefCmd: "https://s.pathe.fr/fr/V1S42/booking", AuditoriumName: json.RawMessage(`"ICE"`)}
	record, err := parseSession(session, movie, theater, "2026-08-15", location)
	if err != nil || record.ServiceDate != "2026-08-15" || record.StartTime.Location() != location || record.EndTime.Sub(record.StartTime) != 90*time.Minute {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	session.Time = "2026-08-16 03:00:00"
	if _, err := parseSession(session, movie, theater, "2026-08-16", location); err == nil {
		t.Fatal("03:00 showtime accepted")
	}
	session.Time = "2026-08-16 08:00:00"
	session.Version = "unknown"
	if _, err := parseSession(session, movie, theater, "2026-08-16", location); err == nil {
		t.Fatal("unknown version accepted")
	}
	session.Version = "vf"
	session.RefCmd = ""
	if _, err := parseSession(session, movie, theater, "2026-08-16", location); err == nil {
		t.Fatal("empty booking URL accepted")
	}
	session.RefCmd = "https://s.pathe.fr/fr/V1S42/booking"
	session.AuditoriumName = nil
	if _, err := parseSession(session, movie, theater, "2026-08-16", location); err == nil {
		t.Fatal("empty room accepted")
	}
}

func TestSessionProviderEndTimeAndFallback(t *testing.T) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	movie := show{slug: "film", title: "Film", runtime: 90, genres: []string{"Drame"}, isMovie: true}
	theater := cinema{slug: "lille", name: "Pathé Lille", address: "1 rue", city: "Lille", postalCode: "59000"}
	base := sessionResponse{
		Time:           "2026-08-15 20:00:00",
		Version:        "vf",
		RefCmd:         "https://s.pathe.fr/fr/V1S42/booking",
		AuditoriumName: json.RawMessage(`"1"`),
	}

	withProviderEnd := base
	withProviderEnd.EndTime = "2026-08-15 21:50:00"
	record, err := parseSession(withProviderEnd, movie, theater, "2026-08-15", location)
	if err != nil {
		t.Fatal(err)
	}
	if record.EndTime.Sub(record.StartTime) != 110*time.Minute || record.Movie.RuntimeMinutes != 90 || record.EndTime.Location() != location {
		t.Fatalf("provider end not preserved: record=%+v", record)
	}

	withRollover := base
	withRollover.Time = "2026-08-15 23:30:00"
	withRollover.EndTime = "2026-08-16 01:20:00"
	record, err = parseSession(withRollover, movie, theater, "2026-08-15", location)
	if err != nil || record.EndTime.Format(providerTimeLayout) != withRollover.EndTime || record.EndTime.Sub(record.StartTime) != 110*time.Minute {
		t.Fatalf("date rollover end=%s err=%v", record.EndTime, err)
	}

	for _, test := range []struct {
		name    string
		endTime string
	}{
		{name: "missing"},
		{name: "empty", endTime: "   "},
		{name: "malformed", endTime: "2026-08-15T21:50:00"},
		{name: "non-canonical", endTime: "2026-8-15 21:50:00"},
		{name: "equal to start", endTime: base.Time},
		{name: "before start", endTime: "2026-08-15 19:59:00"},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := base
			session.EndTime = test.endTime
			record, err := parseSession(session, movie, theater, "2026-08-15", location)
			if err != nil {
				t.Fatal(err)
			}
			if !record.EndTime.Equal(record.StartTime.Add(90*time.Minute)) || record.Movie.RuntimeMinutes != 90 {
				t.Fatalf("fallback record=%+v", record)
			}
		})
	}
}
