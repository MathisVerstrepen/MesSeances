package schedule

import "time"

// upcomingReleaseWeek returns the Wednesday key for a snapshot-validated date.
// Date-only UTC calendar arithmetic avoids timezone and DST shifts.
func upcomingReleaseWeek(date string) string {
	day, _ := time.Parse(time.DateOnly, date)
	days := (int(day.Weekday()) - int(time.Wednesday) + 7) % 7
	return day.AddDate(0, 0, -days).Format(time.DateOnly)
}

// UpcomingWindow excludes Paris today and clamps the next calendar anniversary.
func UpcomingWindow(now time.Time) Window {
	location, _ := time.LoadLocation(Timezone)
	year, month, day := now.In(location).Date()
	today := time.Date(year, month, day, 0, 0, 0, 0, location)
	lastDay := time.Date(year+1, month+1, 0, 0, 0, 0, 0, location).Day()
	through := time.Date(year+1, month, min(day, lastDay), 0, 0, 0, 0, location)
	return Window{From: today.AddDate(0, 0, 1).Format(time.DateOnly), Through: through.Format(time.DateOnly)}
}

// UpcomingDisplayWindow hides every Wednesday-Tuesday week that has started in
// Paris, retaining the import window's inclusive calendar-anniversary upper bound.
func UpcomingDisplayWindow(now time.Time) Window {
	window := UpcomingWindow(now)
	location, _ := time.LoadLocation(Timezone)
	local := now.In(location)
	year, month, day := local.Date()
	today := time.Date(year, month, day, 0, 0, 0, 0, location)
	days := (int(time.Wednesday) - int(today.Weekday()) + 7) % 7
	if days == 0 {
		days = 7
	}
	window.From = today.AddDate(0, 0, days).Format(time.DateOnly)
	return window
}
