package noecinemas

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestParseShowtimeVOF(t *testing.T) {
	f := fixture(t)
	cs, err := parseCinemas(f.cinemas)
	if err != nil {
		t.Fatal(err)
	}
	var c cinema
	for _, item := range cs {
		if item.ID == "P8088" {
			c = item
		}
	}
	var raw scheduleResponse
	if err := json.Unmarshal(f.schedules[c.ID], &raw); err != nil {
		t.Fatal(err)
	}
	movies, err := parseMovies(f.movies)
	if err != nil {
		t.Fatal(err)
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		tags    []string
		want    schedule.Language
		version string
	}{
		{[]string{"Localization.Language.VOF"}, schedule.LanguageVO, "Localization.Language.VOF"},
		{[]string{"Localization.Language.vof"}, schedule.LanguageVO, "Localization.Language.vof"},
		{[]string{"Localization.Language.French", "Localization.Language.VOF"}, schedule.LanguageVO, "Localization.Language.VOF"},
		{[]string{"Showtime.Accessibility.Dubbed", "Localization.Language.VOF"}, schedule.LanguageVO, "Localization.Language.VOF"},
		{[]string{"Localization.Version.Original", "Localization.Language.VOF"}, schedule.LanguageVO, "Localization.Language.VOF"},
		{[]string{"Localization.Language.VOF", "Showtime.Accessibility.OpenCaption"}, schedule.LanguageVOSTFR, "Localization.Language.VOF"},
		{[]string{"Localization.Language.French", "Localization.Language.VOF", "Showtime.Accessibility.OpenCaption"}, schedule.LanguageVOSTFR, "Localization.Language.VOF"},
	} {
		t.Run(strings.Join(tc.tags, "+"), func(t *testing.T) {
			s := raw[c.ID].Schedule["1"]["2026-09-14"][0]
			s.Tags = append(tc.tags, "Format.Projection.3d", "Auditorium.Experience.DolbyAtmos")
			r, err := parseShowtime(s, c, movies["1"], "2026-09-14", location)
			if err != nil || r.Language != tc.want || r.ProviderVersion != tc.version || r.Format != schedule.FormatDolby {
				t.Fatalf("record=%+v err=%v", r, err)
			}
		})
	}
}
