package schedule

import "time"

type HistoryPeriod string

const (
	HistoryNext7  HistoryPeriod = "next7"
	HistoryNext30 HistoryPeriod = "next30"
	HistoryWeek   HistoryPeriod = "week"
	HistoryAll    HistoryPeriod = "all"
)

func HistoryPeriods() [4]HistoryPeriod {
	return [4]HistoryPeriod{HistoryNext7, HistoryNext30, HistoryWeek, HistoryAll}
}

// HistoryPeriodQuery resolves inclusive Paris calendar bounds at calculation
// time. Calendar arithmetic, rather than durations, preserves dates across DST.
func HistoryPeriodQuery(period HistoryPeriod, now time.Time) (StatisticsQuery, error) {
	if period == HistoryAll {
		return StatisticsQuery{}, nil
	}
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		return StatisticsQuery{}, err
	}
	local := now.In(location)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	days := 7
	switch period {
	case HistoryNext7:
	case HistoryNext30:
		days = 30
	case HistoryWeek:
		start = start.AddDate(0, 0, -(int(start.Weekday())-int(time.Wednesday)+7)%7)
	default:
		return StatisticsQuery{}, invalid("La période est invalide.")
	}
	return StatisticsQuery{Date: start.Format(time.DateOnly), DateTo: start.AddDate(0, 0, days-1).Format(time.DateOnly)}, nil
}
