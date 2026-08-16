package schedule

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	MaxTheaters                  = 256
	MaxAdvertisedDatesPerTheater = 64
	MaxShowtimes                 = 250000
	maxIdentityLength            = 128
	maxNameAndTitleLength        = 1024
	maxAddressLength             = 2048
	maxShortFieldLength          = 256
	maxURLLength                 = 4096
)

func ValidInclusiveDateWindow(from, through time.Time) bool {
	return !through.Before(from) && !through.After(from.AddDate(0, 0, 13))
}

func ValidateDataset(data Dataset, requireComplete bool) error {
	if data.SchemaVersion != SchemaVersion || data.Provider != ProviderUGC || data.Timezone != Timezone {
		return fmt.Errorf("invalid schedule dataset metadata")
	}
	if data.Scope != ScopeAll && (requireComplete || data.Scope != ScopeSingle) {
		return fmt.Errorf("invalid schedule dataset scope")
	}
	if data.GeneratedAt.IsZero() || data.GeneratedAt.Location() != time.UTC {
		return fmt.Errorf("invalid generated timestamp")
	}
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		return fmt.Errorf("load schedule timezone: %w", err)
	}
	from, err := time.ParseInLocation(dateLayout, data.Window.From, location)
	if err != nil || from.Format(dateLayout) != data.Window.From {
		return fmt.Errorf("invalid dataset window start")
	}
	through, err := time.ParseInLocation(dateLayout, data.Window.Through, location)
	if err != nil || through.Format(dateLayout) != data.Window.Through || !ValidInclusiveDateWindow(from, through) {
		return fmt.Errorf("invalid dataset window")
	}
	if !validDatasetRecordCounts(len(data.Theaters), len(data.Showtimes)) {
		return fmt.Errorf("schedule dataset record limit exceeded")
	}
	if len(data.Theaters) == 0 || len(data.Showtimes) == 0 {
		return fmt.Errorf("complete dataset must contain theaters and showtimes")
	}
	theaters := make(map[string]TheaterRecord, len(data.Theaters))
	providerTheaters := make(map[string]bool, len(data.Theaters))
	for _, theater := range data.Theaters {
		if len(theater.ID) > maxIdentityLength || len(theater.ProviderID) > maxIdentityLength || len(theater.Slug) > maxIdentityLength || len(theater.Name) > maxNameAndTitleLength || len(theater.Address) > maxAddressLength || len(theater.City) > maxShortFieldLength || len(theater.PostalCode) > maxShortFieldLength {
			return fmt.Errorf("theater field limit exceeded")
		}
		if theater.ID == "" || theater.ProviderID == "" || theater.Slug == "" || theater.Name == "" || theater.Address == "" || theater.City == "" || theater.PostalCode == "" {
			return fmt.Errorf("theater has missing required field")
		}
		if theater.ID != "ugc-"+theater.ProviderID || theater.Slug != theater.ID {
			return fmt.Errorf("invalid theater identity")
		}
		number, parseErr := strconv.ParseUint(theater.ProviderID, 10, 64)
		if parseErr != nil || number == 0 || providerTheaters[theater.ProviderID] {
			return fmt.Errorf("invalid or duplicate provider theater identity")
		}
		if _, exists := theaters[theater.ID]; exists {
			return fmt.Errorf("duplicate theater identity")
		}
		providerTheaters[theater.ProviderID] = true
		theaters[theater.ID] = theater
		if len(theater.AvailableDates) > MaxAdvertisedDatesPerTheater {
			return fmt.Errorf("theater available date limit exceeded")
		}
		seenDates := map[string]bool{}
		for _, value := range theater.AvailableDates {
			date, err := time.ParseInLocation(dateLayout, value, location)
			if err != nil || date.Before(from) || date.After(through) || seenDates[value] {
				return fmt.Errorf("invalid theater available date")
			}
			seenDates[value] = true
		}
		if len(theater.AcceptedPasses) != 1 || theater.AcceptedPasses[0] != "UGC_ILLIMITE" {
			return fmt.Errorf("invalid theater passes")
		}
	}
	showings := map[string]bool{}
	providerShowings := map[string]bool{}
	for _, showing := range data.Showtimes {
		if len(showing.ID) > maxIdentityLength || len(showing.ProviderShowingID) > maxIdentityLength || len(showing.ServiceDate) > maxShortFieldLength || len(showing.TheaterID) > maxIdentityLength || len(showing.Movie.ProviderID) > maxIdentityLength || len(showing.Movie.Slug) > maxIdentityLength || len(showing.Movie.Title) > maxNameAndTitleLength || len(showing.Movie.PosterURL) > maxURLLength || len(showing.Language) > maxShortFieldLength || len(showing.ProviderVersion) > maxShortFieldLength || len(showing.Format) > maxShortFieldLength || len(showing.Room) > maxShortFieldLength || len(showing.BookingURL) > maxURLLength {
			return fmt.Errorf("showing field limit exceeded")
		}
		theater, ok := theaters[showing.TheaterID]
		if !ok || showing.ID == "" || showing.ProviderShowingID == "" || showing.ID != "ugc-showing-"+showing.ProviderShowingID || showings[showing.ID] || providerShowings[showing.ProviderShowingID] {
			return fmt.Errorf("invalid or duplicate showing identity")
		}
		number, err := strconv.ParseUint(showing.ProviderShowingID, 10, 64)
		if err != nil || number == 0 {
			return fmt.Errorf("invalid provider showing identity")
		}
		showings[showing.ID] = true
		providerShowings[showing.ProviderShowingID] = true
		runtime, validRuntime := RuntimeDuration(showing.Movie.RuntimeMinutes)
		if showing.Movie.ProviderID == "" || showing.Movie.Slug != "ugc-film-"+showing.Movie.ProviderID || showing.Movie.Title == "" || !validRuntime {
			return fmt.Errorf("invalid movie")
		}
		if number, err := strconv.ParseUint(showing.Movie.ProviderID, 10, 64); err != nil || number == 0 {
			return fmt.Errorf("invalid provider movie identity")
		}
		date, err := time.ParseInLocation(dateLayout, showing.ServiceDate, location)
		if err != nil || date.Before(from) || date.After(through) || !contains(theater.AvailableDates, showing.ServiceDate) {
			return fmt.Errorf("invalid showing service date")
		}
		if showing.StartTime.IsZero() || !showing.EndTime.Equal(showing.StartTime.Add(runtime)) {
			return fmt.Errorf("invalid showing times")
		}
		localStart := showing.StartTime.In(location)
		_, actualOffset := showing.StartTime.Zone()
		_, expectedOffset := localStart.Zone()
		if actualOffset != expectedOffset {
			return fmt.Errorf("showing timestamp does not use Europe/Paris offset")
		}
		expectedDate := localStart.Format(dateLayout)
		if localStart.Hour() <= 2 {
			expectedDate = localStart.AddDate(0, 0, -1).Format(dateLayout)
		} else if localStart.Hour() < 8 {
			return fmt.Errorf("showing outside cinema day")
		}
		if expectedDate != showing.ServiceDate || !validLanguage(showing.Language) || !validFormat(showing.Format) || showing.ProviderVersion == "" {
			return fmt.Errorf("invalid showing attributes")
		}
		if !validBookingURL(showing.BookingURL, showing.ProviderShowingID) || (showing.Movie.PosterURL != "" && !validUGCURL(showing.Movie.PosterURL, true)) {
			return fmt.Errorf("invalid provider URL")
		}
		if showing.Movie.Enrichment != nil && showing.Movie.Enrichment.BackdropURL != "" && !validTMDBBackdropURL(showing.Movie.Enrichment.BackdropURL) {
			return fmt.Errorf("invalid enrichment backdrop URL")
		}
	}
	return nil
}

