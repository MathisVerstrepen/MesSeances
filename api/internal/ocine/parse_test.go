package ocine

import (
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestSyncRetainsNonBookableFutureSessions(t *testing.T) {
	for _, test := range []struct {
		name, tags, version string
		language            schedule.Language
	}{
		{name: "unknown-version"},
		{name: "VF", tags: `<img class="tag-VF">`, version: "VF", language: schedule.LanguageVF},
		{name: "VO", tags: `<img class="tag-VO">`, version: "VOSTF", language: schedule.LanguageVOSTFR},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := fullFixture()
			for _, id := range cinemaIDs {
				// Ociné also publishes future div sessions without a past class or booking link.
				rows := `<div class="seance hr_seance sc_mor"><span class="hor">18h00</span><img class="tag-SAL04">` + test.tags + `</div>` + fixtureSession(id, "123", "VF", "4", "21h00")
				g.grids[id] = fixtureGrid(id, fixtureFilm(id, "123", rows))
			}
			d, summary, err := Sync(t.Context(), g, fixtureOptions())
			if err != nil {
				t.Fatal(err)
			}
			if len(d.Theaters) != 3 || len(d.Showtimes) != 6 || summary.EndsAttempted != 3 || summary.Requests != 9 {
				t.Fatalf("non-bookable sessions changed census or triggered checkout: %+v", summary)
			}
			for _, s := range d.Showtimes {
				if s.BookingURL != "" {
					if !s.StartTime.Equal(fixtureInstant("21h00")) || s.EndTime.Sub(s.StartTime) != 100*time.Minute {
						t.Fatal("bookable session lost checkout facts")
					}
					continue
				}
				if !s.StartTime.Equal(fixtureInstant("18h00")) || !s.EndTime.Equal(s.StartTime) || s.Room != "Salle 4" || s.Language != test.language || s.ProviderVersion != test.version || s.Format != schedule.Format3D || !strings.HasSuffix(s.ProviderShowingID, "-past") {
					t.Fatalf("non-bookable session lost advertised facts or gained invented ones: %+v", s)
				}
			}
			if _, err := schedule.PreparePublication(d); err != nil {
				t.Fatal("non-bookable future sessions cannot be published", err)
			}
		})
	}
}

func TestNonBookableSessionRejectsMalformedStructure(t *testing.T) {
	for _, test := range []struct{ name, row string }{
		{"anchor-without-booking", `<a class="seance hr_seance"><span class="hor">18h00</span></a>`},
		{"nested-anchor", `<div class="seance hr_seance"><a href="/unexpected/"><span class="hor">18h00</span></a></div>`},
		{"unsupported-element", `<span class="seance hr_seance"><span class="hor">18h00</span></span>`},
		{"invalid-clock", `<div class="seance hr_seance"><span class="hor">25h00</span></div>`},
		{"conflicting-version", `<div class="seance hr_seance"><span class="hor">18h00</span><img class="tag-VF"><img class="tag-VO"></div>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := fullFixture()
			g.grids["309"] = fixtureGrid("309", fixtureFilm("309", "123", test.row))
			d, _, err := Sync(t.Context(), g, fixtureOptions())
			if err == nil || len(d.Showtimes) != 0 || len(d.Theaters) != 0 {
				t.Fatal("malformed session escaped atomic acquisition", err)
			}
		})
	}
}
