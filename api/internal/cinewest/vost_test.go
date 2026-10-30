package cinewest

import (
	"testing"

	"messeances/api/internal/schedule"
)

func TestSyncTicketVOST(t *testing.T) {
	for _, flags := range []struct {
		name, formats string
		features      []string
	}{
		{name: "version alone"}, {name: "ST format", formats: "ST"}, {name: "subtitle feature", features: []string{"subtitle"}},
	} {
		t.Run(flags.name, func(t *testing.T) {
			f := newFixture()
			p := fixtureTicketProgram(t, f, "EMS1378")
			s := &p.Events[0].Sessions[0]
			s.Version, s.Formats, s.Features = "VOST", flags.formats, append(flags.features, "video_imax", "video_3d")
			setFixtureTicketProgram(f, "EMS1378", p)
			d, err := syncFixture(t, f)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, r := range d.Showtimes {
				if r.TheaterID != "cinewest-ticketingcine-EMS1378" {
					continue
				}
				found = true
				if r.Language != schedule.LanguageVOSTFR || r.ProviderVersion != "VOST" || r.Format != schedule.FormatIMAX {
					t.Fatalf("unexpected VOST mapping: %+v", r)
				}
			}
			if !found {
				t.Fatal("missing VOST showing")
			}
		})
	}
}
