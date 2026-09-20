package grandecran

import (
	"strings"
	"testing"

	"messeances/api/internal/schedule"
)

func TestParseShowtimeVOF(t *testing.T) {
	for _, tc := range []struct {
		tags    []string
		version string
	}{
		{[]string{"Localization.Language.VOF"}, "Localization.Language.VOF"},
		{[]string{"Localization.Language.vof"}, "Localization.Language.vof"},
		{[]string{"Localization.Language.French", "Localization.Language.VOF"}, "Localization.Language.VOF"},
		{[]string{"Localization.Version.Original", "Localization.Language.VOF"}, "Localization.Language.VOF"},
	} {
		t.Run(strings.Join(tc.tags, "+"), func(t *testing.T) {
			s := testSession(t, "synthetic", "2026-09-14T20:00:00", "https://achat.grandecran.fr/test/r/123")
			s.Tags = append(tc.tags, "Format.Projection.3d", "Auditorium.Experience.DolbyAtmos")
			m := schedule.MovieRecord{Provider: schedule.ProviderGrandEcran, ProviderID: "cEvent_1", Slug: "grandecran-film-cEvent_1", Title: "Synthetic"}
			r, err := parseShowtime(s, testCinema("G028P"), m, "2026-09-14", testLocation(t))
			if err != nil || r.Language != schedule.LanguageVO || r.ProviderVersion != tc.version || r.Format != schedule.FormatDolby {
				t.Fatalf("record=%+v err=%v", r, err)
			}
		})
	}
}
