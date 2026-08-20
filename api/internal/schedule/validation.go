package schedule

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	MaxTheaters                  = 256
	MaxAdvertisedDatesPerTheater = 512
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
	if data.SchemaVersion != SchemaVersion || !validProvider(data.Provider, true) || data.Timezone != Timezone {
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
		provider := recordProvider(theater.Provider, theater.ID)
		if !validProvider(provider, false) || data.Provider != ProviderCombined && provider != data.Provider || theater.ID == "" || theater.ProviderID == "" || theater.Slug == "" || theater.Name == "" || theater.City == "" {
			return fmt.Errorf("theater has missing required field")
		}
		if provider == ProviderUGC && (theater.Address == "" || theater.PostalCode == "") {
			return fmt.Errorf("theater has missing required field")
		}
		if !validProviderIdentity(provider, "theater", theater.ProviderID) || theater.ID != provider+"-"+theater.ProviderID || theater.Slug != theater.ID {
			return fmt.Errorf("invalid theater identity")
		}
		providerKey := provider + "\x00" + theater.ProviderID
		if providerTheaters[providerKey] {
			return fmt.Errorf("invalid or duplicate provider theater identity")
		}
		if _, exists := theaters[theater.ID]; exists {
			return fmt.Errorf("duplicate theater identity")
		}
		providerTheaters[providerKey] = true
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
		if provider == ProviderUGC && (len(theater.AcceptedPasses) != 1 || theater.AcceptedPasses[0] != "UGC_ILLIMITE") || provider == ProviderKinepolis && len(theater.AcceptedPasses) != 0 {
			return fmt.Errorf("invalid theater passes")
		}
	}
	showings := map[string]bool{}
	providerShowings := map[string]bool{}
	localMovies := map[int64]MovieRecord{}
	sourceLocalMovies := map[string]int64{}
	for _, showing := range data.Showtimes {
		provider := recordProvider(showing.Provider, showing.ID)
		if len(showing.ID) > maxIdentityLength || len(showing.ProviderShowingID) > maxIdentityLength || len(showing.ServiceDate) > maxShortFieldLength || len(showing.TheaterID) > maxIdentityLength || len(showing.Movie.ProviderID) > maxIdentityLength || len(showing.Movie.Slug) > maxIdentityLength || len(showing.Movie.Title) > maxNameAndTitleLength || len(showing.Movie.PosterURL) > maxURLLength || len(showing.Movie.Overview) > 10000 || len(showing.Movie.Genres) > 32 || len(showing.Language) > maxShortFieldLength || len(showing.ProviderVersion) > maxShortFieldLength || len(showing.Format) > maxShortFieldLength || len(showing.Room) > maxShortFieldLength || len(showing.BookingURL) > maxURLLength {
			return fmt.Errorf("showing field limit exceeded")
		}
		theater, ok := theaters[showing.TheaterID]
		providerShowingKey := provider + "\x00" + showing.ProviderShowingID
		if !ok || provider != recordProvider(theater.Provider, theater.ID) || data.Provider != ProviderCombined && provider != data.Provider || showing.ID == "" || showing.ProviderShowingID == "" || showing.ID != provider+"-showing-"+showing.ProviderShowingID || !validProviderIdentity(provider, "showing", showing.ProviderShowingID) || showings[showing.ID] || providerShowings[providerShowingKey] {
			return fmt.Errorf("invalid or duplicate showing identity")
		}
		showings[showing.ID] = true
		providerShowings[providerShowingKey] = true
		runtime, validRuntime := RuntimeDuration(showing.Movie.RuntimeMinutes)
		movieProvider := recordProvider(showing.Movie.Provider, showing.Movie.Slug)
		if movieProvider != provider || showing.Movie.ProviderID == "" || showing.Movie.Slug != provider+"-film-"+showing.Movie.ProviderID || !validProviderIdentity(provider, "movie", showing.Movie.ProviderID) || showing.Movie.Title == "" || !validRuntime {
			return fmt.Errorf("invalid movie")
		}
		if showing.Movie.LocalMovieID < 0 || showing.Movie.LocalMovieID == 0 && showing.Movie.LocalMetadataProvider != "" {
			return fmt.Errorf("invalid local movie")
		}
		if showing.Movie.LocalMovieID > 0 {
			if !validProvider(showing.Movie.LocalMetadataProvider, false) || showing.Movie.Enrichment != nil {
				return fmt.Errorf("invalid local movie")
			}
			sourceKey := provider + "\x00" + showing.Movie.ProviderID
			if priorID, exists := sourceLocalMovies[sourceKey]; exists && priorID != showing.Movie.LocalMovieID {
				return fmt.Errorf("inconsistent local movie identity")
			}
			sourceLocalMovies[sourceKey] = showing.Movie.LocalMovieID
			if prior, exists := localMovies[showing.Movie.LocalMovieID]; exists && !sameLocalMovieMetadata(prior, showing.Movie) {
				return fmt.Errorf("inconsistent local movie metadata")
			}
			localMovies[showing.Movie.LocalMovieID] = showing.Movie
		}
		if showing.Movie.ReleaseDate != "" {
			parsed, err := time.Parse(dateLayout, showing.Movie.ReleaseDate)
			if err != nil || parsed.Format(dateLayout) != showing.Movie.ReleaseDate {
				return fmt.Errorf("invalid movie release date")
			}
		}
		for _, genre := range showing.Movie.Genres {
			if strings.TrimSpace(genre) == "" || len(genre) > maxShortFieldLength {
				return fmt.Errorf("invalid movie genre")
			}
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
		posterProvider := provider
		if showing.Movie.LocalMovieID > 0 {
			posterProvider = showing.Movie.LocalMetadataProvider
		}
		if !validBookingURL(provider, showing.BookingURL, showing.ProviderShowingID, theater.ProviderID) || (showing.Movie.PosterURL != "" && !validProviderImageURL(posterProvider, showing.Movie.PosterURL)) {
			return fmt.Errorf("invalid provider URL")
		}
		if showing.Movie.Enrichment != nil && showing.Movie.Enrichment.BackdropURL != "" && !validTMDBBackdropURL(showing.Movie.Enrichment.BackdropURL) {
			return fmt.Errorf("invalid enrichment backdrop URL")
		}
	}
	return nil
}

