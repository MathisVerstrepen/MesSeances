package cinewest

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"messeances/api/internal/schedule"
)

var globalFilm = regexp.MustCompile(`^[A-Z0-9]{5}$`)
var ticketShowing = regexp.MustCompile(`^emsx[0-9]{12}$`)

func loadTicketProgram(ctx context.Context, f Fetcher, site string, location *time.Location) (cinemaProgram, error) {
	body, err := f.Program(ctx, site)
	if err != nil {
		return cinemaProgram{}, err
	}
	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Error   json.RawMessage `json:"error"`
		Result  *struct {
			Schedule *ticketProgram `json:"schedule"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &response) != nil || response.JSONRPC != "2.0" || response.ID != 1 || len(response.Error) != 0 || response.Result == nil || response.Result.Schedule == nil {
		return cinemaProgram{}, errShape
	}
	p := response.Result.Schedule
	programIDs := map[string]string{"EMS1185": "5053122", "EMS1317": "7075033", "EMS0042": "4411540"}
	if p.ID != programIDs[site] || p.EMS.ID != strings.TrimPrefix(site, "EMS") || p.Events == nil {
		return cinemaProgram{}, errShape
	}
	id := "ticketingcine-" + site
	result := cinemaProgram{theater: theater(id, p.Name, p.Address, p.City, p.Zip)}
	events := map[string]bool{}
	for _, e := range p.Events {
		movieID := "ticketingcine-" + e.ID
		if !globalFilm.MatchString(e.ID) {
			movieID = "ticketingcine-" + site + "-" + e.ID
		}
		if events[movieID] || e.Sessions == nil {
			return cinemaProgram{}, errShape
		}
		events[movieID] = true
		poster := e.Poster
		if !globalFilm.MatchString(e.ID) || !strings.Contains(poster, "/FR"+e.ID+"/") {
			poster = ""
		}
		poster = strings.Replace(poster, "/movie_poster/120/", "/movie_poster/600/", 1)
		m, err := movie(movieID, e.Title, poster, int(e.Duration))
		if err != nil {
			return cinemaProgram{}, err
		}
		for _, s := range e.Sessions {
			if !ticketShowing.MatchString(s.ID) || !strings.HasPrefix(s.ID, "emsx"+p.EMS.ID) {
				return cinemaProgram{}, errShape
			}
			start, err := localTime("200601021504", s.Date, location)
			if err != nil {
				return cinemaProgram{}, err
			}
			end, ok := schedule.MegaramaEnd(start, m.RuntimeMinutes, int(s.FirstPart))
			if !ok {
				return cinemaProgram{}, errShape
			}
			language, format, err := ticketAttributes(s)
			if err != nil {
				return cinemaProgram{}, err
			}
			row, err := showing(id, s.ID, m, start, end, language, string(language), format, s.Room, s.Booking, int(s.FirstPart))
			if err != nil {
				return cinemaProgram{}, err
			}
			result.shows = append(result.shows, row)
		}
	}
	return result, nil
}
func ticketAttributes(s ticketSession) (schedule.Language, schedule.Format, error) {
	if s.Version != "VF" && s.Version != "VO" {
		return "", "", errShape
	}
	flags := map[string]bool{}
	for _, f := range s.Features {
		flags[f] = true
	}
	for _, f := range strings.Split(s.Formats, ",") {
		f = strings.TrimSpace(f)
		switch f {
		case "", "ST", "OCAP", "3D", "4K", "7.1", "ATMOS", "HFR":
		default:
			return "", "", errShape
		}
		flags[f] = true
	}
	language := schedule.Language(s.Version)
	if flags["ST"] || flags["subtitle"] {
		if language == schedule.LanguageVF {
			language = schedule.LanguageVFSTF
		} else {
			language = schedule.LanguageVOSTFR
		}
	}
	format := schedule.Format2D
	switch {
	case flags["video_imax"]:
		format = schedule.FormatIMAX
	case flags["video_motion"]:
		format = schedule.Format4DX
	case flags["video_3d"] || flags["3D"]:
		format = schedule.Format3D
	}
	return language, format, nil
}
func localTime(layout, raw string, location *time.Location) (time.Time, error) {
	start, err := time.ParseInLocation(layout, raw, location)
	if err != nil || start.Format(layout) != raw || start.IsZero() {
		return time.Time{}, errShape
	}
	for _, shift := range []time.Duration{-time.Hour, time.Hour} {
		if start.Add(shift).In(location).Format(layout) == raw {
			return time.Time{}, errShape
		}
	}
	return start, nil
}
