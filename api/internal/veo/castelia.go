package veo

import (
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"messeances/api/internal/schedule"
)

var ticketFilm = regexp.MustCompile(`^[A-Z0-9]{5}$`)
var ticketShowing = regexp.MustCompile(`^emsx1361[0-9]{8}$`)

type ticketSession struct {
	ID        string   `json:"id"`
	Date      string   `json:"date"`
	Room      string   `json:"hall_name"`
	Version   string   `json:"version"`
	Features  []string `json:"features"`
	Formats   string   `json:"formats"`
	FirstPart minutes  `json:"first_part_duration"`
	Booking   string   `json:"booking_url"`
}

func parseCastelia(b []byte, from string, loc *time.Location) (schedule.TheaterRecord, []schedule.ShowtimeRecord, error) {
	var r struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Error   json.RawMessage `json:"error"`
		Result  *struct {
			Schedule *struct {
				Name, Address, City string
				Zip                 string `json:"zip_code"`
				EMS                 struct {
					ID string `json:"id"`
				} `json:"ems"`
				Events []struct {
					ID, Title string
					Duration  minutes
					Poster    string `json:"bill_url"`
					Sessions  []ticketSession
				} `json:"events"`
			} `json:"schedule"`
		} `json:"result"`
	}
	bad := func() (schedule.TheaterRecord, []schedule.ShowtimeRecord, error) {
		return schedule.TheaterRecord{}, nil, errShape
	}
	if decodeJSON(b, &r) != nil || r.JSONRPC != "2.0" || r.ID != 1 || len(r.Error) != 0 || r.Result == nil || r.Result.Schedule == nil {
		return bad()
	}
	p := r.Result.Schedule
	if p.EMS.ID != "1361" || p.Events == nil || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Address) == "" || strings.TrimSpace(p.City) == "" || strings.TrimSpace(p.Zip) == "" {
		return bad()
	}
	t := schedule.TheaterRecord{Provider: schedule.ProviderVeo, ID: "veo-EMS1361", Slug: "veo-EMS1361", ProviderID: "EMS1361", Name: strings.TrimSpace(p.Name), Address: strings.TrimSpace(p.Address), City: strings.TrimSpace(p.City), PostalCode: strings.TrimSpace(p.Zip), AcceptedPasses: []string{}, AvailableDates: []string{}}
	rows := []schedule.ShowtimeRecord{}
	movies := map[string]bool{}
	seen := map[string]schedule.ShowtimeRecord{}
	dates := map[string]bool{}
	for _, e := range p.Events {
		if !ticketFilm.MatchString(e.ID) || movies[e.ID] || e.Sessions == nil || strings.TrimSpace(e.Title) == "" {
			return bad()
		}
		movies[e.ID] = true
		if e.Duration > 0 {
			if _, ok := schedule.RuntimeDuration(int(e.Duration)); !ok {
				return bad()
			}
		}
		poster, ok := schedule.VeoSourcePosterURL(e.Poster, true)
		if !ok {
			return bad()
		}
		if poster != "" && !strings.Contains(poster, "/FR"+e.ID+"/") {
			return bad()
		}
		id := "ticketingcine-" + e.ID
		m := schedule.MovieRecord{Provider: schedule.ProviderVeo, ProviderID: id, Slug: "veo-film-" + id, Title: strings.TrimSpace(e.Title), RuntimeMinutes: int(e.Duration), PosterURL: poster, Genres: []string{}}
		for _, s := range e.Sessions {
			if !ticketShowing.MatchString(s.ID) {
				return bad()
			}
			start, err := localTime("200601021504", s.Date, loc)
			if err != nil {
				return bad()
			}
			date := serviceDate(start)
			if date < from {
				continue
			}
			end, ok := schedule.MegaramaEnd(start, int(e.Duration), int(s.FirstPart))
			if !ok {
				return bad()
			}
			language, version, format, err := ticketAttributes(s)
			if err != nil {
				return bad()
			}
			showing := schedule.VeoShowingID("EMS1361", s.ID)
			booking := s.Booking
			if booking == "" {
				booking = strings.TrimSuffix(CasteliaURL, "/")
			}
			if !schedule.ValidVeoBookingURL(booking, "EMS1361", showing) {
				return bad()
			}
			row := schedule.ShowtimeRecord{Provider: schedule.ProviderVeo, ID: "veo-showing-" + showing, ProviderShowingID: showing, TheaterID: t.ID, ServiceDate: date, Movie: m, StartTime: start, EndTime: end, FirstPartDurationMinutes: int(s.FirstPart), Language: language, ProviderVersion: version, Format: format, Room: strings.TrimSpace(s.Room), BookingURL: booking}
			if prior, ok := seen[row.ID]; ok {
				if !reflect.DeepEqual(prior, row) {
					return bad()
				}
				continue
			}
			seen[row.ID] = row
			rows = append(rows, row)
			dates[date] = true
		}
	}
	for date := range dates {
		t.AvailableDates = append(t.AvailableDates, date)
	}
	sort.Strings(t.AvailableDates)
	return t, rows, nil
}
func ticketAttributes(s ticketSession) (schedule.Language, string, schedule.Format, error) {
	if s.Version != "VF" && s.Version != "VO" && s.Version != "VOST" {
		return "", "", "", errShape
	}
	flags := append([]string(nil), s.Features...)
	for _, f := range strings.Split(s.Formats, ",") {
		flags = append(flags, strings.TrimSpace(f))
	}
	caption := contains(flags, "ST") || contains(flags, "OCAP") || contains(flags, "subtitle")
	// TicketingCine also publishes VOST directly, without a separate subtitle flag.
	language := schedule.Language(s.Version)
	version := s.Version
	if s.Version == "VOST" {
		language = schedule.LanguageVOSTFR
	} else if caption {
		if s.Version == "VF" {
			language = schedule.LanguageVFSTF
			version = "VFSTF"
		} else {
			language = schedule.LanguageVOSTFR
			version = "VOSTF"
		}
	}
	return language, version, normalizeFormat(flags), nil
}
