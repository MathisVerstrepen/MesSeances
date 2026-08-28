package schedule

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrDatasetValidation = errors.New("dataset validation failed")

const (
	maxIdentityLength     = 128
	maxNameAndTitleLength = 1024
	maxAddressLength      = 2048
	maxShortFieldLength   = 256
	maxURLLength          = 4096
)

func ValidInclusiveDateWindow(from, through time.Time) bool {
	return !through.Before(from)
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
		if provider != ProviderKinepolis && (theater.Address == "" || theater.PostalCode == "") {
			return fmt.Errorf("theater has missing required field")
		}
		if (theater.Latitude == nil) != (theater.Longitude == nil) || theater.Latitude != nil && (math.IsNaN(*theater.Latitude) || math.IsInf(*theater.Latitude, 0) || *theater.Latitude < -90 || *theater.Latitude > 90 || math.IsNaN(*theater.Longitude) || math.IsInf(*theater.Longitude, 0) || *theater.Longitude < -180 || *theater.Longitude > 180) {
			return fmt.Errorf("invalid theater coordinates")
		}
		if !validProviderIdentity(provider, "theater", theater.ProviderID) || theater.ID != string(provider)+"-"+theater.ProviderID || theater.Slug != theater.ID {
			return fmt.Errorf("invalid theater identity")
		}
		providerKey := string(provider) + "\x00" + theater.ProviderID
		if providerTheaters[providerKey] {
			return fmt.Errorf("invalid or duplicate provider theater identity")
		}
		if _, exists := theaters[theater.ID]; exists {
			return fmt.Errorf("duplicate theater identity")
		}
		providerTheaters[providerKey] = true
		theaters[theater.ID] = theater
		seenDates := map[string]bool{}
		for _, value := range theater.AvailableDates {
			date, err := time.ParseInLocation(dateLayout, value, location)
			if err != nil || date.Before(from) || date.After(through) || seenDates[value] {
				return fmt.Errorf("invalid theater available date")
			}
			seenDates[value] = true
		}
		if provider == ProviderUGC && (len(theater.AcceptedPasses) != 1 || theater.AcceptedPasses[0] != "UGC_ILLIMITE") || provider != ProviderUGC && len(theater.AcceptedPasses) != 0 {
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
		providerShowingKey := string(provider) + "\x00" + showing.ProviderShowingID
		if !ok || provider != recordProvider(theater.Provider, theater.ID) || data.Provider != ProviderCombined && provider != data.Provider || showing.ID == "" || showing.ProviderShowingID == "" || showing.ID != string(provider)+"-showing-"+showing.ProviderShowingID || !validProviderIdentity(provider, "showing", showing.ProviderShowingID) || showings[showing.ID] || providerShowings[providerShowingKey] {
			return fmt.Errorf("invalid or duplicate showing identity")
		}
		showings[showing.ID] = true
		providerShowings[providerShowingKey] = true
		runtime, validRuntime := RuntimeDuration(showing.Movie.RuntimeMinutes)
		movieProvider := recordProvider(showing.Movie.Provider, showing.Movie.Slug)
		unknownCGRRuntime := provider == ProviderCGR && showing.Movie.RuntimeMinutes == 0
		if movieProvider != provider || showing.Movie.ProviderID == "" || showing.Movie.Slug != string(provider)+"-film-"+showing.Movie.ProviderID || !validProviderIdentity(provider, "movie", showing.Movie.ProviderID) || showing.Movie.Title == "" || !validRuntime && !unknownCGRRuntime {
			return fmt.Errorf("invalid movie")
		}
		if showing.Movie.LocalMovieID < 0 || showing.Movie.LocalMovieID == 0 && showing.Movie.LocalMetadataProvider != "" {
			return fmt.Errorf("invalid local movie")
		}
		if showing.Movie.LocalMovieID > 0 {
			if !validProvider(showing.Movie.LocalMetadataProvider, false) || showing.Movie.Enrichment != nil {
				return fmt.Errorf("invalid local movie")
			}
			sourceKey := string(provider) + "\x00" + showing.Movie.ProviderID
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
		validEnd := showing.EndTime.After(showing.StartTime) || unknownCGRRuntime && showing.EndTime.Equal(showing.StartTime)
		if showing.StartTime.IsZero() || showing.EndTime.IsZero() || !validEnd || provider == ProviderKinepolis && !showing.EndTime.Equal(showing.StartTime.Add(runtime)) {
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
		if showing.Movie.Enrichment != nil {
			if showing.Movie.Enrichment.BackdropURL != "" && !validTMDBBackdropURL(showing.Movie.Enrichment.BackdropURL) {
				return fmt.Errorf("invalid enrichment backdrop URL")
			}
			if invalidTrailerKeys(showing.Movie.Enrichment.TMDBID, showing.Movie.Enrichment.TrailerVFYouTubeKey, showing.Movie.Enrichment.TrailerVOYouTubeKey) {
				return fmt.Errorf("invalid enrichment trailer YouTube keys")
			}
		}
	}
	if data.PublicMovies != nil || data.MovieSources != nil || data.MovieAliases != nil {
		if err := validatePublicMovieCatalog(data); err != nil {
			return err
		}
	}
	return nil
}

func validatePublicMovieCatalog(data Dataset) error {
	publicMovies := make(map[int64]PublicMovieRecord, len(data.PublicMovies))
	activeTMDB := make(map[int64]bool)
	for _, movie := range data.PublicMovies {
		if movie.ID <= 0 || !validProvider(movie.IdentityAnchorProvider, false) || !validProviderIdentity(movie.IdentityAnchorProvider, "movie", movie.IdentityAnchorSourceID) || movie.Title == "" || movie.RuntimeMinutes < 0 || movie.RuntimeMinutes == 0 && movie.IdentityAnchorProvider != ProviderCGR || invalidTrailerKeys(movie.TMDBID, movie.TrailerVFYouTubeKey, movie.TrailerVOYouTubeKey) || movie.UpdatedAt.IsZero() || movie.UpdatedAt.Location() != time.UTC || publicMovies[movie.ID].ID != 0 {
			return fmt.Errorf("invalid public movie")
		}
		if movie.RedirectToID == movie.ID {
			return fmt.Errorf("invalid public movie redirect")
		}
		if movie.RedirectToID == 0 && movie.TMDBID > 0 {
			if activeTMDB[movie.TMDBID] {
				return fmt.Errorf("duplicate active public movie TMDB identity")
			}
			activeTMDB[movie.TMDBID] = true
		}
		publicMovies[movie.ID] = movie
	}
	for _, movie := range data.PublicMovies {
		if movie.RedirectToID == 0 {
			continue
		}
		target, ok := publicMovies[movie.RedirectToID]
		if !ok || target.RedirectToID != 0 {
			return fmt.Errorf("invalid public movie redirect target")
		}
	}
	sources := make(map[string]PublicMovieSourceRecord, len(data.MovieSources))
	for _, source := range data.MovieSources {
		key := string(source.Provider) + "\x00" + source.SourceMovieID
		target, ok := publicMovies[source.PublicMovieID]
		if !ok || target.RedirectToID != 0 || sources[key].SourceMovieID != "" || !validProvider(source.Provider, false) || !validProviderIdentity(source.Provider, "movie", source.SourceMovieID) || source.SourceSlug != string(source.Provider)+"-film-"+source.SourceMovieID || source.Title == "" || source.RuntimeMinutes < 0 || source.RuntimeMinutes == 0 && source.Provider != ProviderCGR || source.PosterURL != "" && !validProviderImageURL(source.Provider, source.PosterURL) {
			return fmt.Errorf("invalid public movie source")
		}
		sources[key] = source
	}
	canonicalSlugs := make(map[string]bool)
	for _, movie := range data.PublicMovies {
		canonicalSlugs[publicMovieIDSlug(movie.ID)] = true
	}
	aliases := make(map[string]bool, len(data.MovieAliases))
	for _, alias := range data.MovieAliases {
		target, ok := publicMovies[alias.PublicMovieID]
		validIdentity := alias.Kind == "source" && validProvider(alias.Provider, false) && validProviderIdentity(alias.Provider, "movie", alias.SourceMovieID)
		validEvidence := (alias.Kind == "local" || alias.Kind == "tmdb") && alias.Provider == "" && alias.SourceMovieID == ""
		if alias.Slug == "" || canonicalSlugs[alias.Slug] || aliases[alias.Slug] || !ok || target.RedirectToID != 0 || !validIdentity && !validEvidence {
			return fmt.Errorf("invalid movie slug alias")
		}
		aliases[alias.Slug] = true
	}
	for _, showing := range data.Showtimes {
		key := string(recordProvider(showing.Movie.Provider, showing.Movie.Slug)) + "\x00" + showing.Movie.ProviderID
		source, ok := sources[key]
		if !ok || showing.Movie.PublicMovieID != source.PublicMovieID {
			return fmt.Errorf("active movie source mapping missing")
		}
	}
	return nil
}

func invalidTrailerKeys(tmdbID int64, vf, vo string) bool {
	return vf != "" && (tmdbID <= 0 || !validYouTubeKey(vf)) || vo != "" && (tmdbID <= 0 || !validYouTubeKey(vo)) || vf != "" && vf == vo
}

func validYouTubeKey(value string) bool {
	if len(value) != 11 {
		return false
	}
	for _, character := range value {
		if character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func sameLocalMovieMetadata(a, b MovieRecord) bool {
	return a.LocalMetadataProvider == b.LocalMetadataProvider && a.Title == b.Title && a.RuntimeMinutes == b.RuntimeMinutes && a.PosterURL == b.PosterURL && a.Overview == b.Overview && a.ReleaseDate == b.ReleaseDate && strings.Join(a.Genres, "\x00") == strings.Join(b.Genres, "\x00")
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
		if providerA != ProviderUGC {
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

func validLanguage(v Language) bool {
	if v == LanguageVOSTFR || v == LanguageVF || v == LanguageVO || v == LanguageVFSME {
		return true
	}
	return v != LanguageAll && providerLanguage.MatchString(string(v))
}
func validFormat(v Format) bool {
	return v == Format2D || v == Format3D || v == FormatIMAX || v == FormatDolby || v == FormatScreenX || v == FormatLaserUltra || v == Format4DX || v == FormatICE
}

func validUGCURL(raw string, allowAssets bool) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "www.ugc.fr" || allowAssets && strings.HasSuffix(host, ".ugc.fr")
}

func validBookingURL(provider Provider, raw, showingID, theaterProviderID string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.RawPath != "" {
		return false
	}
	if provider == ProviderKinepolis {
		return parsed.Host == "kinepolis.fr" && parsed.RawQuery == "" && parsed.Path == "/direct-vista-redirect/"+showingID+"/0/"+theaterProviderID+"/0"
	}
	if provider == ProviderPathe {
		parts := strings.Split(parsed.Path, "/")
		return parsed.Host == "s.pathe.fr" && parsed.RawQuery == "" && parsed.Opaque == "" && !parsed.ForceQuery && len(parts) == 4 && parts[0] == "" && parts[1] == "fr" && providerIdentity.MatchString(parts[2]) && parts[2] == showingID && parts[3] == "booking" && !strings.Contains(parsed.Path, `\`) && !hasPatheTraversalSegment(parsed.Path)
	}
	if provider == ProviderCGR {
		parts := strings.Split(parsed.Path, "/")
		return parsed.Host == "achat.cgrcinemas.fr" && parsed.RawQuery == "" && parsed.Opaque == "" && !parsed.ForceQuery && len(parts) == 4 && parts[0] == "" && validCGRBookingSlug(parts[1]) && parts[2] == "r" && validPositiveDecimal(parts[3]) && !strings.Contains(parsed.Path, `\`) && !hasPatheTraversalSegment(parsed.Path)
	}
	if parsed.Host != "www.ugc.fr" || parsed.Path != "/reservationSeances.html" {
		return false
	}
	query := parsed.Query()
	return len(query) == 1 && len(query["id"]) == 1 && query.Get("id") == showingID
}

func validCGRBookingSlug(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

var (
	providerIdentity     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
	patheShowingIdentity = regexp.MustCompile(`^V[1-9][0-9]*S[1-9][0-9]*$`)
	cgrTheaterIdentity   = regexp.MustCompile(`^[A-Z][0-9]{4}$`)
	cgrShowingIdentity   = regexp.MustCompile(`^[A-Z][0-9]{4}-[a-f0-9]{64}$`)
	providerLanguage     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,15}$`)
)

func validProvider(provider Provider, combined bool) bool {
	return provider == ProviderUGC || provider == ProviderKinepolis || provider == ProviderPathe || provider == ProviderCGR || combined && provider == ProviderCombined
}

func validProviderIdentity(provider Provider, kind, value string) bool {
	if !providerIdentity.MatchString(value) {
		return false
	}
	if provider == ProviderUGC {
		number, err := strconv.ParseUint(value, 10, 64)
		return err == nil && number > 0
	}
	if provider == ProviderPathe {
		maxLength := maxIdentityLength
		switch kind {
		case "theater":
			maxLength -= len("pathe-")
		case "movie":
			maxLength -= len("pathe-film-")
		case "showing":
			maxLength -= len("pathe-showing-")
		default:
			return false
		}
		if len(value) > maxLength {
			return false
		}
		if kind == "showing" {
			return patheShowingIdentity.MatchString(value)
		}
		return true
	}
	if provider == ProviderCGR {
		switch kind {
		case "theater":
			return cgrTheaterIdentity.MatchString(value)
		case "movie":
			return validPositiveDecimal(value)
		case "showing":
			return cgrShowingIdentity.MatchString(value)
		default:
			return false
		}
	}
	return provider == ProviderKinepolis
}

func validPositiveDecimal(value string) bool {
	if value == "" || value[0] == '0' {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func validProviderImageURL(provider Provider, raw string) bool {
	if provider == ProviderUGC {
		return validUGCURL(raw, true)
	}
	parsed, err := url.Parse(raw)
	if provider == ProviderKinepolis {
		return err == nil && len(raw) <= maxURLLength && parsed.Scheme == "https" && parsed.Host == "cdn.kinepolis.fr" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && strings.HasPrefix(parsed.Path, "/images/") && !hasPathTraversalSegment(parsed.Path) && !strings.Contains(parsed.Path, `\`)
	}
	if provider == ProviderCGR {
		if err != nil || len(raw) > maxURLLength || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Opaque != "" || parsed.ForceQuery || parsed.Path == "" || parsed.Path == "/" || hasPatheTraversalSegment(parsed.Path) || strings.Contains(parsed.Path, `\`) {
			return false
		}
		host := strings.ToLower(parsed.Hostname())
		return parsed.Host == host && (host == "acsta.net" || strings.HasSuffix(host, ".acsta.net"))
	}
	if err != nil || len(raw) > maxURLLength || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Opaque != "" || parsed.ForceQuery || parsed.Path == "" || parsed.Path == "/" || hasPatheTraversalSegment(parsed.Path) || strings.Contains(parsed.Path, `\`) {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return provider == ProviderPathe && parsed.Host == host && (host == "pathe.fr" || strings.HasSuffix(host, ".pathe.fr"))
}

func hasPatheTraversalSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
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
