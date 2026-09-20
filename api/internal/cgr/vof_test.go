package cgr

import (
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestParseShowtimeVOF(t *testing.T) {
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
		{[]string{"Localization.Version.Original", "Localization.Language.VOF"}, schedule.LanguageVO, "Localization.Language.VOF"},
		{[]string{"Localization.Language.VOF", "Showtime.Accessibility.Subtitled"}, schedule.LanguageVOSTFR, "Localization.Language.VOF"},
		{[]string{"Localization.Language.French", "Localization.Language.VOF", "Showtime.Accessibility.Subtitled"}, schedule.LanguageVOSTFR, "Localization.Language.VOF"},
	} {
		t.Run(strings.Join(tc.tags, "+"), func(t *testing.T) {
			s := showtimeResponse{ID: "synthetic", StartsAt: "2026-08-27T20:00:00", Tags: append(tc.tags, "Format.Projection.3d", "Auditorium.Experience.Ice"), Data: showtimeData{Ticketing: []byte(`[{"provider":"default","type":"DESKTOP","urls":["https://achat.cgrcinemas.fr/synthetic/r/1"]}]`)}}
			r, err := parseShowtime(s, cinema{id: "W8010", timeZone: schedule.Timezone}, movie{id: "1001", title: "Synthetic", runtime: 90}, "2026-08-27", location)
			if err != nil || r.Language != tc.want || r.ProviderVersion != tc.version || r.Format != schedule.FormatICE {
				t.Fatalf("record=%+v err=%v", r, err)
			}
		})
	}
}
