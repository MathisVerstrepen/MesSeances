package syncschedule

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

var localTimePattern = regexp.MustCompile(`^(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)

var weekdayOrder = map[string]int{
	"mon": 1,
	"tue": 2,
	"wed": 3,
	"thu": 4,
	"fri": 5,
	"sat": 6,
	"sun": 7,
}

var cronWeekday = map[string]string{
	"mon": "1",
	"tue": "2",
	"wed": "3",
	"thu": "4",
	"fri": "5",
	"sat": "6",
	"sun": "0",
}

type parsedDefinition struct {
	definition Definition
	schedule   cron.Schedule
}

func parseDefinition(definition Definition, location *time.Location, after time.Time) (parsedDefinition, error) {
	normalized, expression, err := normalizeDefinition(definition)
	if err != nil {
		return parsedDefinition{}, err
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	parsed, err := parser.Parse(expression)
	if err != nil {
		return parsedDefinition{}, ErrInvalidSchedule
	}
	spec, ok := parsed.(*cron.SpecSchedule)
	if !ok {
		return parsedDefinition{}, ErrInvalidSchedule
	}
	spec.Location = location
	if _, err := nextOccurrences(parsed, after, location); err != nil {
		return parsedDefinition{}, err
	}
	return parsedDefinition{definition: normalized, schedule: parsed}, nil
}

func normalizeDefinition(definition Definition) (Definition, string, error) {
	switch definition.Kind {
	case KindDaily:
		if !localTimePattern.MatchString(definition.Time) || len(definition.Weekdays) != 0 || definition.Expression != "" {
			return Definition{}, "", ErrInvalidSchedule
		}
		hour, minute := splitLocalTime(definition.Time)
		return Definition{Kind: KindDaily, Time: definition.Time}, fmt.Sprintf("%s %s * * *", minute, hour), nil
	case KindWeekly:
		if !localTimePattern.MatchString(definition.Time) || len(definition.Weekdays) == 0 || definition.Expression != "" {
			return Definition{}, "", ErrInvalidSchedule
		}
		seen := make(map[string]struct{}, len(definition.Weekdays))
		weekdays := make([]string, 0, len(definition.Weekdays))
		for _, weekday := range definition.Weekdays {
			if _, ok := weekdayOrder[weekday]; !ok {
				return Definition{}, "", ErrInvalidSchedule
			}
			if _, ok := seen[weekday]; ok {
				continue
			}
			seen[weekday] = struct{}{}
			weekdays = append(weekdays, weekday)
		}
		sort.Slice(weekdays, func(i, j int) bool { return weekdayOrder[weekdays[i]] < weekdayOrder[weekdays[j]] })
		days := make([]string, len(weekdays))
		for i, weekday := range weekdays {
			days[i] = cronWeekday[weekday]
		}
		hour, minute := splitLocalTime(definition.Time)
		return Definition{Kind: KindWeekly, Time: definition.Time, Weekdays: weekdays}, fmt.Sprintf("%s %s * * %s", minute, hour, strings.Join(days, ",")), nil
	case KindCron:
		if definition.Time != "" || len(definition.Weekdays) != 0 {
			return Definition{}, "", ErrInvalidSchedule
		}
		expression := strings.Join(strings.Fields(definition.Expression), " ")
		if expression == "" || len(expression) > 255 {
			return Definition{}, "", ErrInvalidSchedule
		}
		fields := strings.Fields(expression)
		if len(fields) != 5 || strings.HasPrefix(fields[0], "TZ=") || strings.HasPrefix(fields[0], "CRON_TZ=") {
			return Definition{}, "", ErrInvalidSchedule
		}
		return Definition{Kind: KindCron, Expression: expression}, expression, nil
	default:
		return Definition{}, "", ErrInvalidSchedule
	}
}

func splitLocalTime(value string) (hour string, minute string) {
	parts := strings.Split(value, ":")
	return parts[0], parts[1]
}

func nextOccurrences(schedule cron.Schedule, after time.Time, location *time.Location) ([]time.Time, error) {
	next := make([]time.Time, 0, 5)
	cursor := after.In(location)
	for len(next) < 5 {
		cursor = schedule.Next(cursor)
		if cursor.IsZero() {
			return nil, ErrInvalidSchedule
		}
		next = append(next, cursor)
	}
	return next, nil
}

// NextFive returns five base occurrences strictly after after in Europe/Paris.
func NextFive(definition Definition, after time.Time) ([]time.Time, error) {
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		return nil, ErrInvalidSchedule
	}
	parsed, err := parseDefinition(definition, location, after)
	if err != nil {
		return nil, err
	}
	return nextOccurrences(parsed.schedule, after, location)
}
