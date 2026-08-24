package syncschedule

import (
	"errors"
	"testing"
	"time"
)

func TestDefinitionsNormalizeAndRejectInvalidInput(t *testing.T) {
	location := mustParis(t)
	after := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		in   Definition
		want Definition
	}{
		{name: "daily", in: Definition{Kind: KindDaily, Time: "08:05"}, want: Definition{Kind: KindDaily, Time: "08:05"}},
		{name: "weekly canonical", in: Definition{Kind: KindWeekly, Time: "20:30", Weekdays: []string{"sun", "mon", "sun", "wed"}}, want: Definition{Kind: KindWeekly, Time: "20:30", Weekdays: []string{"mon", "wed", "sun"}}},
		{name: "cron collapsed", in: Definition{Kind: KindCron, Expression: "  5\t8  *  *  1-5 "}, want: Definition{Kind: KindCron, Expression: "5 8 * * 1-5"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parseDefinition(test.in, location, after)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.definition.Kind != test.want.Kind || parsed.definition.Time != test.want.Time || parsed.definition.Expression != test.want.Expression || !sameStrings(parsed.definition.Weekdays, test.want.Weekdays) {
				t.Fatalf("normalized=%+v want=%+v", parsed.definition, test.want)
			}
		})
	}

	invalid := []Definition{
		{},
		{Kind: KindDaily, Time: "8:05"},
		{Kind: KindDaily, Time: "08:05", Weekdays: []string{"mon"}},
		{Kind: KindWeekly, Time: "08:05"},
		{Kind: KindWeekly, Time: "08:05", Weekdays: []string{"monday"}},
		{Kind: KindCron, Expression: "@daily"},
		{Kind: KindCron, Expression: "0 8 * *"},
		{Kind: KindCron, Expression: "0 0 8 * * *"},
		{Kind: KindCron, Expression: "TZ=UTC 0 8 * * *"},
		{Kind: KindCron, Expression: "CRON_TZ=UTC 0 8 * * *"},
		{Kind: KindCron, Expression: "0 8 30 2 *"},
		{Kind: KindCron, Expression: "0 8 * * *", Time: "08:00"},
		{Kind: KindCron, Expression: string(make([]byte, 256))},
	}
	for i, definition := range invalid {
		if _, err := parseDefinition(definition, location, after); !errors.Is(err, ErrInvalidSchedule) {
			t.Errorf("invalid[%d] err=%v", i, err)
		}
	}
}

func TestNextFiveUsesParisDSTSemantics(t *testing.T) {
	location := mustParis(t)
	springAfter := time.Date(2026, 3, 27, 0, 0, 0, 0, time.UTC)
	for _, definition := range []Definition{
		{Kind: KindDaily, Time: "02:30"},
		{Kind: KindCron, Expression: "30 2 * * *"},
	} {
		next, err := NextFive(definition, springAfter)
		if err != nil {
			t.Fatal(err)
		}
		for _, occurrence := range next {
			if occurrence.In(location).Format("2006-01-02") == "2026-03-29" {
				t.Fatalf("nonexistent spring occurrence returned: %v", occurrence)
			}
		}
	}
	weekly, err := NextFive(Definition{Kind: KindWeekly, Time: "02:30", Weekdays: []string{"sun"}}, time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if weekly[0].In(location).Format("2006-01-02 15:04") != "2026-03-22 02:30" || weekly[1].In(location).Format("2006-01-02 15:04") != "2026-04-05 02:30" {
		t.Fatalf("weekly spring occurrences=%v", weekly[:2])
	}

	fallAfter := time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC)
	for _, definition := range []Definition{
		{Kind: KindDaily, Time: "02:30"},
		{Kind: KindCron, Expression: "30 2 * * *"},
	} {
		next, err := NextFive(definition, fallAfter)
		if err != nil {
			t.Fatal(err)
		}
		if got := next[0].Format(time.RFC3339); got != "2026-10-25T02:30:00+02:00" {
			t.Fatalf("first fall occurrence=%s", got)
		}
		if got := next[1].Format(time.RFC3339); got != "2026-10-25T02:30:00+01:00" {
			t.Fatalf("second fall occurrence=%s", got)
		}
		if next[0].UTC().Equal(next[1].UTC()) {
			t.Fatal("fall occurrences share UTC identity")
		}
	}
}

func mustParis(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		t.Fatal(err)
	}
	return location
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