func sameLocalMovieMetadata(a, b MovieRecord) bool {
	return a.LocalMetadataProvider == b.LocalMetadataProvider && a.Title == b.Title && a.RuntimeMinutes == b.RuntimeMinutes && a.PosterURL == b.PosterURL && a.Overview == b.Overview && a.ReleaseDate == b.ReleaseDate && strings.Join(a.Genres, "\x00") == strings.Join(b.Genres, "\x00")
}

func validDatasetRecordCounts(theaters, showtimes int) bool {
	return theaters >= 0 && theaters <= MaxTheaters && showtimes >= 0 && showtimes <= MaxShowtimes
}

func normalizeDataset(data *Dataset) {
	for i := range data.Theaters {
		sort.Strings(data.Theaters[i].AvailableDates)
	}
	sort.Slice(data.Theaters, func(i, j int) bool {
		providerA, providerB := recordProvider(data.Theaters[i].Provider, data.Theaters[i].ID), recordProvider(data.Theaters[j].Provider, data.Theaters[j].ID)
		if providerA != providerB {
			return providerA < providerB
		}
		if providerA == ProviderKinepolis {
			return data.Theaters[i].ProviderID < data.Theaters[j].ProviderID
		}
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
	return v == Format2D || v == Format3D || v == FormatIMAX || v == FormatDolby || v == FormatScreenX || v == FormatLaserUltra || v == Format4DX
}

func validUGCURL(raw string, allowAssets bool) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "www.ugc.fr" || allowAssets && strings.HasSuffix(host, ".ugc.fr")
}

func validBookingURL(provider, raw, showingID, theaterProviderID string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.RawPath != "" {
		return false
	}
	if provider == ProviderKinepolis {
		return parsed.Host == "kinepolis.fr" && parsed.RawQuery == "" && parsed.Path == "/direct-vista-redirect/"+showingID+"/0/"+theaterProviderID+"/0"
	}
	if parsed.Host != "www.ugc.fr" || parsed.Path != "/reservationSeances.html" {
		return false
	}
	query := parsed.Query()
	return len(query) == 1 && len(query["id"]) == 1 && query.Get("id") == showingID
}

var providerIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func validProvider(provider string, combined bool) bool {
	return provider == ProviderUGC || provider == ProviderKinepolis || combined && provider == ProviderCombined
}

func validProviderIdentity(provider, kind, value string) bool {
	if !providerIdentity.MatchString(value) {
		return false
	}
	if provider == ProviderUGC {
		number, err := strconv.ParseUint(value, 10, 64)
		return err == nil && number > 0
	}
	return provider == ProviderKinepolis
}

func validProviderImageURL(provider, raw string) bool {
	if provider == ProviderUGC {
		return validUGCURL(raw, true)
	}
	parsed, err := url.Parse(raw)
	return err == nil && len(raw) <= maxURLLength && parsed.Scheme == "https" && parsed.Host == "cdn.kinepolis.fr" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && strings.HasPrefix(parsed.Path, "/images/") && !hasPathTraversalSegment(parsed.Path) && !strings.Contains(parsed.Path, `\`)
}

func hasPathTraversalSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func validTMDBBackdropURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	suffix := strings.TrimPrefix(parsed.Path, "/t/p/w780/")
	return len(raw) <= maxURLLength && strings.HasPrefix(raw, "https://image.tmdb.org/t/p/w780/") && parsed.Scheme == "https" && parsed.Host == "image.tmdb.org" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.RawPath == "" && strings.HasPrefix(parsed.Path, "/t/p/w780/") && suffix != "" && !strings.HasPrefix(suffix, "/") && !hasPathTraversalSegment(parsed.Path) && !strings.Contains(parsed.Path, "\\")
}
