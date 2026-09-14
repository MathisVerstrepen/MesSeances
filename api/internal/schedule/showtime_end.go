package schedule

import "time"

// Estimates are response-only fallbacks. Canonical ends and source records remain unchanged.
func estimateShowtimeEnd(showtime Showtime, adsMinutes int) (*time.Time, *int) {
	if !validShowtimeTime(showtime.StartTime) || !showtime.EndTime.Equal(showtime.StartTime) || adsMinutes < 0 || adsMinutes > 120 {
		return nil, nil
	}
	runtime, ok := RuntimeDuration(showtime.Movie.RuntimeMinutes)
	if !ok {
		return nil, nil
	}
	ads := time.Duration(adsMinutes) * time.Minute
	if runtime > time.Duration(1<<63-1)-ads {
		return nil, nil
	}
	end := showtime.StartTime.Add(runtime + ads).UTC()
	if !validShowtimeTime(end) || !end.After(showtime.StartTime) {
		return nil, nil
	}
	return &end, &adsMinutes
}

func validShowtimeTime(value time.Time) bool {
	return !value.IsZero() && value.UTC().Year() >= 1 && value.UTC().Year() <= 9999
}

func usableShowtimeEnd(showtime Showtime) (time.Time, bool) {
	if !validShowtimeTime(showtime.StartTime) || !validShowtimeTime(showtime.EndTime) {
		return time.Time{}, false
	}
	if showtime.EndTime.After(showtime.StartTime) {
		return showtime.EndTime, true
	}
	if showtime.EndTime.Equal(showtime.StartTime) && showtime.EstimatedEndTime != nil && showtime.EstimatedEndAdsMinutes != nil && *showtime.EstimatedEndAdsMinutes >= 0 && *showtime.EstimatedEndAdsMinutes <= 120 && validShowtimeTime(*showtime.EstimatedEndTime) && showtime.EstimatedEndTime.After(showtime.StartTime) {
		return *showtime.EstimatedEndTime, true
	}
	return time.Time{}, false
}

func showtimeDurationMinutes(showtime Showtime) int {
	end, ok := usableShowtimeEnd(showtime)
	if !ok {
		return 0
	}
	return int(end.Sub(showtime.StartTime) / time.Minute)
}
