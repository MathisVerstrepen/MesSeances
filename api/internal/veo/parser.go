package veo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"messeances/api/internal/schedule"
)

var errShape = errors.New("invalid Véo response structure")
var errSnapshotChanged = errors.New("véo advertised index and schedule disagree")

type sourceID string

func (id *sourceID) UnmarshalJSON(b []byte) error {
	var s string
	if len(b) > 0 && b[0] == '"' {
		if json.Unmarshal(b, &s) != nil {
			return errShape
		}
	} else {
		s = string(b)
		for _, c := range s {
			if c < '0' || c > '9' {
				return errShape
			}
		}
	}
	if s == "" || len(s) > 2048 || strings.ContainsRune(s, 0) {
		return errShape
	}
	*id = sourceID(s)
	return nil
}

type minutes int

func (m *minutes) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*m = 0
		return nil
	}
	s := string(b)
	if len(b) > 0 && b[0] == '"' {
		if json.Unmarshal(b, &s) != nil {
			return errShape
		}
	}
	n, err := strconv.ParseUint(s, 10, 31)
	if err != nil {
		return errShape
	}
	*m = minutes(n)
	return nil
}

type cinema struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	TimeZone      string `json:"timeZone"`
	PracticalInfo struct {
		Location struct{ Address, City, Zip string } `json:"location"`
	} `json:"practicalInfo"`
	origin     string
	upstreamID string
}

func (c cinema) sourceID() string {
	if c.upstreamID != "" {
		return c.upstreamID
	}
	return c.ID
}

func (c cinema) dedicatedGueret() bool {
	return c.ID == "G06DB" && c.origin == GueretOrigin && c.sourceID() == "P1394"
}

type programResponse struct {
	MovieIDs      *struct{ TitleAsc, ReleaseAsc []sourceID } `json:"movieIds"`
	ScheduledDays map[string][]string                        `json:"scheduledDays"`
}
type movieResponse struct {
	ID      sourceID `json:"id"`
	Title   string   `json:"title"`
	Runtime *int     `json:"runtime"`
	Poster  string   `json:"poster"`
}
type showtimeResponse struct {
	ID       string   `json:"id"`
	StartsAt string   `json:"startsAt"`
	Tags     []string `json:"tags"`
	Screen   *struct {
		Name string `json:"name"`
	} `json:"screen"`
	Data struct {
		Ticketing []struct {
			Provider, Type string
			URLs           []string `json:"urls"`
		} `json:"ticketing"`
	} `json:"data"`
}
type scheduleResponse map[string]struct {
	Schedule map[string]map[string][]showtimeResponse `json:"schedule"`
}

