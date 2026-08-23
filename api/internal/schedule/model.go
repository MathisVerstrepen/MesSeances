package schedule

import (
	"math"
	"time"
)

const (
	Timezone = "Europe/Paris"
)

type MovieCatalogSort string

const (
	MovieCatalogSortTitleAsc        MovieCatalogSort = "title_asc"
	MovieCatalogSortTitleDesc       MovieCatalogSort = "title_desc"
	MovieCatalogSortReleaseDateDesc MovieCatalogSort = "release_date_desc"
	MovieCatalogSortRuntimeAsc      MovieCatalogSort = "runtime_asc"
	MovieCatalogSortRuntimeDesc     MovieCatalogSort = "runtime_desc"
	MovieCatalogSortShowtimesDesc   MovieCatalogSort = "showtimes_desc"
)

func RuntimeDuration(minutes int) (time.Duration, bool) {
	if minutes <= 0 || uint64(minutes) > uint64(math.MaxInt64)/uint64(time.Minute) {
		return 0, false
	}
	duration := time.Duration(int64(minutes) * int64(time.Minute))
	return duration, true
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
	Slug           string    `json:"slug"`
	Title          string    `json:"title"`
	RuntimeMinutes int       `json:"runtime_minutes"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Showtime struct {
	Provider   Provider  `json:"provider"`
	ID         string    `json:"id"`
	Movie      Movie     `json:"movie"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	Language   Language  `json:"language"`
	Format     Format    `json:"format"`
	Room       string    `json:"room"`
	BookingURL *string   `json:"booking_url"`
}

type TimelineShowtime struct {
	Showtime
	StartOffsetMinutes int     `json:"start_offset_minutes"`
	DurationMinutes    int     `json:"duration_minutes"`
	PosterURL          *string `json:"poster_url"`
	BackdropURL        *string `json:"backdrop_url"`
}

type TimelineTheater struct {
	Provider       Provider           `json:"provider"`
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
	Provider Provider `json:"provider"`
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	City     string   `json:"city"`
}

type Theater struct {
	Provider       Provider `json:"provider"`
	ID             string   `json:"id"`
	Slug           string   `json:"slug"`
	Name           string   `json:"name"`
	Address        string   `json:"address"`
	City           string   `json:"city"`
	CitySlug       string   `json:"city_slug"`
	PostalCode     string   `json:"postal_code"`
	AvailableDates []string `json:"available_dates"`
	AcceptedPasses []string `json:"accepted_passes"`
}

type City struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type CityTheater struct {
	Provider Provider `json:"provider"`
	ID       string   `json:"id"`
	Slug     string   `json:"slug"`
	Name     string   `json:"name"`
}

type CityInventoryItem struct {
	Name     string        `json:"name"`
	Slug     string        `json:"slug"`
	Theaters []CityTheater `json:"theaters"`
}

type CityInventory struct {
	GeneratedAt time.Time           `json:"generated_at"`
	Items       []CityInventoryItem `json:"items"`
}

type CityDetail struct {
	GeneratedAt time.Time          `json:"generated_at"`
	City        City               `json:"city"`
	Theaters    []Theater          `json:"theaters"`
	Movies      []MovieCatalogItem `json:"movies"`
}

type TheaterShowtimes struct {
	GeneratedAt time.Time          `json:"generated_at"`
	Timezone    string             `json:"timezone"`
	Theater     Theater            `json:"theater"`
	Date        *string            `json:"date"`
	Showtimes   []TimelineShowtime `json:"showtimes"`
}

type MovieCatalogItem struct {
	Slug           string    `json:"slug"`
	Title          string    `json:"title"`
	RuntimeMinutes int       `json:"runtime_minutes"`
	UpdatedAt      time.Time `json:"updated_at"`
	ShowtimeCount  int       `json:"showtime_count,omitempty"`
	PosterURL      *string   `json:"poster_url"`
	TMDBID         *int64    `json:"tmdb_id"`
	Overview       *string   `json:"overview"`
	ReleaseDate    *string   `json:"release_date"`
	Genres         []string  `json:"genres"`
}

type MovieCatalog struct {
	GeneratedAt     time.Time          `json:"generated_at"`
	CatalogRevision string             `json:"catalog_revision"`
	Items           []MovieCatalogItem `json:"items"`
	Page            int                `json:"page"`
	PageSize        int                `json:"page_size"`
	Total           int                `json:"total"`
}

type MovieTheaterShowtimes struct {
	Provider  Provider   `json:"provider"`
	ID        string     `json:"id"`
	Slug      string     `json:"slug"`
	Name      string     `json:"name"`
	City      string     `json:"city"`
	Showtimes []Showtime `json:"showtimes"`
}

type MovieSchedule struct {
	Movie             MovieCatalogItem        `json:"movie"`
	BackdropURL       *string                 `json:"backdrop_url"`
	CurrentlyScreened bool                    `json:"currently_screened"`
	Date              string                  `json:"date"`
	AvailableDates    []string                `json:"available_dates"`
	Theaters          []MovieTheaterShowtimes `json:"theaters"`
}

type SlotResult struct {
	Showtime           Showtime       `json:"showtime"`
	Theater            TheaterSummary `json:"theater"`
	EffectiveStartTime time.Time      `json:"effective_start_time"`
	EffectiveEndTime   time.Time      `json:"effective_end_time"`
	BufferAdsMinutes   int            `json:"buffer_ads_minutes"`
	SlackBeforeMinutes int            `json:"slack_before_minutes"`
	SlackAfterMinutes  int            `json:"slack_after_minutes"`
	PosterURL          *string        `json:"poster_url"`
	BackdropURL        *string        `json:"backdrop_url"`
}

type TimelineQuery struct {
	Date       string
	TheaterIDs []string
	Language   Language
}

type TheaterCatalogQuery struct {
	City  string
	Chain Provider
}

type MovieCatalogQuery struct {
	CurrentlyScreened *bool
	IncludeEnded      bool
	Search            string
	Sort              MovieCatalogSort
	TheaterIDs        []string
	Page              int
	PageSize          int
}

type MovieShowtimesQuery struct {
	Slug       string
	Date       string
	City       string
	TheaterIDs []string
}

type TheaterShowtimesQuery struct {
	Slug         string
	Date         string
	DateProvided bool
}

type SlotQuery struct {
	City         string
	TheaterIDs   []string
	Date         string
	StartAfter   string
	FinishBefore string
	BufferAds    int
	IncludeAds   bool
	Language     Language
	Format       Format
}
