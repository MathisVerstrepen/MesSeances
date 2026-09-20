package cineville

import (
	"testing"

	"messeances/api/internal/schedule"
)

func TestSyncVOF(t *testing.T) {
	for _, subtitles := range []scalar{"0", "false", "1", "true"} {
		t.Run(string(subtitles), func(t *testing.T) {
			p := fixturePage(fixtureCinema())
			s := &p.Program[0].Dates[0].Showtimes[0]
			s.Version, s.Subtitles, s.Attributes = "VOF", subtitles, "32,21,10008"
			d, _, err := Sync(t.Context(), singleFetcher(t, p), fixtureOptions())
			if err != nil || len(d.Showtimes) != 1 {
				t.Fatalf("dataset=%+v err=%v", d, err)
			}
			want := schedule.LanguageVO
			if subtitles == "1" || subtitles == "true" {
				want = schedule.LanguageVOSTFR
			}
			r := d.Showtimes[0]
			if r.Language != want || r.ProviderVersion != "VOF" || r.Format != schedule.FormatInfinityVision {
				t.Fatalf("record=%+v", r)
			}
		})
	}
	for _, s := range []session{{Version: "vof", Subtitles: "0", Relief: "2D"}, {Version: "VOF", Subtitles: "unknown", Relief: "2D"}} {
		if _, _, err := parseAttributes(s); err == nil {
			t.Fatal("malformed attributes accepted")
		}
	}
}
