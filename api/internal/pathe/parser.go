package pathe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"messeances/api/internal/schedule"
)

var (
	slugPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
	showingIDPattern = regexp.MustCompile(`^V[1-9][0-9]*S[1-9][0-9]*$`)
)

const (
	providerTimeLayout       = "2006-01-02 15:04:05"
	maxDerivedIdentityLength = 128
	maxCinemaSlugLength      = maxDerivedIdentityLength - len("pathe-")
	maxMovieSlugLength       = maxDerivedIdentityLength - len("pathe-film-")
	maxShowingIDLength       = maxDerivedIdentityLength - len("pathe-showing-")
)

type cinema struct {
	slug, name, address, city, postalCode string
}

type show struct {
	slug, title, poster string
	runtime             int
	genres              []string
	isMovie             bool
}

func decodeJSON(body []byte, destination any) error {
	if len(body) == 0 || len(body) > MaxResponseBytes {
		return fmt.Errorf("invalid JSON response size")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("malformed JSON response")
	}
	if err := decoder.Decode(&struct{}{}); err == io.EOF {
		return nil
	}
	return fmt.Errorf("malformed JSON response")
}

func parseCinemas(body []byte) ([]cinema, error) {
	var response []cinemaResponse
	if err := decodeJSON(body, &response); err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(response))
	result := make([]cinema, 0, len(response))
	for _, item := range response {
		slug := strings.TrimSpace(item.Slug)
		name := strings.TrimSpace(item.Name)
		if !validSourceSlug(slug, maxCinemaSlugLength) || seen[slug] {
			return nil, fmt.Errorf("invalid or duplicate cinema slug")
		}
		if name == "" || len(item.Theaters) == 0 {
			return nil, fmt.Errorf("cinema metadata is incomplete")
		}
		address := strings.TrimSpace(item.Theaters[0].AddressLine1)
		city := strings.TrimSpace(item.Theaters[0].AddressCity)
		postalCode := strings.TrimSpace(item.Theaters[0].AddressZip)
		if address == "" || city == "" || postalCode == "" {
			return nil, fmt.Errorf("cinema metadata is incomplete")
		}
		seen[slug] = true
		result = append(result, cinema{slug: slug, name: name, address: address, city: city, postalCode: postalCode})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].slug < result[j].slug })
	return result, nil
}

func parseShows(body []byte) (map[string]show, error) {
	var response showsResponse
	if err := decodeJSON(body, &response); err != nil {
		return nil, err
	}
	if len(response.Shows) == 0 {
		return nil, fmt.Errorf("show list is empty")
	}
	result := make(map[string]show, len(response.Shows))
	for _, item := range response.Shows {
		slug := strings.TrimSpace(item.Slug)
		title := strings.TrimSpace(item.Title)
		_, validRuntime := schedule.RuntimeDuration(item.Duration)
		if !validSourceSlug(slug, maxMovieSlugLength) || result[slug].slug != "" {
			return nil, fmt.Errorf("invalid or duplicate show slug")
		}
		if title == "" || !validRuntime || item.IsMovie == nil {
			return nil, fmt.Errorf("show metadata is incomplete")
		}
		genres := make([]string, 0, len(item.Genres))
		for _, rawGenre := range item.Genres {
			genre := strings.TrimSpace(rawGenre)
			if genre == "" {
				return nil, fmt.Errorf("show metadata is incomplete")
			}
			genres = append(genres, genre)
		}
		result[slug] = show{slug: slug, title: title, runtime: item.Duration, poster: safePosterURL(strings.TrimSpace(item.PosterPath.Large)), genres: genres, isMovie: *item.IsMovie}
	}
	return result, nil
}

func parseProgram(body []byte, knownShows map[string]show, from time.Time, location *time.Location) (map[string][]string, error) {
	var response cinemaProgram
	if err := decodeJSON(body, &response); err != nil {
		return nil, err
	}
	if response.Days == nil || response.Shows == nil {
		return nil, fmt.Errorf("cinema program is incomplete")
	}
	result := make(map[string][]string, len(response.Shows))
	for showSlug, item := range response.Shows {
		if _, ok := knownShows[showSlug]; !ok {
			return nil, fmt.Errorf("program references unknown show")
		}
		if item.Days == nil {
			return nil, fmt.Errorf("cinema program is incomplete")
		}
		seenDates := map[string]bool{}
		for rawDate := range item.Days {
			date, err := time.ParseInLocation("2006-01-02", rawDate, location)
			if err != nil || date.Format("2006-01-02") != rawDate {
				return nil, fmt.Errorf("program contains invalid advertised date")
			}
			if date.Before(from) || seenDates[rawDate] {
				continue
			}
			seenDates[rawDate] = true
			result[showSlug] = append(result[showSlug], rawDate)
		}
		sort.Strings(result[showSlug])
		if len(result[showSlug]) == 0 {
			delete(result, showSlug)
		}
	}
	return result, nil
}

