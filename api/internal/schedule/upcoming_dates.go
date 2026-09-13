package schedule

import "time"

// UpcomingWindow excludes Paris today and clamps the next calendar anniversary.
func UpcomingWindow(now time.Time) Window {
	location, _ := time.LoadLocation(Timezone)
	year, month, day := now.In(location).Date()
	today := time.Date(year, month, day, 0, 0, 0, 0, location)
	lastDay := time.Date(year+1, month+1, 0, 0, 0, 0, 0, location).Day()
	through := time.Date(year+1, month, min(day, lastDay), 0, 0, 0, 0, location)
	return Window{From: today.AddDate(0, 0, 1).Format(time.DateOnly), Through: through.Format(time.DateOnly)}
}
