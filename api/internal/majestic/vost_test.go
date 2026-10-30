package majestic

import (
	"testing"

	"messeances/api/internal/schedule"
)

func TestSyncVOST(t *testing.T) {
	for _, flags := range []struct {
		name, formats string
		features      []string
	}{
		{name: "version alone"}, {name: "ST format", formats: "ST"}, {name: "subtitle feature", features: []string{"subtitle"}},
	} {
		t.Run(flags.name, func(t *testing.T) {
			g := fixtures(t)
			p, err := parseProgram(g.bodies[sites[0]], sites[0])
			if err != nil {
				t.Fatal(err)
			}
			s := &p.Events[0].Sessions[0]
			s.Version, s.Formats, s.Features = "VOST", flags.formats, append(flags.features, "video_3d")
			setProgram(t, g, sites[0], p)
			d, _, err := runFixture(t, g)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, r := range d.Showtimes {
				if r.TheaterID != "majestic-"+sites[0] {
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
