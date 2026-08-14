package schedule

import "time"

const (
	Timezone = "Europe/Paris"

	LanguageAll    = "ALL"
	LanguageVOSTFR = "VOSTFR"
	LanguageVF     = "VF"
)

// ValidationError describes a query value that does not satisfy the schedule contract.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

type Movie struct {
	Slug           string `json:"slug"`
	Title          string `json:"title"`
	RuntimeMinutes int    `json:"runtime_minutes"`
}

type Showtime struct {
	ID         string    `json:"id"`
	Movie      Movie     `json:"movie"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	Language   string    `json:"language"`
	Format     string    `json:"format"`
	Room       string    `json:"room"`
	BookingURL *string   `json:"booking_url"`
}

type TimelineShowtime struct {
	Showtime
	StartOffsetMinutes int `json:"start_offset_minutes"`
	DurationMinutes    int `json:"duration_minutes"`
}

type TimelineTheater struct {
	ID             string             `json:"id"`
	Slug           string             `json:"slug"`
	Name           string             `json:"name"`
	City           string             `json:"city"`
	AcceptedPasses []string           `json:"accepted_passes"`
	Showtimes      []TimelineShowtime `json:"showtimes"`
}

type Timeline struct {
	Date            string            `json:"date"`
	Timezone        string            `json:"timezone"`
	WindowStartTime time.Time         `json:"window_start_time"`
	WindowEndTime   time.Time         `json:"window_end_time"`
	Theaters        []TimelineTheater `json:"theaters"`
}

type TheaterSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	City string `json:"city"`
}

type SlotResult struct {
	Showtime           Showtime       `json:"showtime"`
	Theater            TheaterSummary `json:"theater"`
	EffectiveEndTime   time.Time      `json:"effective_end_time"`
	BufferAdsMinutes   int            `json:"buffer_ads_minutes"`
	SlackBeforeMinutes int            `json:"slack_before_minutes"`
	SlackAfterMinutes  int            `json:"slack_after_minutes"`
}

type TimelineQuery struct {
	Date       string
	TheaterIDs []string
	Language   string
}

type SlotQuery struct {
	City         string
	Date         string
	StartAfter   string
	FinishBefore string
	BufferAds    int
	Language     string
}
