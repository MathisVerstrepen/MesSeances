package cgr

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"messeances/api/internal/schedule"
)

var (
	theaterIDPattern   = regexp.MustCompile(`^[A-Z][0-9]{4}$`)
	movieIDPattern     = regexp.MustCompile(`^[1-9][0-9]{0,127}$`)
	languagePattern    = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,15}$`)
	cinemaPathPattern  = regexp.MustCompile(`^/theaters/[a-z0-9-]+$`)
	bookingPathPattern = regexp.MustCompile(`^/[a-z0-9-]+/r/[1-9][0-9]*$`)
)

type cinema struct {
	id, name, path, timeZone, address, city, postalCode string
}

type movie struct {
	id, title, poster string
	runtime           int
	genres            []string
}

func decodeJSON(body []byte, destination any) error {
	if len(body) == 0 || len(body) > MaxResponseBytes {
		return fmt.Errorf("invalid JSON response size")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("malformed JSON response")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("malformed JSON response")
	}
	return nil
}

func parseCinemas(body []byte) ([]cinema, error) {
	var response struct {
		Data struct {
			AllTheater struct {
				Nodes []cinemaResponse `json:"nodes"`
			} `json:"allTheater"`
		} `json:"data"`
	}
	if err := decodeJSON(body, &response); err != nil {
		return nil, err
	}
	candidates := response.Data.AllTheater.Nodes
	if len(candidates) == 0 {
		return nil, fmt.Errorf("cinema list is empty")
	}
	seen := make(map[string]bool, len(candidates))
	result := make([]cinema, 0, len(candidates))
	for _, item := range candidates {
		id, name, path := strings.TrimSpace(item.ID), strings.TrimSpace(item.Name), strings.TrimSpace(item.Path)
		location := item.PracticalInfo.Location
		address, city, postalCode := strings.TrimSpace(location.Address), strings.TrimSpace(location.City), strings.TrimSpace(location.Zip)
		if !validTheaterID(id) || seen[id] || name == "" || address == "" || city == "" || postalCode == "" || item.TimeZone != schedule.Timezone || !cinemaPathPattern.MatchString(path) || !strings.HasPrefix(path, "/theaters/"+strings.ToLower(id)+"-") {
			return nil, fmt.Errorf("cinema metadata is invalid")
		}
		seen[id] = true
		result = append(result, cinema{id: id, name: name, path: path, timeZone: item.TimeZone, address: address, city: city, postalCode: postalCode})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result, nil
}

func parseProgram(body []byte, from time.Time, location *time.Location) (map[string][]string, error) {
	var response scheduledMoviesResponse
	if err := decodeJSON(body, &response); err != nil {
		return nil, err
	}
	indexes := [][]sourceID{response.MovieIDs.ReleaseAsc, response.MovieIDs.ReleaseDesc, response.MovieIDs.TitleAsc}
	if response.ScheduledDays == nil || indexes[0] == nil || indexes[1] == nil || indexes[2] == nil {
		return nil, fmt.Errorf("scheduled movies response is incomplete")
	}
	indexed := make(map[string]bool, len(indexes[0]))
	for index, values := range indexes {
		seen := make(map[string]bool, len(values))
		for _, rawID := range values {
			id := strings.TrimSpace(string(rawID))
			if !validMovieID(id) || seen[id] || index > 0 && !indexed[id] {
				return nil, fmt.Errorf("scheduled movie index is invalid")
			}
			seen[id] = true
			if index == 0 {
				indexed[id] = true
			}
		}
		if len(seen) != len(indexed) {
			return nil, fmt.Errorf("scheduled movie indexes are inconsistent")
		}
	}
	result := make(map[string][]string, len(response.ScheduledDays))
	for rawID, dates := range response.ScheduledDays {
		id := strings.TrimSpace(rawID)
		if !validMovieID(id) || !indexed[id] || dates == nil {
			return nil, fmt.Errorf("scheduled movies response is invalid")
		}
		seen := map[string]bool{}
		for _, rawDate := range dates {
			normalizedDate, date, ok := advertisedDate(rawDate, location)
			if !ok {
				return nil, fmt.Errorf("scheduled movie date is invalid")
			}
			if date.Before(from) || seen[normalizedDate] {
				continue
			}
			seen[normalizedDate] = true
			result[id] = append(result[id], normalizedDate)
		}
		sort.Strings(result[id])
		if len(result[id]) == 0 {
			delete(result, id)
		}
	}
	return result, nil
}

func parseMovies(body []byte) (map[string]movie, error) {
	var raw json.RawMessage
	if err := decodeJSON(body, &raw); err != nil {
		return nil, err
	}
	var items []movieResponse
	if err := json.Unmarshal(raw, &items); err != nil {
		var wrapper struct {
			Movies []movieResponse `json:"movies"`
		}
		if json.Unmarshal(raw, &wrapper) == nil && wrapper.Movies != nil {
			items = wrapper.Movies
		} else {
			var single movieResponse
			if json.Unmarshal(raw, &single) != nil || single.ID == "" {
				return nil, fmt.Errorf("movie response is invalid")
			}
			items = []movieResponse{single}
		}
	}
	if items == nil {
		return nil, fmt.Errorf("movie response is incomplete")
	}
	result := make(map[string]movie, len(items))
	for _, item := range items {
		id, title := strings.TrimSpace(string(item.ID)), strings.TrimSpace(item.Title)
		if !validMovieID(id) || title == "" || result[id].id != "" {
			return nil, fmt.Errorf("movie metadata is invalid")
		}
		runtime := 0
		if item.Runtime != nil {
			if *item.Runtime <= 0 {
				return nil, fmt.Errorf("movie runtime is invalid")
			}
			runtime = *item.Runtime / 60
			if runtime == 0 {
				return nil, fmt.Errorf("movie runtime is invalid")
			}
		}
		genres := make([]string, 0, len(item.Genres))
		seenGenres := make(map[string]bool, len(item.Genres))
		for _, rawGenre := range item.Genres {
			genre := strings.TrimSpace(rawGenre)
			if genre == "" || seenGenres[genre] {
				continue
			}
			seenGenres[genre] = true
			genres = append(genres, genre)
		}
		result[id] = movie{id: id, title: title, runtime: runtime, poster: safePosterURL(strings.TrimSpace(item.Poster)), genres: genres}
	}
	return result, nil
}

func parseSchedule(body []byte, theater cinema, program map[string][]string, movies map[string]movie, location *time.Location, allowMissingDate string) ([]schedule.ShowtimeRecord, error) {
	var response scheduleResponse
	if err := decodeJSON(body, &response); err != nil {
		return nil, err
	}
	container, ok := response[theater.id]
	if !ok || len(response) != 1 || container.Schedule == nil {
		return nil, fmt.Errorf("schedule response is incomplete")
	}
	result := []schedule.ShowtimeRecord{}
	seenShowtimes := make(map[string]schedule.ShowtimeRecord)
	for movieID, dates := range program {
		item, ok := movies[movieID]
		if !ok {
			return nil, fmt.Errorf("schedule references unknown movie")
		}
		byDate, ok := container.Schedule[movieID]
		if !ok || byDate == nil {
			byDate = map[string][]showtimeResponse{}
		}
		for _, date := range dates {
			sessions, ok := byDate[date]
			if !ok || sessions == nil {
				if date == allowMissingDate {
					continue
				}
				return nil, fmt.Errorf("%w: advertised movie date is missing: theater=%s movie=%s date=%s", errProviderSnapshotChanged, theater.id, movieID, date)
			}
			for _, session := range sessions {
				record, err := parseShowtime(session, theater, item, date, location)
				if err != nil {
					return nil, err
				}
				if prior, exists := seenShowtimes[record.ProviderShowingID]; exists {
					if !reflect.DeepEqual(prior, record) {
						return nil, fmt.Errorf("duplicate showtime identity has conflicting data: theater=%s showing=%s", theater.id, record.ProviderShowingID)
					}
					continue
				}
				seenShowtimes[record.ProviderShowingID] = record
				result = append(result, record)
			}
		}
	}
	return result, nil
}

func parseShowtime(item showtimeResponse, theater cinema, movie movie, advertisedDate string, location *time.Location) (schedule.ShowtimeRecord, error) {
	rawID := strings.TrimSpace(item.ID)
	if rawID == "" {
		return schedule.ShowtimeRecord{}, fmt.Errorf("showtime identity is missing")
	}
	start, ok := parseStartTime(item.StartsAt, location)
	if !ok {
		return schedule.ShowtimeRecord{}, fmt.Errorf("showtime start is invalid")
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
	language, providerVersion, err := normalizeVersion(item.Tags)
	if err != nil {
		return schedule.ShowtimeRecord{}, err
	}
	bookingURL, err := ticketingURL(item.Data.Ticketing)
	if err != nil {
		return schedule.ShowtimeRecord{}, err
	}
	room := ""
	if item.Screen != nil {
		room = strings.TrimSpace(item.Screen.Name)
	}
	providerShowingID := showingIdentity(theater.id, rawID, start.Format("2006-01-02T15:04:05"), room, bookingURL)
	end := start
	if movie.runtime > 0 {
		end = start.Add(time.Duration(movie.runtime) * time.Minute)
	}
	return schedule.ShowtimeRecord{
		Provider: schedule.ProviderCGR, ID: "cgr-showing-" + providerShowingID, ProviderShowingID: providerShowingID,
		ServiceDate: serviceDate, TheaterID: "cgr-" + theater.id,
		Movie:     schedule.MovieRecord{Provider: schedule.ProviderCGR, ProviderID: movie.id, Slug: "cgr-film-" + movie.id, Title: movie.title, RuntimeMinutes: movie.runtime, PosterURL: movie.poster, Genres: append([]string(nil), movie.genres...)},
		StartTime: start, EndTime: end, Language: language, ProviderVersion: providerVersion, Format: normalizeFormat(item.Tags), Room: room, BookingURL: bookingURL,
	}, nil
}

func parseStartTime(raw string, location *time.Location) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		local := parsed.In(location)
		return local, local.Format(time.RFC3339) == parsed.Format(time.RFC3339)
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if parsed, err := time.ParseInLocation(layout, value, location); err == nil && parsed.Format(layout) == value {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func normalizeVersion(tags []string) (schedule.Language, string, error) {
	available := make(map[string]bool, len(tags))
	for _, tag := range tags {
		available[strings.TrimSpace(tag)] = true
	}
	if available["Localization.Version.Original"] && available["Showtime.Accessibility.Subtitled"] {
		return schedule.LanguageVOSTFR, "Localization.Version.Original+Showtime.Accessibility.Subtitled", nil
	}
	if available["Localization.Language.French"] {
		return schedule.LanguageVF, "Localization.Language.French", nil
	}
	if available["Localization.Version.Original"] {
		return schedule.LanguageVO, "Localization.Version.Original", nil
	}
	const prefix = "Localization.Language."
	for _, tag := range tags {
		if strings.HasPrefix(tag, prefix) {
			value := strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(tag, prefix)))
			if value != string(schedule.LanguageAll) && languagePattern.MatchString(value) {
				return schedule.Language(value), tag, nil
			}
		}
	}
	return "", "", fmt.Errorf("showtime language is unknown")
}

func normalizeFormat(tags []string) schedule.Format {
	for _, mapping := range []struct {
		tag    string
		format schedule.Format
	}{{"Auditorium.Experience.Ice", schedule.FormatICE}, {"Auditorium.Experience.DolbyAtmos", schedule.FormatDolby}, {"Format.Projection.3d", schedule.Format3D}} {
		for _, tag := range tags {
			if strings.EqualFold(strings.TrimSpace(tag), mapping.tag) {
				return mapping.format
			}
		}
	}
	return schedule.Format2D
}

func ticketingURL(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", fmt.Errorf("showtime ticketing URL is missing")
	}
	var entries []ticketingResponse
	if json.Unmarshal(raw, &entries) != nil || entries == nil {
		return "", fmt.Errorf("showtime ticketing URL is invalid")
	}
	candidate := ""
	for _, entry := range entries {
		if entry.Provider != "default" || entry.Type != "DESKTOP" {
			continue
		}
		if candidate != "" || len(entry.URLs) != 1 {
			return "", fmt.Errorf("showtime ticketing URL is invalid")
		}
		candidate = strings.TrimSpace(entry.URLs[0])
	}
	parsed, err := url.Parse(candidate)
	if err != nil || len(candidate) > 2048 || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.RawPath != "" || parsed.Fragment != "" || parsed.Opaque != "" || parsed.ForceQuery || strings.Contains(parsed.Path, `\`) || hasTraversal(parsed.Path) {
		return "", fmt.Errorf("showtime ticketing URL is invalid")
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Host != host || host != "achat.cgrcinemas.fr" || !bookingPathPattern.MatchString(parsed.Path) {
		return "", fmt.Errorf("showtime ticketing URL is invalid")
	}
	return parsed.String(), nil
}

func safePosterURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Opaque != "" || parsed.ForceQuery || parsed.Path == "" || parsed.Path == "/" || strings.Contains(parsed.Path, `\`) || hasTraversal(parsed.Path) {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Host != host || host != "acsta.net" && !strings.HasSuffix(host, ".acsta.net") {
		return ""
	}
	return parsed.String()
}

func showingIdentity(theaterID string, sourceParts ...string) string {
	sum := sha256.Sum256([]byte(theaterID + "\x00" + strings.Join(sourceParts, "\x00")))
	return theaterID + "-" + hex.EncodeToString(sum[:])
}

func validTheaterID(value string) bool { return theaterIDPattern.MatchString(value) }
func validMovieID(value string) bool   { return movieIDPattern.MatchString(value) }
func validDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func validScheduleWindow(from, to string) bool {
	const layout = "2006-01-02T15:04:05"
	start, startErr := time.Parse(layout, from)
	end, endErr := time.Parse(layout, to)
	return startErr == nil && endErr == nil && start.Format(layout) == from && end.Format(layout) == to && start.Hour() == 3 && end.Hour() == 3 && end.After(start)
}

func advertisedDate(value string, location *time.Location) (string, time.Time, bool) {
	if parsed, err := time.ParseInLocation("2006-01-02", value, location); err == nil && parsed.Format("2006-01-02") == value {
		return value, parsed, true
	}
	if len(value) < len("2006-01-02") {
		return "", time.Time{}, false
	}
	prefix := value[:len("2006-01-02")]
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		return "", time.Time{}, false
	}
	parsed, err := time.ParseInLocation("2006-01-02", prefix, location)
	return prefix, parsed, err == nil
}
