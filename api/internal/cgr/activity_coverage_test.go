package cgr

import (
	"messeances/api/internal/schedule"
	"testing"
	"time"
)

func TestActivityAcquisitionCoverage(t *testing.T) {
	location, _ := time.LoadLocation(schedule.Timezone)
	program := map[string][]string{"1": {"2026-09-14"}, "2": {"2026-09-14"}}
	movies := map[string]movie{"1": {id: "1"}, "2": {id: "2"}}
	for _, tc := range []struct {
		body    string
		unknown bool
	}{{`{"W8010":{"schedule":{"1":{"2026-09-14":[]},"2":{"2026-09-14":[]}}}}`, false}, {`{"W8010":{"schedule":{"1":{"2026-09-14":[]},"2":{}}}}`, true}} {
		unknown := map[string]bool{}
		rows, err := parseSchedule([]byte(tc.body), cinema{id: "W8010", timeZone: schedule.Timezone}, program, movies, location, "2026-09-14", unknown)
		if err != nil || len(rows) != 0 || unknown["2026-09-14"] != tc.unknown {
			t.Fatal(tc, unknown, err)
		}
		coverage := schedule.ProgramCoverage("cgr-W8010", program, unknown)
		if len(coverage) != 1 || (coverage[0].Status == schedule.CoverageUnknown) != tc.unknown {
			t.Fatal(coverage)
		}
	}
}
