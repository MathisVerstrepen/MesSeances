package noecinemas

import (
	"messeances/api/internal/schedule"
	"testing"
	"time"
)

func TestActivityAcquisitionCoverage(t *testing.T) {
	location, _ := time.LoadLocation(schedule.Timezone)
	program := map[string][]string{"1": {"2026-09-14"}, "2": {"2026-09-14"}}
	movies := map[string]schedule.MovieRecord{"1": {ProviderID: "1"}, "2": {ProviderID: "2"}}
	for _, tc := range []struct {
		body    string
		unknown bool
	}{{`{"P0867":{"schedule":{"1":{"2026-09-14":[]},"2":{"2026-09-14":[]}}}}`, true}, {`{"P0867":{"schedule":{"1":{"2026-09-14":[]},"2":{}}}}`, true}} {
		unknown := map[string]bool{}
		rows, err := parseSchedule([]byte(tc.body), cinema{ID: "P0867", TimeZone: schedule.Timezone}, program, movies, location, "2026-09-14", unknown)
		if err != nil || len(rows) != 0 || unknown["2026-09-14"] != tc.unknown {
			t.Fatal(tc, unknown, err)
		}
		coverage := schedule.ProgramCoverage("noecinemas-P0867", program, unknown)
		if len(coverage) != 1 || (coverage[0].Status == schedule.CoverageUnknown) != tc.unknown {
			t.Fatal(coverage)
		}
	}
	f := fixture(t)
	cinemas, _ := parseCinemas(f.cinemas)
	for _, c := range cinemas {
		if c.ID != "P8088" {
			continue
		}
		program, _ := parseProgram(f.programs[c.ID], "2026-09-14", location)
		movies, _ := parseMovies(f.movies)
		unknown := map[string]bool{}
		if _, err := parseSchedule(f.schedules[c.ID], c, program, movies, location, "2026-09-14", unknown); err != nil || len(unknown) != 0 {
			t.Fatal("ordinary response failed coverage", unknown, err)
		}
	}
}
