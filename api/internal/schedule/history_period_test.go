package schedule

import (
	"testing"
	"time"
)

func TestHistoryPeriodParisBounds(t *testing.T) {
	for _, tc := range []struct {
		name, now, today, next7, next30, weekFrom, weekThrough string
	}{
		{"Tuesday", "2026-10-20T12:00:00Z", "2026-10-20", "2026-10-26", "2026-11-18", "2026-10-14", "2026-10-20"},
		{"before Paris Wednesday", "2026-10-20T21:59:59Z", "2026-10-20", "2026-10-26", "2026-11-18", "2026-10-14", "2026-10-20"},
		{"Paris Wednesday UTC Tuesday", "2026-10-20T22:00:00Z", "2026-10-21", "2026-10-27", "2026-11-19", "2026-10-21", "2026-10-27"},
		{"spring DST", "2026-03-28T23:30:00Z", "2026-03-29", "2026-04-04", "2026-04-27", "2026-03-25", "2026-03-31"},
		{"autumn DST before", "2026-10-25T00:30:00Z", "2026-10-25", "2026-10-31", "2026-11-23", "2026-10-21", "2026-10-27"},
		{"autumn DST after", "2026-10-25T01:30:00Z", "2026-10-25", "2026-10-31", "2026-11-23", "2026-10-21", "2026-10-27"},
		{"year end", "2025-12-30T23:00:00Z", "2025-12-31", "2026-01-06", "2026-01-29", "2025-12-31", "2026-01-06"},
		{"leap day", "2024-02-28T23:00:00Z", "2024-02-29", "2024-03-06", "2024-03-29", "2024-02-28", "2024-03-05"},
		{"nonleap February", "2026-02-27T23:00:00Z", "2026-02-28", "2026-03-06", "2026-03-29", "2026-02-25", "2026-03-03"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			for _, bounds := range []struct {
				period        HistoryPeriod
				from, through string
			}{
				{HistoryNext7, tc.today, tc.next7},
				{HistoryNext30, tc.today, tc.next30},
				{HistoryWeek, tc.weekFrom, tc.weekThrough},
				{HistoryAll, "", ""},
			} {
				t.Run(string(bounds.period), func(t *testing.T) {
					got, err := HistoryPeriodQuery(bounds.period, now)
					if err != nil || got.Date != bounds.from || got.DateTo != bounds.through {
						t.Fatal(got, err, bounds)
					}
				})
			}
		})
	}
	for _, period := range []HistoryPeriod{"", "custom", "NEXT7", " next7 "} {
		if _, err := HistoryPeriodQuery(period, time.Now()); err == nil {
			t.Fatal("accepted invalid period", period)
		}
	}
}