func parseSession(item sessionResponse, movie show, theater cinema, advertisedDate string, location *time.Location) (schedule.ShowtimeRecord, error) {
	start, validStart := parseProviderTime(item.Time, location)
	if !validStart {
		return schedule.ShowtimeRecord{}, fmt.Errorf("showtime has invalid start time")
	}
	serviceDate := start.Format("2006-01-02")
	if start.Hour() <= 2 {
		serviceDate = start.AddDate(0, 0, -1).Format("2006-01-02")
	} else if start.Hour() < 8 {
		return schedule.ShowtimeRecord{}, fmt.Errorf("showtime is outside cinema day")
	}
	if serviceDate != advertisedDate {
		return schedule.ShowtimeRecord{}, fmt.Errorf("showtime does not match advertised date")
	}
	language, providerVersion, err := normalizeVersion(item.Version)
	if err != nil {
		return schedule.ShowtimeRecord{}, err
	}
	room, err := auditoriumName(item.AuditoriumName)
	if err != nil || room == "" {
		return schedule.ShowtimeRecord{}, fmt.Errorf("showtime has invalid room")
	}
	bookingURL, showingID, err := canonicalBookingURL(strings.TrimSpace(item.RefCmd))
	if err != nil {
		return schedule.ShowtimeRecord{}, err
	}
	runtime, validRuntime := schedule.RuntimeDuration(movie.runtime)
	if !validRuntime {
		return schedule.ShowtimeRecord{}, fmt.Errorf("showtime has invalid movie runtime")
	}
	end, validEnd := parseProviderTime(item.EndTime, location)
	if !validEnd || !end.After(start) {
		end = start.Add(runtime)
	}
	return schedule.ShowtimeRecord{
		Provider:          schedule.ProviderPathe,
		ID:                "pathe-showing-" + showingID,
		ProviderShowingID: showingID,
		ServiceDate:       serviceDate,
		TheaterID:         "pathe-" + theater.slug,
		Movie: schedule.MovieRecord{
			Provider:       schedule.ProviderPathe,
			ProviderID:     movie.slug,
			Slug:           "pathe-film-" + movie.slug,
			Title:          movie.title,
			RuntimeMinutes: movie.runtime,
			PosterURL:      movie.poster,
			Genres:         append([]string(nil), movie.genres...),
		},
		StartTime:       start,
		EndTime:         end,
		Language:        language,
		ProviderVersion: providerVersion,
		Format:          normalizeFormat(item.Tags),
		Room:            room,
		BookingURL:      bookingURL,
	}, nil
}

func parseProviderTime(raw string, location *time.Location) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	parsed, err := time.ParseInLocation(providerTimeLayout, value, location)
	return parsed, err == nil && parsed.Format(providerTimeLayout) == value
}

func normalizeVersion(raw string) (schedule.Language, string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "vf":
		return schedule.LanguageVF, value, nil
	case "vost":
		return schedule.LanguageVOSTFR, value, nil
	case "vo":
		return schedule.LanguageVO, value, nil
	case "vfst":
		return schedule.LanguageVFSME, value, nil
	default:
		return "", "", fmt.Errorf("showtime has unknown version")
	}
}

func normalizeFormat(tags []string) schedule.Format {
	available := map[string]bool{}
	for _, tag := range tags {
		value := strings.ToUpper(strings.TrimSpace(tag))
		value = strings.NewReplacer(" ", "", "-", "", "_", "").Replace(value)
		available[value] = true
	}
	for _, candidate := range []struct {
		tags   []string
		format schedule.Format
	}{
		{[]string{"IMAX"}, schedule.FormatIMAX},
		{[]string{"4DX"}, schedule.Format4DX},
		{[]string{"ICE"}, schedule.FormatICE},
		{[]string{"DOLBY", "DOLBYATMOS", "DOLBYCINEMA"}, schedule.FormatDolby},
		{[]string{"SCREENX"}, schedule.FormatScreenX},
		{[]string{"3D"}, schedule.Format3D},
	} {
		for _, tag := range candidate.tags {
			if available[tag] {
				return candidate.format
			}
		}
	}
	return schedule.Format2D
}

func auditoriumName(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", fmt.Errorf("missing room")
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text), nil
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&number) != nil {
		return "", fmt.Errorf("invalid room")
	}
	value := number.String()
	if _, err := strconv.ParseUint(value, 10, 64); err != nil {
		return "", fmt.Errorf("invalid room")
	}
	return value, nil
}

func canonicalBookingURL(raw string) (string, string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "s.pathe.fr" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Opaque != "" || parsed.ForceQuery || strings.Contains(parsed.Path, `\`) || hasTraversal(parsed.Path) {
		return "", "", fmt.Errorf("showtime has invalid booking URL")
	}
	parts := strings.Split(parsed.Path, "/")
	if len(parts) != 4 || parts[0] != "" || parts[1] != "fr" || parts[2] == "" || parts[3] != "booking" || !slugPattern.MatchString(parts[2]) {
		return "", "", fmt.Errorf("showtime has invalid booking URL")
	}
	showingID := parts[2]
	if !showingIDPattern.MatchString(showingID) || len(showingID) > maxShowingIDLength {
		return "", "", fmt.Errorf("showtime has invalid booking URL")
	}
	return parsed.String(), showingID, nil
}

func validSourceSlug(value string, maxLength int) bool {
	return len(value) <= maxLength && slugPattern.MatchString(value)
}

func safePosterURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Opaque != "" || parsed.ForceQuery || parsed.Path == "" || parsed.Path == "/" || strings.Contains(parsed.Path, `\`) || hasTraversal(parsed.Path) {
		return ""
	}
	if parsed.IsAbs() {
		host := strings.ToLower(parsed.Hostname())
		if parsed.Scheme != "https" || parsed.Host != host || host != "pathe.fr" && !strings.HasSuffix(host, ".pathe.fr") {
			return ""
		}
		return parsed.String()
	}
	if parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") {
		return ""
	}
	return APIBaseURL + parsed.Path
}
