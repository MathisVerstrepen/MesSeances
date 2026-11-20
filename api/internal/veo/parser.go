package veo

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"messeances/api/internal/boxoffice"
	"messeances/api/internal/schedule"
)

var errShape = errors.New("invalid Véo response structure")
var errSnapshotChanged = errors.New("véo advertised index and schedule disagree")

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

type showtimeResponse = boxoffice.Showtime
type scheduleResponse = boxoffice.Schedule

func boxTheater(id string) bool { return id != "EMS1361" && schedule.ValidVeoIdentity("theater", id) }
func boxMovie(id string) bool {
	return !strings.HasPrefix(id, "ticketingcine-") && schedule.ValidVeoIdentity("movie", id)
}
func parseCinemas(b []byte, origin string) ([]cinema, error) {
	var r struct {
		Data struct{ AllTheater struct{ Nodes []cinema } }
	}
	if boxoffice.DecodeJSON(b, &r) != nil || len(r.Data.AllTheater.Nodes) == 0 {
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
	p, err := boxoffice.ParseProgram(b, from, loc)
	switch {
	case errors.Is(err, boxoffice.ErrShape):
		return nil, errShape
	case errors.Is(err, boxoffice.ErrSnapshotChanged):
		return nil, errSnapshotChanged
	default:
		return p, err
	}
}
func parseMovies(b []byte) (map[string]schedule.MovieRecord, error) {
	var items []boxoffice.Movie
	if boxoffice.DecodeJSON(b, &items) != nil || items == nil {
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
	var r boxoffice.Schedule
	if boxoffice.DecodeJSON(b, &r) != nil || len(r) != 1 || r[c.sourceID()].Schedule == nil {
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
					// A repeated native booking can disagree only on optional
					// room metadata. Keep the session identity and leave room
					// unknown rather than selecting an arbitrary published room.
					if prior.BookingURL == row.BookingURL {
						a, b := prior, row
						a.Room, b.Room = "", ""
						if reflect.DeepEqual(a, b) {
							rows[index].Room = ""
							duplicate = true
							continue
						}
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
	if err != nil || boxoffice.ServiceDate(start) != date {
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
		if (t.Provider == provider || c.ID == "P0793" && t.Provider == "relay") && t.Type == "DESKTOP" {
			count++
			if len(t.URLs) != 1 {
				return schedule.ShowtimeRecord{}, errShape
			}
			booking = t.URLs[0]
			if c.ID == "P0793" {
				booking = normalizeSaintAffriqueBooking(booking)
				if t.Provider == "relay" && !saintAffriqueSourceBinding(booking, start, s.Tags) {
					return schedule.ShowtimeRecord{}, errShape
				}
			}
		}
	}
	id := schedule.VeoShowingID(c.ID, s.ID)
	if count != 1 || !schedule.ValidVeoBookingURL(booking, c.ID, id) {
		return schedule.ShowtimeRecord{}, errShape
	}
	if c.ID == "P0793" && strings.HasPrefix(booking, "https://relay.mvtx.us/") && !saintAffriqueSourceBinding(booking, start, s.Tags) {
		return schedule.ShowtimeRecord{}, errShape
	}
	if c.dedicatedGueret() && !gueretSourceBinding(booking, start, s.Tags) {
		return schedule.ShowtimeRecord{}, errShape
	}
	room := ""
	if s.Screen != nil {
		room = strings.TrimSpace(s.Screen.Name)
	}
	return schedule.ShowtimeRecord{Provider: schedule.ProviderVeo, ID: "veo-showing-" + id, ProviderShowingID: id, TheaterID: "veo-" + c.ID, ServiceDate: date, Movie: m, StartTime: start, EndTime: start, Language: language, ProviderVersion: version, Format: boxoffice.NormalizeFormat(s.Tags), Room: room, BookingURL: booking}, nil
}
func decodeJSON(b []byte, out any) error {
	if boxoffice.DecodeJSON(b, out) != nil {
		return errShape
	}
	return nil
}
func localTime(layout, raw string, loc *time.Location) (time.Time, error) {
	t, err := boxoffice.LocalTime(layout, raw, loc)
	if err != nil {
		return time.Time{}, errShape
	}
	return t, nil
}
func parseStart(raw string, loc *time.Location) (time.Time, error) {
	t, err := boxoffice.ParseStart(raw, loc)
	if err != nil {
		return time.Time{}, errShape
	}
	return t, nil
}
func serviceDate(t time.Time) string { return boxoffice.ServiceDate(t) }
func normalizeVersion(tags []string) (schedule.Language, string, error) {
	language, version, err := boxoffice.NormalizeVersion(tags)
	if err != nil {
		return "", "", fmt.Errorf("unsupported Véo session language")
	}
	return language, version, nil
}
func normalizeFormat(tags []string) schedule.Format { return boxoffice.NormalizeFormat(tags) }
