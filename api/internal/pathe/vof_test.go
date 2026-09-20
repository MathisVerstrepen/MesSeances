package pathe

import (
	"encoding/json"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestParseSessionVOF(t *testing.T) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"VOF", "vof", " VoF "} {
		t.Run(version, func(t *testing.T) {
			s := sessionResponse{Time: "2026-08-15 20:00:00", Version: version, Tags: []string{"3d", "imax"}, RefCmd: "https://s.pathe.fr/fr/V1S42/booking", AuditoriumName: json.RawMessage(`"1"`)}
			r, err := parseSession(s, show{slug: "film", title: "Synthetic", runtime: 90}, cinema{slug: "lille"}, "2026-08-15", location)
			if err != nil || r.Language != schedule.LanguageVO || r.ProviderVersion != "vof" || r.Format != schedule.FormatIMAX {
				t.Fatalf("record=%+v err=%v", r, err)
			}
		})
	}
}
