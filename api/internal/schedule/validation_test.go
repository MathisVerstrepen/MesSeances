package schedule

import (
	"strings"
	"testing"
	"time"
)

func TestValidInclusiveDateWindowAcrossParisDST(t *testing.T) {
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 18, 0, 0, 0, 0, location)
	if !ValidInclusiveDateWindow(from, from.AddDate(0, 0, 13)) {
		t.Fatal("14 inclusive calendar days rejected")
	}
	if ValidInclusiveDateWindow(from, from.AddDate(0, 0, 14)) {
		t.Fatal("15 inclusive calendar days accepted")
	}
	data := testDataset()
	data.Window = Window{From: "2026-10-18", Through: "2026-10-31"}
	for i := range data.Theaters {
		data.Theaters[i].AvailableDates = []string{"2026-10-18"}
	}
	for i := range data.Showtimes {
		local := data.Showtimes[i].StartTime.In(location)
		startDate := from
		if local.Hour() <= 2 {
			startDate = from.AddDate(0, 0, 1)
		}
		start := time.Date(startDate.Year(), startDate.Month(), startDate.Day(), local.Hour(), local.Minute(), 0, 0, location)
		data.Showtimes[i].ServiceDate = "2026-10-18"
		data.Showtimes[i].StartTime = start
		data.Showtimes[i].EndTime = start.Add(time.Duration(data.Showtimes[i].Movie.RuntimeMinutes) * time.Minute)
	}
	if err := ValidateDataset(data, true); err != nil {
		t.Fatalf("valid dataset rejected: %v", err)
	}
	data.Window.Through = "2026-11-01"
	if err := ValidateDataset(data, true); err == nil || err.Error() != "invalid dataset window" {
		t.Fatalf("error=%v", err)
	}
}

func TestValidateDatasetResourceAndRuntimeBounds(t *testing.T) {
	if validDatasetRecordCounts(MaxTheaters+1, 1) || validDatasetRecordCounts(1, MaxShowtimes+1) || !validDatasetRecordCounts(MaxTheaters, MaxShowtimes) {
		t.Fatal("record limits inconsistent")
	}
	data := testDataset()
	data.Theaters[0].Name = strings.Repeat("x", maxNameAndTitleLength+1)
	if err := ValidateDataset(data, true); err == nil || err.Error() != "theater field limit exceeded" {
		t.Fatalf("error=%v", err)
	}
	data = testDataset()
	data.Theaters[0].AvailableDates = make([]string, MaxAdvertisedDatesPerTheater+1)
	if err := ValidateDataset(data, true); err == nil || err.Error() != "theater available date limit exceeded" {
		t.Fatalf("error=%v", err)
	}
	data = testDataset()
	showing := &data.Showtimes[0]
	showing.Movie.RuntimeMinutes = MaxRuntimeMinutes
	showing.EndTime = showing.StartTime.Add(10 * time.Hour)
	if err := ValidateDataset(data, true); err != nil {
		t.Fatalf("maximum runtime rejected: %v", err)
	}
	showing.EndTime = showing.StartTime.Add(9*time.Hour + 59*time.Minute)
	if err := ValidateDataset(data, true); err == nil || err.Error() != "invalid showing times" {
		t.Fatalf("error=%v", err)
	}
}

func TestValidateDatasetScopeIdentityAndURLs(t *testing.T) {
	data := testDataset()
	data.Scope = ScopeSingle
	if err := ValidateDataset(data, true); err == nil {
		t.Fatal("single scope accepted as complete")
	}
	if err := ValidateDataset(data, false); err != nil {
		t.Fatalf("single scope rejected for diagnostics: %v", err)
	}
	data = testDataset()
	data.Showtimes[0].BookingURL = "https://evil.example/reservationSeances.html?id=100"
	if err := ValidateDataset(data, true); err == nil {
		t.Fatal("untrusted URL accepted")
	}
}
