package schedule

import "time"

func ParseServiceDate(value string) (time.Time, error) {
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		return time.Time{}, err
	}
	return time.ParseInLocation(dateLayout, value, location)
}

func FormatServiceDate(value time.Time) string { return value.Format(dateLayout) }
