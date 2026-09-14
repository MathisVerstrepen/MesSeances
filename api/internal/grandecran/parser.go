package grandecran

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"messeances/api/internal/schedule"
)

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
		l.Address = strings.TrimSpace(l.Address)
		l.City = strings.TrimSpace(l.City)
		l.Zip = strings.TrimSpace(l.Zip)
		if !schedule.ValidGrandEcranIdentity("theater", c.ID) || seen[c.ID] || c.TimeZone != schedule.Timezone || c.Name == "" || l.Address == "" || l.City == "" || l.Zip == "" {
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
	// Only indexes actually supplied by this platform are validated.
	for _, index := range [][]sourceID{r.MovieIDs.TitleAsc, r.MovieIDs.ReleaseAsc} {
		if index == nil {
			continue
		}
		seen := map[string]bool{}
		for _, raw := range index {
			id := string(raw)
			if !schedule.ValidGrandEcranIdentity("movie", id) || seen[id] {
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
		if !schedule.ValidGrandEcranIdentity("movie", id) || dates == nil {
			return nil, fmt.Errorf("invalid scheduled movie")
		}
		seen := map[string]bool{}
		for _, raw := range dates {
			date := raw
			if len(raw) > 10 {
				if _, err := time.Parse(time.RFC3339, raw); err != nil {
					return nil, fmt.Errorf("invalid advertised date")
				}
				date = raw[:10]
			}
			parsed, err := time.ParseInLocation(time.DateOnly, date, location)
			if err != nil || parsed.Format(time.DateOnly) != date {
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
func parseMovies(body []byte) (map[string]schedule.MovieRecord, error) {
	var raw json.RawMessage
	if err := decodeJSON(body, &raw); err != nil {
		return nil, err
	}
	var items []movieResponse
	if json.Unmarshal(raw, &items) != nil {
		var wrapper struct {
			Movies []movieResponse `json:"movies"`
		}
		if json.Unmarshal(raw, &wrapper) == nil && wrapper.Movies != nil {
			items = wrapper.Movies
		} else {
			var single movieResponse
			if json.Unmarshal(raw, &single) != nil || single.ID == "" {
				return nil, fmt.Errorf("invalid movie response")
			}
			items = []movieResponse{single}
		}
	}
	if items == nil {
		return nil, fmt.Errorf("missing movies")
	}
	result := map[string]schedule.MovieRecord{}
	for _, m := range items {
		id := string(m.ID)
		if !schedule.ValidGrandEcranIdentity("movie", id) || result[id].ProviderID != "" || strings.TrimSpace(m.Title) == "" {
			return nil, fmt.Errorf("invalid movie metadata")
		}
		runtime := 0
		if m.Runtime != nil {
			runtime = *m.Runtime / 60
			if *m.Runtime <= 0 || runtime == 0 {
				return nil, fmt.Errorf("invalid movie runtime")
			}
			if _, ok := schedule.RuntimeDuration(runtime); !ok {
				return nil, fmt.Errorf("invalid movie runtime")
			}
		}
		poster, validPoster := schedule.GrandEcranSourcePosterURL(m.Poster)
		if !validPoster {
			return nil, fmt.Errorf("invalid movie poster")
		}
		genres := []string{}
		seen := map[string]bool{}
		for _, g := range m.Genres {
			g = strings.TrimSpace(g)
			if g != "" && !seen[g] {
				genres = append(genres, g)
				seen[g] = true
			}
		}
		release := m.ReleaseDate
		if release != "" {
			if len(release) > 10 {
				if _, err := time.Parse(time.RFC3339, release); err != nil {
					return nil, fmt.Errorf("invalid release date")
				}
				release = release[:10]
			}
			if d, err := time.Parse(time.DateOnly, release); err != nil || d.Format(time.DateOnly) != release {
				return nil, fmt.Errorf("invalid release date")
			}
		}
		result[id] = schedule.MovieRecord{Provider: schedule.ProviderGrandEcran, ProviderID: id, Slug: "grandecran-film-" + id, Title: strings.TrimSpace(m.Title), RuntimeMinutes: runtime, PosterURL: poster, Genres: genres, Overview: strings.TrimSpace(m.Synopsis), ReleaseDate: release}
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
	if strings.TrimSpace(s.ID) == "" || strings.ContainsRune(s.ID, 0) {
		return bad("invalid showtime identity")
	}
	start, ok := parseStartTime(s.StartsAt, location)
	if !ok {
		return bad("invalid showtime start")
	}
	service := start.Format(time.DateOnly)
	if start.Hour() < 3 {
		service = start.AddDate(0, 0, -1).Format(time.DateOnly)
	}
	if service != date {
		return bad("inconsistent service date")
	}
	language, version, err := normalizeVersion(s.Tags)
	if err != nil {
		return bad("unknown showtime language")
	}
	booking := ""
	count := 0
	for _, entry := range s.Data.Ticketing {
		if entry.Provider == "default" && entry.Type == "DESKTOP" {
			count++
			if len(entry.URLs) != 1 {
				return bad("invalid ticketing URL")
			}
			booking = entry.URLs[0]
		}
	}
	if count != 1 || !schedule.ValidGrandEcranBookingURL(booking) {
		return bad("invalid ticketing URL")
	}
	room := ""
	if s.Screen != nil {
		room = strings.TrimSpace(s.Screen.Name)
	}
	parts := []string{c.ID, m.ProviderID, s.ID, start.Format("2006-01-02T15:04:05"), booking}
	for _, part := range parts {
		if strings.ContainsRune(part, 0) {
			return bad("invalid identity component")
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	id := c.ID + "-" + hex.EncodeToString(sum[:])
	return schedule.ShowtimeRecord{Provider: schedule.ProviderGrandEcran, ID: "grandecran-showing-" + id, ProviderShowingID: id, TheaterID: "grandecran-" + c.ID, ServiceDate: date, Movie: m, StartTime: start, EndTime: start, Language: language, ProviderVersion: version, Format: normalizeFormat(s.Tags), Room: room, BookingURL: booking}, nil
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
	if available["Localization.Language.French"] {
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
