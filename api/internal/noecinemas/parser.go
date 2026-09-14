package noecinemas

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"messeances/api/internal/schedule"
)

const maxSessionIDBytes = 4096

func decodeJSON(body []byte, out any) error {
	if len(body) == 0 || len(body) > MaxResponseBytes {
		return fmt.Errorf("invalid JSON response size")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("malformed JSON response")
	}
	return nil
}
func parseCinemas(body []byte) ([]cinema, error) {
	var r struct {
		Data struct{ AllTheater struct{ Nodes []cinema } }
	}
	if err := decodeJSON(body, &r); err != nil {
		return nil, err
	}
	items := r.Data.AllTheater.Nodes
	if len(items) == 0 {
		return nil, fmt.Errorf("empty cinema list")
	}
	seen := map[string]bool{}
	for i := range items {
		c := &items[i]
		l := &c.PracticalInfo.Location
		c.Name = strings.TrimSpace(c.Name)
		l.Address, l.City, l.Zip = strings.TrimSpace(l.Address), strings.TrimSpace(l.City), strings.TrimSpace(l.Zip)
		if !schedule.ValidNoeCinemasIdentity("theater", c.ID) || seen[c.ID] || c.TimeZone != schedule.Timezone || c.Name == "" || l.Address == "" || l.City == "" || l.Zip == "" {
			return nil, fmt.Errorf("invalid cinema metadata")
		}
		seen[c.ID] = true
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}
func parseProgram(body []byte, from string, location *time.Location) (map[string][]string, error) {
	var r programResponse
	if err := decodeJSON(body, &r); err != nil {
		return nil, err
	}
	if r.ScheduledDays == nil {
		return nil, fmt.Errorf("missing scheduled days")
	}
	for _, index := range [][]sourceID{r.MovieIDs.TitleAsc, r.MovieIDs.ReleaseAsc} {
		if index == nil {
			continue
		}
		seen := map[string]bool{}
		for _, raw := range index {
			id := string(raw)
			if !schedule.ValidNoeCinemasIdentity("movie", id) || seen[id] {
				return nil, fmt.Errorf("invalid movie index")
			}
			seen[id] = true
		}
		for id := range r.ScheduledDays {
			if !seen[id] {
				return nil, fmt.Errorf("incomplete movie index")
			}
		}
	}
	result := map[string][]string{}
	for id, dates := range r.ScheduledDays {
		if !schedule.ValidNoeCinemasIdentity("movie", id) || dates == nil {
			return nil, fmt.Errorf("invalid scheduled movie")
		}
		seen := map[string]bool{}
		for _, raw := range dates {
			date, valid := sourceDate(raw)
			if !valid {
				return nil, fmt.Errorf("invalid advertised date")
			}
			if _, err := time.ParseInLocation(time.DateOnly, date, location); err != nil {
				return nil, fmt.Errorf("invalid advertised date")
			}
			if date >= from && !seen[date] {
				result[id] = append(result[id], date)
				seen[date] = true
			}
		}
		sort.Strings(result[id])
	}
	return result, nil
}
func sourceDate(raw string) (string, bool) {
	if len(raw) > 10 {
		if _, err := time.Parse(time.RFC3339, raw); err != nil {
			return "", false
		}
		raw = raw[:10]
	}
	d, err := time.Parse(time.DateOnly, raw)
	return raw, err == nil && d.Format(time.DateOnly) == raw
}
func parseMovies(body []byte) (map[string]schedule.MovieRecord, error) {
	var items []movieResponse
	if err := decodeJSON(body, &items); err != nil {
		return nil, err
	}
	if items == nil {
		return nil, fmt.Errorf("missing movies")
	}
	result := map[string]schedule.MovieRecord{}
	for _, m := range items {
		id := string(m.ID)
		if !schedule.ValidNoeCinemasIdentity("movie", id) || result[id].ProviderID != "" || strings.TrimSpace(m.Title) == "" {
			return nil, fmt.Errorf("invalid movie metadata")
		}
		runtime := 0
		if m.Runtime != nil {
			runtime = *m.Runtime / 60
			if *m.Runtime <= 0 || runtime == 0 {
				return nil, fmt.Errorf("invalid movie runtime")
			}
			if _, valid := schedule.RuntimeDuration(runtime); !valid {
				return nil, fmt.Errorf("invalid movie runtime")
			}
		}
		poster, valid := schedule.NoeCinemasSourcePosterURL(m.Poster)
		if !valid {
			return nil, fmt.Errorf("invalid movie poster")
		}
		genres := []string{}
		for _, genre := range m.Genres {
			genre = strings.TrimSpace(genre)
			if genre != "" && !slices.Contains(genres, genre) {
				genres = append(genres, genre)
			}
		}
		release := ""
		if m.ReleaseDate != "" {
			release, valid = sourceDate(m.ReleaseDate)
			if !valid {
				return nil, fmt.Errorf("invalid release date")
			}
		}
		result[id] = schedule.MovieRecord{Provider: schedule.ProviderNoeCinemas, ProviderID: id, Slug: "noecinemas-film-" + id, Title: strings.TrimSpace(m.Title), RuntimeMinutes: runtime, PosterURL: poster, Genres: genres, Overview: strings.TrimSpace(m.Synopsis), ReleaseDate: release}
	}
	return result, nil
}
func parseSchedule(body []byte, c cinema, program map[string][]string, movies map[string]schedule.MovieRecord, location *time.Location, allowMissing string) ([]schedule.ShowtimeRecord, error) {
	var r scheduleResponse
	if err := decodeJSON(body, &r); err != nil {
		return nil, err
	}
	container, ok := r[c.ID]
	if !ok || len(r) != 1 || container.Schedule == nil {
		return nil, fmt.Errorf("incomplete schedule response")
	}
	// A program that changed between requests must not silently lose new films
	// or dates. The caller retries the entire snapshot once, never a partial one.
	for id, dates := range container.Schedule {
		if !schedule.ValidNoeCinemasIdentity("movie", id) || dates == nil {
			return nil, fmt.Errorf("invalid schedule movie")
		}
		if _, ok := program[id]; !ok {
			return nil, errSnapshotChanged
		}
		for date := range dates {
			if parsed, valid := sourceDate(date); !valid || parsed != date {
				return nil, fmt.Errorf("invalid schedule date")
			}
			if !slices.Contains(program[id], date) {
				return nil, errSnapshotChanged
			}
		}
	}
	result := []schedule.ShowtimeRecord{}
	seen := map[string]schedule.ShowtimeRecord{}
	for id, dates := range program {
		m, ok := movies[id]
		if !ok {
			return nil, fmt.Errorf("unknown schedule movie")
		}
		for _, date := range dates {
			sessions := container.Schedule[id][date]
			if len(sessions) == 0 {
				if date == allowMissing {
					continue
				}
				return nil, errSnapshotChanged
			}
			for _, s := range sessions {
				record, err := parseShowtime(s, c, m, date, location)
				if err != nil {
					return nil, err
				}
				if prior, ok := seen[record.ID]; ok {
					if !reflect.DeepEqual(prior, record) {
						return nil, fmt.Errorf("conflicting showtime identity")
					}
					continue
				}
				seen[record.ID] = record
				result = append(result, record)
			}
		}
	}
	return result, nil
}
func parseShowtime(s showtimeResponse, c cinema, m schedule.MovieRecord, date string, location *time.Location) (schedule.ShowtimeRecord, error) {
	bad := func(message string) (schedule.ShowtimeRecord, error) {
		return schedule.ShowtimeRecord{}, fmt.Errorf("%s", message)
	}
	if strings.TrimSpace(s.ID) == "" || len(s.ID) > maxSessionIDBytes || strings.ContainsRune(s.ID, 0) {
		return bad("invalid showtime identity")
	}
	start, valid := parseStartTime(s.StartsAt, location)
	if !valid {
		return bad("invalid showtime start")
	}
	service := start
	if service.Hour() < 3 {
		service = service.AddDate(0, 0, -1)
	}
	if service.Format(time.DateOnly) != date {
		return bad("inconsistent service date")
	}
	language, version, err := normalizeVersion(s.Tags)
	if err != nil {
		return bad("unknown showtime language")
	}
	booking, count := "", 0
	for _, entry := range s.Data.Ticketing {
		if entry.Provider == "default" && entry.Type == "DESKTOP" {
			count++
			if len(entry.URLs) != 1 {
				return bad("invalid ticketing URL")
			}
			booking = entry.URLs[0]
		}
	}
	if count != 1 || !schedule.ValidNoeCinemasBookingURL(booking, c.ID) {
		return bad("invalid ticketing URL")
	}
	room := ""
	if s.Screen != nil {
		room = strings.TrimSpace(s.Screen.Name)
	}
	// Exact source session text is cinema-scoped. Mutable metadata is excluded.
	sum := sha256.Sum256([]byte(c.ID + "\x00" + s.ID))
	id := c.ID + "-" + hex.EncodeToString(sum[:])
	return schedule.ShowtimeRecord{Provider: schedule.ProviderNoeCinemas, ID: "noecinemas-showing-" + id, ProviderShowingID: id, TheaterID: "noecinemas-" + c.ID, ServiceDate: date, Movie: m, StartTime: start, EndTime: start, Language: language, ProviderVersion: version, Format: normalizeFormat(s.Tags), Room: room, BookingURL: booking}, nil
}
func parseStartTime(raw string, location *time.Location) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		local := t.In(location)
		return local, local.Format(time.RFC3339) == t.Format(time.RFC3339)
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, raw, location); err == nil && t.Format(layout) == raw {
			return t, true
		}
	}
	return time.Time{}, false
}

var languagePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,15}$`)

func normalizeVersion(tags []string) (schedule.Language, string, error) {
	available := map[string]bool{}
	for _, tag := range tags {
		available[strings.TrimSpace(tag)] = true
	}
	if available["Localization.Version.Original"] {
		return schedule.LanguageVOSTFR, "VOSTFR", nil
	}
	if available["Localization.Language.French"] && available["Showtime.Accessibility.OpenCaption"] {
		return schedule.Language("VFSTF"), "VFSTF", nil
	}
	if available["Localization.Language.French"] || available["Showtime.Accessibility.Dubbed"] {
		return schedule.LanguageVF, "VF", nil
	}
	for _, tag := range tags {
		if value, ok := strings.CutPrefix(tag, "Localization.Language."); ok {
			value = strings.ToUpper(value)
			if value != "ALL" && languagePattern.MatchString(value) {
				return schedule.Language(value), value, nil
			}
		}
	}
	return "", "", fmt.Errorf("unknown language")
}
func normalizeFormat(tags []string) schedule.Format {
	for _, m := range []struct {
		tag    string
		format schedule.Format
	}{{"Auditorium.Experience.DolbyAtmos", schedule.FormatDolby}, {"Format.Projection.3d", schedule.Format3D}} {
		for _, tag := range tags {
			if strings.EqualFold(tag, m.tag) {
				return m.format
			}
		}
	}
	return schedule.Format2D
}