func validDatasetRecordCounts(theaters, showtimes int) bool {
	return theaters >= 0 && theaters <= MaxTheaters && showtimes >= 0 && showtimes <= MaxShowtimes
}

func normalizeDataset(data *Dataset) {
	for i := range data.Theaters {
		sort.Strings(data.Theaters[i].AvailableDates)
	}
	sort.Slice(data.Theaters, func(i, j int) bool {
		a, _ := strconv.ParseUint(data.Theaters[i].ProviderID, 10, 64)
		b, _ := strconv.ParseUint(data.Theaters[j].ProviderID, 10, 64)
		return a < b
	})
	sort.Slice(data.Showtimes, func(i, j int) bool {
		a, b := data.Showtimes[i], data.Showtimes[j]
		if a.TheaterID != b.TheaterID {
			return a.TheaterID < b.TheaterID
		}
		if a.ServiceDate != b.ServiceDate {
			return a.ServiceDate < b.ServiceDate
		}
		if !a.StartTime.Equal(b.StartTime) {
			return a.StartTime.Before(b.StartTime)
		}
		return a.ID < b.ID
	})
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func validLanguage(v string) bool {
	return v == LanguageVOSTFR || v == LanguageVF || v == LanguageVO || v == LanguageVFSME
}
func validFormat(v string) bool {
	return v == "2D" || v == "3D" || v == "IMAX" || v == "DOLBY" || v == "4DX"
}

func validUGCURL(raw string, allowAssets bool) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "www.ugc.fr" || allowAssets && strings.HasSuffix(host, ".ugc.fr")
}

func validBookingURL(raw, showingID string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "www.ugc.fr" || parsed.User != nil || parsed.Path != "/reservationSeances.html" || parsed.Fragment != "" {
		return false
	}
	query := parsed.Query()
	return len(query) == 1 && len(query["id"]) == 1 && query.Get("id") == showingID
}

func validTMDBBackdropURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	suffix := strings.TrimPrefix(parsed.Path, "/t/p/w780/")
	return len(raw) <= maxURLLength && strings.HasPrefix(raw, "https://image.tmdb.org/t/p/w780/") && parsed.Scheme == "https" && parsed.Host == "image.tmdb.org" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.RawPath == "" && strings.HasPrefix(parsed.Path, "/t/p/w780/") && suffix != "" && !strings.HasPrefix(suffix, "/") && !strings.Contains(parsed.Path, "..") && !strings.Contains(parsed.Path, "\\")
}
