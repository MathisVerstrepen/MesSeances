package noecinemas

import (
	"encoding/json"
	"messeances/api/internal/schedule"
	"strings"
	"testing"
	"time"
)

func TestParserBoundaries(t *testing.T) {
	l, _ := time.LoadLocation(schedule.Timezone)
	for _, raw := range []string{`{}`, `{"data":{"allTheater":{"nodes":[]}}}`, string(fixture(t).cinemas) + ` {}`} {
		if _, err := parseCinemas([]byte(raw)); err == nil {
			t.Fatal("bad discovery accepted")
		}
	}
	for _, raw := range []string{`{"scheduledDays":{"0":["2026-09-14"]}}`, `{"scheduledDays":{"1":["2026-02-30"]}}`, `{"movieIds":["1"],"scheduledDays":{"2":["2026-09-14"]}}`} {
		if _, err := parseProgram([]byte(raw), "2026-09-14", l); err == nil {
			t.Fatal("invalid program")
		}
	}
	for _, runtime := range []string{"-1", "0", "1.5", "\"120\""} {
		raw := `[{"id":"1","title":"Test","runtime":` + runtime + `}]`
		if _, err := parseMovies([]byte(raw)); err == nil {
			t.Fatalf("runtime %s accepted", runtime)
		}
	}
	for _, raw := range []string{"2026-03-29T02:30:00", "2026-09-14T20:00:00Z", "2026-02-30T20:00:00", "invalid"} {
		if _, ok := parseStartTime(raw, l); ok {
			t.Fatalf("bad time %s", raw)
		}
	}
	for _, raw := range []string{"2026-10-25T02:30:00+02:00", "2026-10-25T02:30:00+01:00", "2026-09-15T03:00:00"} {
		if _, ok := parseStartTime(raw, l); !ok {
			t.Fatalf("valid time %s rejected", raw)
		}
	}
}
func TestScheduleDuplicatesIdentityAndUnsafeRecords(t *testing.T) {
	f := fixture(t)
	cinemas, _ := parseCinemas(f.cinemas)
	var c cinema
	for _, item := range cinemas {
		if item.ID == "P8088" {
			c = item
		}
	}
	l, _ := time.LoadLocation(schedule.Timezone)
	p, _ := parseProgram(f.programs[c.ID], "2026-09-14", l)
	movies, _ := parseMovies(f.movies)
	for _, mode := range []string{"equal", "conflict", "blank-id", "nul-id", "long-id", "wrong-host", "wrong-cinema", "unknown-version", "duplicate-default", "03-boundary"} {
		t.Run(mode, func(t *testing.T) {
			var raw scheduleResponse
			if err := json.Unmarshal(f.schedules[c.ID], &raw); err != nil {
				t.Fatal(err)
			}
			r := raw[c.ID].Schedule["1"]["2026-09-14"][0]
			switch mode {
			case "equal", "conflict":
				raw[c.ID].Schedule["1"]["2026-09-14"] = append(raw[c.ID].Schedule["1"]["2026-09-14"], r)
				if mode == "conflict" {
					r.StartsAt = "2026-09-15T02:00:00"
				}
			case "blank-id":
				r.ID = " "
			case "nul-id":
				r.ID = "a\x00b"
			case "long-id":
				r.ID = strings.Repeat("x", 4097)
			case "wrong-host":
				r.Data.Ticketing[1].URLs = []string{"https://evil.test/reserver/r/123"}
			case "wrong-cinema":
				r.Data.Ticketing[1].URLs = []string{"https://achat.cinepal.fr/reserver/r/123"}
			case "unknown-version":
				r.Tags = []string{"Laser", "4k"}
			case "duplicate-default":
				r.Data.Ticketing = append(r.Data.Ticketing, r.Data.Ticketing[1])
			case "03-boundary":
				r.StartsAt = "2026-09-15T03:00:00"
			}
			raw[c.ID].Schedule["1"]["2026-09-14"][0] = r
			b, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			records, err := parseSchedule(b, c, p, movies, l, "")
			if mode == "equal" {
				if err != nil || len(records) != 2 {
					t.Fatal("equal duplicate rejected")
				}
				return
			}
			if err == nil {
				t.Fatal("invalid schedule accepted")
			}
		})
	}
}
