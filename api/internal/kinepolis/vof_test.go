package kinepolis

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestParseScheduleVOF(t *testing.T) {
	for _, tc := range []struct {
		attributes []string
		want       schedule.Language
	}{
		{[]string{"VOF"}, schedule.LanguageVO},
		{[]string{"vof"}, schedule.LanguageVO},
		{[]string{"FR", "VOF", "VF"}, schedule.LanguageVO},
		{[]string{"VOF", "VF SME"}, schedule.LanguageVOSTFR},
		{[]string{"VOF", "VOSTFR"}, schedule.LanguageVOSTFR},
		{[]string{"VOF", "Sous-titré français"}, schedule.LanguageVOSTFR},
		{[]string{"VOF", "Sous-tîtres : Français"}, schedule.LanguageVOSTFR},
	} {
		t.Run(strings.Join(tc.attributes, "+"), func(t *testing.T) {
			attributes, err := json.Marshal(tc.attributes)
			if err != nil {
				t.Fatal(err)
			}
			body := strings.Replace(string(fixture(t)), `["Version originale","Sous-titré français"]`, string(attributes), 1)
			d, _, err := parseSchedule([]byte(body), "2026-08-15", time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))
			if err != nil || len(d.Showtimes) != 2 {
				t.Fatalf("dataset=%+v err=%v", d, err)
			}
			r := d.Showtimes[0]
			if r.Language != tc.want || r.ProviderVersion != strings.Join(tc.attributes, " ") || r.Format != schedule.FormatIMAX {
				t.Fatalf("record=%+v", r)
			}
		})
	}
}
