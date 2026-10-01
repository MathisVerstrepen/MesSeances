package pathe

import (
	"messeances/api/internal/schedule"
	"testing"
	"time"
)

func TestActivityAcquisitionCoverage(t *testing.T) {
	location, _ := time.LoadLocation(schedule.Timezone)
	job := showtimeJob{operation: OperationMovieTimes, theater: cinema{slug: "lille"}, movie: show{slug: "film-a"}, dates: []string{"2026-08-15", "2026-08-16"}}
	for _, tc := range []struct {
		body    string
		unknown bool
	}{{`{"2026-08-15":[],"2026-08-16":[]}`, false}, {`{"2026-08-15":[]}`, true}, {`{}`, true}} {
		unknown := map[string]bool{}
		rows, err := parseShowtimeResponse([]byte(tc.body), job, location, unknown)
		if err != nil || len(rows) != 0 || unknown["2026-08-16"] != tc.unknown {
			t.Fatal(tc, unknown, err)
		}
	}
	_, err := parseShowtimeResponse([]byte(`null`), job, location, map[string]bool{})
	if err == nil {
		t.Fatal("invalid response certified")
	}
}