func decodeJSON(b []byte, out any) error {
	if len(b) == 0 || len(b) > MaxResponseBytes {
		return errShape
	}
	d := json.NewDecoder(bytes.NewReader(b))
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return errShape
	}
	return nil
}
func boxTheater(id string) bool { return id != "EMS1361" && schedule.ValidVeoIdentity("theater", id) }
func boxMovie(id string) bool {
	return !strings.HasPrefix(id, "ticketingcine-") && schedule.ValidVeoIdentity("movie", id)
}
func parseCinemas(b []byte, origin string) ([]cinema, error) {
	var r struct {
		Data struct{ AllTheater struct{ Nodes []cinema } }
	}
	if decodeJSON(b, &r) != nil || len(r.Data.AllTheater.Nodes) == 0 {
		return nil, errShape
	}
	seen := map[string]bool{}
	for i := range r.Data.AllTheater.Nodes {
		c := &r.Data.AllTheater.Nodes[i]
		l := &c.PracticalInfo.Location
		c.Name = strings.TrimSpace(c.Name)
		l.Address = strings.TrimSpace(l.Address)
		l.City = strings.TrimSpace(l.City)
		l.Zip = strings.TrimSpace(l.Zip)
		c.origin = origin
		if origin == MainOrigin && c.ID == "G06DB" {
			c.origin, c.upstreamID = GueretOrigin, "P1394"
		}
		if !boxTheater(c.ID) || seen[c.ID] || c.Name == "" || l.Address == "" || l.City == "" || l.Zip == "" || c.TimeZone != schedule.Timezone {
			return nil, errShape
		}
		seen[c.ID] = true
	}
	sort.Slice(r.Data.AllTheater.Nodes, func(i, j int) bool { return r.Data.AllTheater.Nodes[i].ID < r.Data.AllTheater.Nodes[j].ID })
	return r.Data.AllTheater.Nodes, nil
}
func parseProgram(b []byte, from string, loc *time.Location) (map[string][]string, error) {
	var r programResponse
	if decodeJSON(b, &r) != nil || r.MovieIDs == nil || r.ScheduledDays == nil {
		return nil, errShape
	}
	for _, index := range [][]sourceID{r.MovieIDs.TitleAsc, r.MovieIDs.ReleaseAsc} {
		if index == nil {
			return nil, errShape
		}
		seen := map[string]bool{}
		for _, raw := range index {
			id := string(raw)
			if !boxMovie(id) || seen[id] {
				return nil, errShape
			}
			seen[id] = true
		}
		if len(seen) != len(r.ScheduledDays) {
			return nil, errSnapshotChanged
		}
		for id := range r.ScheduledDays {
			if !seen[id] {
				return nil, errSnapshotChanged
			}
		}
	}
	result := map[string][]string{}
	for id, dates := range r.ScheduledDays {
		if !boxMovie(id) || dates == nil {
			return nil, errShape
		}
		seen := map[string]bool{}
		for _, raw := range dates {
			date := raw
			if len(raw) > 10 {
				if _, err := time.Parse(time.RFC3339, raw); err != nil {
					return nil, errShape
				}
				date = raw[:10]
			}
			parsed, err := time.ParseInLocation(time.DateOnly, date, loc)
			if err != nil || parsed.Format(time.DateOnly) != date {
				return nil, errShape
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
func parseMovies(b []byte) (map[string]schedule.MovieRecord, error) {
	var items []movieResponse
	if decodeJSON(b, &items) != nil || items == nil {
		return nil, errShape
	}
	result := map[string]schedule.MovieRecord{}
	for _, m := range items {
		id := string(m.ID)
		runtime := 0
		if m.Runtime != nil {
			runtime = *m.Runtime / 60
			if *m.Runtime <= 0 || runtime == 0 {
				return nil, errShape
			}
		}
		if runtime > 0 {
			if _, ok := schedule.RuntimeDuration(runtime); !ok {
				return nil, errShape
			}
		}
		poster, ok := schedule.VeoSourcePosterURL(m.Poster, false)
		if !ok || !boxMovie(id) || result[id].ProviderID != "" || strings.TrimSpace(m.Title) == "" {
			return nil, errShape
		}
		result[id] = schedule.MovieRecord{Provider: schedule.ProviderVeo, ProviderID: id, Slug: "veo-film-" + id, Title: strings.TrimSpace(m.Title), RuntimeMinutes: runtime, PosterURL: poster, Genres: []string{}}
	}
	return result, nil
}
func parseSchedule(b []byte, c cinema, p map[string][]string, movies map[string]schedule.MovieRecord, loc *time.Location, allowMissing, from, through string) ([]schedule.ShowtimeRecord, map[string]bool, error) {
	var r scheduleResponse
	if decodeJSON(b, &r) != nil || len(r) != 1 || r[c.sourceID()].Schedule == nil {
		return nil, nil, errShape
	}
	container := r[c.sourceID()].Schedule
	// Never discard positive unadvertised evidence, including an empty index.
	for id, days := range container {
		for date, sessions := range days {
			if len(sessions) > 0 && date >= from && date <= through && !contains(p[id], date) {
				return nil, nil, errSnapshotChanged
			}
		}
	}
	rows := []schedule.ShowtimeRecord{}
	unknown := map[string]bool{}
	groups := map[string][]int{}
	for id, dates := range p {
		m, ok := movies[id]
		if !ok && len(dates) > 0 {
			return nil, nil, errShape
		}
		for _, date := range dates {
			sessions := container[id][date]
			if len(sessions) == 0 {
				if date == allowMissing {
					unknown[date] = true
					continue
				}
				return nil, nil, errSnapshotChanged
			}
			for _, s := range sessions {
				row, err := parseShowtime(s, c, m, date, loc)
				if err != nil {
					return nil, nil, err
				}
				duplicate := false
				for _, index := range groups[s.ID] {
					prior := rows[index]
					if reflect.DeepEqual(prior, row) {
						duplicate = true
						continue
					}
					// Only distinct, known rooms with distinct ticket sessions may
					// share a room-blind source ID. Every other showing fact agrees.
					if prior.Room == "" || row.Room == "" || prior.Room == row.Room || prior.BookingURL == row.BookingURL {
						return nil, nil, errShape
					}
					prior.Room, prior.BookingURL = row.Room, row.BookingURL
					if !reflect.DeepEqual(prior, row) {
						return nil, nil, errShape
					}
				}
				if duplicate {
					continue
				}
				groups[s.ID] = append(groups[s.ID], len(rows))
				rows = append(rows, row)
			}
		}
	}
	// Assign every collision member, including the first, only after the whole
	// group is validated. Ordinary source identities remain unchanged.
	for rawID, indices := range groups {
		if len(indices) < 2 {
			continue
		}
		for _, index := range indices {
			row := &rows[index]
			row.ProviderShowingID = schedule.VeoShowingID(c.ID, rawID+"\x00"+row.BookingURL)
			row.ID = "veo-showing-" + row.ProviderShowingID
		}
	}
	return rows, unknown, nil
}
func contains(items []string, s string) bool {
	for _, i := range items {
		if i == s {
			return true
		}
	}
	return false
}
func parseShowtime(s showtimeResponse, c cinema, m schedule.MovieRecord, date string, loc *time.Location) (schedule.ShowtimeRecord, error) {
	if strings.TrimSpace(s.ID) == "" || len(s.ID) > 4096 || strings.ContainsRune(s.ID, 0) {
		return schedule.ShowtimeRecord{}, errShape
	}
	start, err := parseStart(s.StartsAt, loc)
	if err != nil || serviceDate(start) != date {
		return schedule.ShowtimeRecord{}, errShape
	}
	language, version, err := normalizeVersion(s.Tags)
	if err != nil {
		return schedule.ShowtimeRecord{}, err
	}
	booking := ""
	count := 0
	provider := "default"
	if c.dedicatedGueret() {
		provider = "relay"
	}
	for _, t := range s.Data.Ticketing {
		if t.Provider == provider && t.Type == "DESKTOP" {
			count++
			if len(t.URLs) != 1 {
				return schedule.ShowtimeRecord{}, errShape
			}
			booking = t.URLs[0]
		}
	}
	id := schedule.VeoShowingID(c.ID, s.ID)
	if count != 1 || !schedule.ValidVeoBookingURL(booking, c.ID, id) {
		return schedule.ShowtimeRecord{}, errShape
	}
	if c.dedicatedGueret() && !gueretSourceBinding(booking, start, s.Tags) {
		return schedule.ShowtimeRecord{}, errShape
	}
	room := ""
	if s.Screen != nil {
		room = strings.TrimSpace(s.Screen.Name)
	}
	return schedule.ShowtimeRecord{Provider: schedule.ProviderVeo, ID: "veo-showing-" + id, ProviderShowingID: id, TheaterID: "veo-" + c.ID, ServiceDate: date, Movie: m, StartTime: start, EndTime: start, Language: language, ProviderVersion: version, Format: normalizeFormat(s.Tags), Room: room, BookingURL: booking}, nil
}
func localTime(layout, raw string, loc *time.Location) (time.Time, error) {
	t, err := time.ParseInLocation(layout, raw, loc)
	if err != nil || t.IsZero() || t.Format(layout) != raw {
		return time.Time{}, errShape
	}
	// A wall clock without offset cannot disambiguate a repeated autumn hour.
	for _, delta := range []time.Duration{-time.Hour, time.Hour} {
		if t.Add(delta).In(loc).Format(layout) == raw {
			return time.Time{}, errShape
		}
	}
	return t, nil
}
func parseStart(raw string, loc *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		local := t.In(loc)
		if local.Format(time.RFC3339) == t.Format(time.RFC3339) {
			return local, nil
		}
		return time.Time{}, errShape
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := localTime(layout, raw, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errShape
}
func serviceDate(t time.Time) string {
	if t.Hour() < 3 {
		t = t.AddDate(0, 0, -1)
	}
	return t.Format(time.DateOnly)
}
func normalizeVersion(tags []string) (schedule.Language, string, error) {
	flags := map[string]bool{}
	for _, t := range tags {
		flags[t] = true
	}
	if flags["Localization.Version.Original"] {
		return schedule.LanguageVOSTFR, "VOSTF", nil
	}
	if flags["Localization.Language.French"] {
		if flags["Showtime.Accessibility.OpenCaption"] {
			return schedule.LanguageVFSTF, "VFSTF", nil
		}
		return schedule.LanguageVF, "VF", nil
	}
	return "", "", fmt.Errorf("unsupported Véo session language")
}
func normalizeFormat(tags []string) schedule.Format {
	flags := map[string]bool{}
	for _, tag := range tags {
		flags[strings.ToLower(tag)] = true
	}
	for _, f := range []struct {
		tags   []string
		format schedule.Format
	}{
		{[]string{"auditorium.experience.imax", "video_imax", "imax"}, schedule.FormatIMAX},
		{[]string{"auditorium.experience.4dx", "video_motion", "4dx"}, schedule.Format4DX},
		{[]string{"auditorium.experience.dolbycinema", "auditorium.experience.dolbyatmos", "audio_atmos", "atmos", "dolby"}, schedule.FormatDolby},
		{[]string{"format.projection.3d", "video_3d", "3d"}, schedule.Format3D},
	} {
		for _, tag := range f.tags {
			if flags[tag] {
				return f.format
			}
		}
	}
	return schedule.Format2D
}
