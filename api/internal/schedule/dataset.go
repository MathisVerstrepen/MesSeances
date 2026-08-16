package schedule

import "time"

const (
	SchemaVersion = 1
	ProviderUGC   = "ugc"
	ScopeAll      = "all_cinemas"
	ScopeSingle   = "single_cinema"
	LanguageVO    = "VO"
	LanguageVFSME = "VF_SME"
)

type Window struct {
	From    string `json:"from"`
	Through string `json:"through"`
}

type Dataset struct {
	SchemaVersion int              `json:"schema_version"`
	Provider      string           `json:"provider"`
	Scope         string           `json:"scope"`
	GeneratedAt   time.Time        `json:"generated_at"`
	Timezone      string           `json:"timezone"`
	Window        Window           `json:"window"`
	Theaters      []TheaterRecord  `json:"theaters"`
	Showtimes     []ShowtimeRecord `json:"showtimes"`
}

type TheaterRecord struct {
	ID             string   `json:"id"`
	ProviderID     string   `json:"provider_id"`
	Slug           string   `json:"slug"`
	Name           string   `json:"name"`
	Address        string   `json:"address"`
	City           string   `json:"city"`
	PostalCode     string   `json:"postal_code"`
	AvailableDates []string `json:"available_dates"`
	AcceptedPasses []string `json:"accepted_passes"`
}

type MovieRecord struct {
	ProviderID     string `json:"provider_id"`
	Slug           string `json:"slug"`
	Title          string `json:"title"`
	RuntimeMinutes int    `json:"runtime_minutes"`
	PosterURL      string `json:"poster_url,omitempty"`
}

type ShowtimeRecord struct {
	ID                string      `json:"id"`
	ProviderShowingID string      `json:"provider_showing_id"`
	ServiceDate       string      `json:"service_date"`
	TheaterID         string      `json:"theater_id"`
	Movie             MovieRecord `json:"movie"`
	StartTime         time.Time   `json:"start_time"`
	EndTime           time.Time   `json:"end_time"`
	Language          string      `json:"language"`
	ProviderVersion   string      `json:"provider_version"`
	Format            string      `json:"format"`
	Room              string      `json:"room"`
	BookingURL        string      `json:"booking_url"`
}

func cloneDataset(in Dataset) Dataset {
	out := in
	out.Theaters = append([]TheaterRecord(nil), in.Theaters...)
	for i := range out.Theaters {
		out.Theaters[i].AvailableDates = append([]string(nil), in.Theaters[i].AvailableDates...)
		out.Theaters[i].AcceptedPasses = append([]string(nil), in.Theaters[i].AcceptedPasses...)
	}
	out.Showtimes = append([]ShowtimeRecord(nil), in.Showtimes...)
	return out
}
