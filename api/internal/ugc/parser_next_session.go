package ugc

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func parseNextSessionDate(value string, location *time.Location) (time.Time, error) {
	match := nextSessionPattern.FindStringSubmatch(collapse(value))
	if len(match) != 5 {
		return time.Time{}, fmt.Errorf("invalid next session date")
	}
	day, dayErr := strconv.Atoi(match[2])
	year, yearErr := strconv.Atoi(match[4])
	month, monthOK := frenchMonths[strings.ToLower(match[3])]
	if dayErr != nil || yearErr != nil || !monthOK {
		return time.Time{}, fmt.Errorf("invalid next session date")
	}
	date := time.Date(year, month, day, 0, 0, 0, 0, location)
	if date.Year() != year || date.Month() != month || date.Day() != day {
		return time.Time{}, fmt.Errorf("invalid next session date")
	}
	if match[1] != "" {
		weekday, ok := frenchWeekdays[strings.ToLower(match[1])]
		if !ok || date.Weekday() != weekday {
			return time.Time{}, fmt.Errorf("invalid next session date")
		}
	}
	return date, nil
}
