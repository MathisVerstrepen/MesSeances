package grandecran

import (
	"messeances/api/internal/schedule"
	"testing"
)

func TestActivityAcquisitionCoverage(t *testing.T) {
	program := map[string][]string{"1": {"2026-09-14"}, "2": {"2026-09-14"}}
	movies := map[string]schedule.MovieRecord{"1": {ProviderID: "1"}, "2": {ProviderID: "2"}}
	for _, tc := range []struct {
		body    string
		unknown bool
	}{{`{"G028P":{"schedule":{"1":{"2026-09-14":[]},"2":{"2026-09-14":[]}}}}`, true}, {`{"G028P":{"schedule":{"1":{"2026-09-14":[]},"2":{}}}}`, true}} {
		unknown := map[string]bool{}
		rows, err := parseSchedule([]byte(tc.body), testCinema("G028P"), program, movies, testLocation(t), "2026-09-14", unknown)
		if err != nil || len(rows) != 0 || unknown["2026-09-14"] != tc.unknown {
			t.Fatal(tc, unknown, err)
		}
		coverage := schedule.ProgramCoverage("grandecran-G028P", program, unknown)
		if len(coverage) != 1 || (coverage[0].Status == schedule.CoverageUnknown) != tc.unknown {
			t.Fatal(coverage)
		}
	}
	unknown := map[string]bool{}
	r := scheduleResponse{"G028P": {Schedule: map[string]map[string][]showtimeResponse{"1": {"2026-09-14": {testSession(t, "one", "2026-09-14T20:00:00", "https://achat.grandecran.fr/test/r/123")}}, "2": {"2026-09-14": {testSession(t, "two", "2026-09-14T21:00:00", "https://achat.grandecran.fr/test/r/124")}}}}}
	if _, err := parseSchedule(testJSON(t, r), testCinema("G028P"), program, movies, testLocation(t), "2026-09-14", unknown); err != nil || len(unknown) != 0 {
		t.Fatal("ordinary response failed coverage", unknown, err)
	}
}
