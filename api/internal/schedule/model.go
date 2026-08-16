package schedule

import "time"

const (
	Timezone          = "Europe/Paris"
	MaxRuntimeMinutes = 600

	LanguageAll    = "ALL"
	LanguageVOSTFR = "VOSTFR"
	LanguageVF     = "VF"
)

func RuntimeDuration(minutes int) (time.Duration, bool) {
	if minutes <= 0 || minutes > MaxRuntimeMinutes {
		return 0, false
	}
	duration := time.Duration(minutes) * time.Minute
	return duration, duration > 0 && duration/time.Minute == time.Duration(minutes)
}

// ValidationError describes a query value that does not satisfy the schedule contract.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// NotFoundError describes a snapshot resource that does not exist.
type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string {
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
	StartOffsetMinutes int     `json:"start_offset_minutes"`
	DurationMinutes    int     `json:"duration_minutes"`
	BackdropURL        *string `json:"backdrop_url"`
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

type Theater struct {
	ID             string   `json:"id"`
	Slug           string   `json:"slug"`
	Name           string   `json:"name"`
	Address        string   `json:"address"`
	City           string   `json:"city"`
	PostalCode     string   `json:"postal_code"`
	AvailableDates []string `json:"available_dates"`
	AcceptedPasses []string `json:"accepted_passes"`
}

type MovieCatalogItem struct {
	Slug           string   `json:"slug"`
	Title          string   `json:"title"`
	RuntimeMinutes int      `json:"runtime_minutes"`
	PosterURL      *string  `json:"poster_url"`
	TMDBID         *int64   `json:"tmdb_id"`
	Overview       *string  `json:"overview"`
	ReleaseDate    *string  `json:"release_date"`
	Genres         []string `json:"genres"`
}

type MovieCatalog struct {
	Items    []MovieCatalogItem `json:"items"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Total    int                `json:"total"`
}

type MovieTheaterShowtimes struct {
	ID        string     `json:"id"`
	Slug      string     `json:"slug"`
	Name      string     `json:"name"`
	City      string     `json:"city"`
	Showtimes []Showtime `json:"showtimes"`
}

type MovieSchedule struct {
	Movie    MovieCatalogItem        `json:"movie"`
	Date     string                  `json:"date"`
	Theaters []MovieTheaterShowtimes `json:"theaters"`
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

type TheaterCatalogQuery struct {
	City  string
	Chain string
}

type MovieCatalogQuery struct {
	CurrentlyScreened *bool
	Search            string
	Page              int
	PageSize          int
}

type MovieShowtimesQuery struct {
	Slug       string
	Date       string
	City       string
	TheaterIDs []string
}

type SlotQuery struct {
	City         string
	TheaterIDs   []string
	Date         string
	StartAfter   string
	FinishBefore string
	BufferAds    int
	Language     string
}
