package schedule

import "time"

const (
	SchemaVersion = 1
)

type Window struct {
	From    string `json:"from"`
	Through string `json:"through"`
}

type Dataset struct {
	SchemaVersion int                       `json:"schema_version"`
	Provider      Provider                  `json:"provider"`
	Scope         Scope                     `json:"scope"`
	GeneratedAt   time.Time                 `json:"generated_at"`
	Timezone      string                    `json:"timezone"`
	Window        Window                    `json:"window"`
	Theaters      []TheaterRecord           `json:"theaters"`
	Showtimes     []ShowtimeRecord          `json:"showtimes"`
	PublicMovies  []PublicMovieRecord       `json:"-"`
	MovieSources  []PublicMovieSourceRecord `json:"-"`
	MovieAliases  []MovieSlugAliasRecord    `json:"-"`
}

type PublicMovieRecord struct {
	ID                     int64
	RedirectToID           int64
	IdentityAnchorProvider Provider
	IdentityAnchorSourceID string
	Title                  string
	RuntimeMinutes         int
	PosterURL              string
	BackdropURL            string
	Overview               string
	ReleaseDate            string
	Genres                 []string
	TMDBID                 int64
	UpdatedAt              time.Time
}

type PublicMovieSourceRecord struct {
	Provider       Provider
	SourceMovieID  string
	PublicMovieID  int64
	SourceSlug     string
	Title          string
	RuntimeMinutes int
	PosterURL      string
	Overview       string
	ReleaseDate    string
	Genres         []string
}

type MovieSlugAliasRecord struct {
	Slug          string
	PublicMovieID int64
	Kind          string
	Provider      Provider
	SourceMovieID string
}

type TheaterRecord struct {
	Provider       Provider `json:"provider,omitempty"`
	ID             string   `json:"id"`
	ProviderID     string   `json:"provider_id"`
	Slug           string   `json:"slug"`
	Name           string   `json:"name"`
	Address        string   `json:"address"`
	City           string   `json:"city"`
	PostalCode     string   `json:"postal_code"`
	AvailableDates []string `json:"available_dates"`
	AcceptedPasses []string `json:"accepted_passes"`
	Latitude       *float64 `json:"-"`
	Longitude      *float64 `json:"-"`
}

type MovieRecord struct {
	Provider              Provider         `json:"provider,omitempty"`
	ProviderID            string           `json:"provider_id"`
	Slug                  string           `json:"slug"`
	Title                 string           `json:"title"`
	RuntimeMinutes        int              `json:"runtime_minutes"`
	PosterURL             string           `json:"poster_url,omitempty"`
	Overview              string           `json:"overview,omitempty"`
	ReleaseDate           string           `json:"release_date,omitempty"`
	Genres                []string         `json:"genres,omitempty"`
	Enrichment            *MovieEnrichment `json:"-"`
	LocalMovieID          int64            `json:"-"`
	LocalMetadataProvider Provider         `json:"-"`
	PublicMovieID         int64            `json:"-"`
}

type MovieEnrichment struct {
	TMDBID      int64
	Overview    string
	ReleaseDate string
	Genres      []string
	PosterURL   string
	BackdropURL string
}

type ShowtimeRecord struct {
	Provider          Provider    `json:"provider,omitempty"`
	ID                string      `json:"id"`
	ProviderShowingID string      `json:"provider_showing_id"`
	ServiceDate       string      `json:"service_date"`
	TheaterID         string      `json:"theater_id"`
	Movie             MovieRecord `json:"movie"`
	StartTime         time.Time   `json:"start_time"`
	EndTime           time.Time   `json:"end_time"`
	Language          Language    `json:"language"`
	ProviderVersion   string      `json:"provider_version"`
	Format            Format      `json:"format"`
	Room              string      `json:"room"`
	BookingURL        string      `json:"booking_url"`
}

func cloneDataset(in Dataset) Dataset {
	out := in
	out.Theaters = append([]TheaterRecord(nil), in.Theaters...)
	for i := range out.Theaters {
		out.Theaters[i].AvailableDates = append([]string(nil), in.Theaters[i].AvailableDates...)
		out.Theaters[i].AcceptedPasses = append([]string(nil), in.Theaters[i].AcceptedPasses...)
		out.Theaters[i].Latitude = cloneFloat(in.Theaters[i].Latitude)
		out.Theaters[i].Longitude = cloneFloat(in.Theaters[i].Longitude)
	}
	out.Showtimes = append([]ShowtimeRecord(nil), in.Showtimes...)
	for i := range out.Showtimes {
		out.Showtimes[i].Movie.Genres = append([]string(nil), in.Showtimes[i].Movie.Genres...)
		if in.Showtimes[i].Movie.Enrichment != nil {
			value := *in.Showtimes[i].Movie.Enrichment
			value.Genres = append([]string(nil), value.Genres...)
			out.Showtimes[i].Movie.Enrichment = &value
		}
	}
	out.PublicMovies = append([]PublicMovieRecord(nil), in.PublicMovies...)
	for i := range out.PublicMovies {
		out.PublicMovies[i].Genres = append([]string(nil), in.PublicMovies[i].Genres...)
	}
	out.MovieSources = append([]PublicMovieSourceRecord(nil), in.MovieSources...)
	for i := range out.MovieSources {
		out.MovieSources[i].Genres = append([]string(nil), in.MovieSources[i].Genres...)
	}
	out.MovieAliases = append([]MovieSlugAliasRecord(nil), in.MovieAliases...)
	return out
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
