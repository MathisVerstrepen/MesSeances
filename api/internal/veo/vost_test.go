package veo

import (
	"strings"
	"testing"

	"messeances/api/internal/schedule"
)

func TestSyncCasteliaVOST(t *testing.T) {
	for _, flags := range []struct{ name, features string }{
		{"version alone", `"video_3d"`}, {"ST feature", `"video_3d","ST"`}, {"subtitle feature", `"video_3d","subtitle"`},
	} {
		t.Run(flags.name, func(t *testing.T) {
			f := &fixtureGetter{modify: func(op Operation, _ string, b []byte) ([]byte, error) {
				if op == OperationCastelia {
					b = []byte(strings.NewReplacer(`"version":"VO"`, `"version":"VOST"`, `"features":["ST"]`, `"features":[`+flags.features+`]`, `https://images.ticketingcine.fr/assets/global/600/FR/fixture.jpg`, ``).Replace(string(b)))
				}
				return b, nil
			}}
			d, _, err := Sync(t.Context(), f, fixtureOptions())
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, r := range d.Showtimes {
				if r.TheaterID != "veo-EMS1361" {
					continue
				}
				found = true
				if r.Language != schedule.LanguageVOSTFR || r.ProviderVersion != "VOST" || r.Format != schedule.Format3D {
					t.Fatalf("unexpected VOST mapping: %+v", r)
				}
			}
			if !found {
				t.Fatal("missing VOST showing")
			}
		})
	}
}
