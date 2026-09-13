package schedule

import (
	"testing"
	"time"
)

func TestUpcomingDisplayWindow(t *testing.T) {
	for _, test := range []struct{ name, now, from, through string }{
		{"Thursday", "2026-09-10T12:00:00Z", "2026-09-16", "2027-09-10"},
		{"Friday", "2026-09-11T12:00:00Z", "2026-09-16", "2027-09-11"},
		{"Saturday", "2026-09-12T12:00:00Z", "2026-09-16", "2027-09-12"},
		{"Sunday", "2026-09-13T12:00:00Z", "2026-09-16", "2027-09-13"},
		{"Monday", "2026-09-14T12:00:00Z", "2026-09-16", "2027-09-14"},
		{"Tuesday", "2026-09-15T21:59:59Z", "2026-09-16", "2027-09-15"},
		{"Wednesday Paris midnight before UTC", "2026-09-15T22:00:00Z", "2026-09-23", "2027-09-16"},
		{"Wednesday UTC midnight", "2026-09-16T00:00:00Z", "2026-09-23", "2027-09-16"},
		{"month boundary", "2026-08-30T12:00:00Z", "2026-09-02", "2027-08-30"},
		{"year boundary", "2026-12-30T12:00:00Z", "2027-01-06", "2027-12-30"},
		{"leap anniversary clamp", "2028-02-29T12:00:00Z", "2028-03-01", "2029-02-28"},
		{"before leap year", "2027-02-28T12:00:00Z", "2027-03-03", "2028-02-28"},
		{"before spring DST", "2026-03-28T23:00:00Z", "2026-04-01", "2027-03-29"},
		{"after spring DST", "2026-03-29T01:00:00Z", "2026-04-01", "2027-03-29"},
		{"before autumn DST", "2026-10-24T22:00:00Z", "2026-10-28", "2027-10-25"},
		{"after autumn DST", "2026-10-25T01:00:00Z", "2026-10-28", "2027-10-25"},
		{"winter Tuesday", "2026-12-29T22:59:59Z", "2026-12-30", "2027-12-29"},
		{"winter Wednesday midnight", "2026-12-29T23:00:00Z", "2027-01-06", "2027-12-30"},
	} {
		t.Run(test.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, test.now)
			if err != nil {
				t.Fatal(err)
			}
			got := UpcomingDisplayWindow(now)
			if got != (Window{From: test.from, Through: test.through}) {
				t.Fatalf("window=%+v", got)
			}
			if got.Through != UpcomingWindow(now).Through {
				t.Fatal("display changed import upper bound")
			}
		})
	}
}
