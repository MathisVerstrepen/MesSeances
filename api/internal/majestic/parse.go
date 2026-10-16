package majestic

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"messeances/api/internal/schedule"
)

func parseProgram(body []byte, site string) (program, error) {
	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Error   json.RawMessage `json:"error"`
		Result  *struct {
			Schedule *program `json:"schedule"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &response) != nil || response.JSONRPC != "2.0" || response.ID != 1 || len(response.Error) != 0 || response.Result == nil || response.Result.Schedule == nil {
		return program{}, errShape
	}
	p := *response.Result.Schedule
	if p.Events == nil || p.EMS.ID != "" && p.EMS.ID != site[3:] || clean(p.Name) == "" || clean(p.Address) == "" || clean(p.City) == "" || clean(p.ZipCode) == "" {
		return program{}, errShape
	}
	seen := map[string]bool{}
	for _, e := range p.Events {
		id := movieID(site, e.ID)
		if !schedule.ValidMajesticIdentity("movie", id) || seen[id] || clean(e.Title) == "" || e.Sessions == nil || e.BillURL != "" && posterURL(e.BillURL, e.ID) == "" {
			return program{}, fmt.Errorf("%w: event metadata", errShape)
		}
		seen[id] = true
		for _, s := range e.Sessions {
			if !schedule.ValidMajesticIdentity("showing", s.ID) || !strings.HasPrefix(s.ID, "emsx"+site[3:]) || clean(s.HallName) == "" || !dateID.MatchString(s.Date) {
				return program{}, fmt.Errorf("%w: session metadata", errShape)
			}
			if _, _, _, err := attributes(s); err != nil {
				return program{}, err
			}
			if s.BookingURL != "" && !schedule.MajesticBookingMatchesMovie(s.BookingURL, site, s.ID, id) {
				return program{}, fmt.Errorf("%w: session booking", errShape)
			}
		}
	}
	return p, nil
}
func movieID(site, id string) string {
	if globalID.MatchString(id) {
		return id
	}
	if localID.MatchString(id) && strings.HasPrefix(id, "emsx"+site[3:]) {
		return site + "-" + id
	}
	return ""
}
func attributes(s session) (schedule.Language, schedule.Format, string, error) {
	version := s.Version
	if version != "VF" && version != "VO" {
		return "", "", "", fmt.Errorf("%w: version", errShape)
	}
	features := map[string]bool{}
	for _, f := range s.Features {
		features[f] = true
	}
	for _, f := range strings.Split(s.Formats, ",") {
		features[strings.TrimSpace(f)] = true
	}
	language := schedule.Language(version)
	if features["subtitle"] || features["ST"] {
		if version == "VF" {
			language = schedule.LanguageVFSTF
			version = "VFSTF"
		} else {
			language = schedule.LanguageVOSTFR
			version = "VOSTF"
		}
	}
	format := schedule.Format2D
	switch {
	case features["video_imax"]:
		format = schedule.FormatIMAX
	case features["video_3d"] || features["3D"]:
		format = schedule.Format3D
	case features["video_immersif"] || strings.Contains(strings.ToUpper(s.HallName), "ICE"):
		format = schedule.FormatICE
	case features["audio_atmos"]:
		format = schedule.FormatDolby
	}
	return language, format, version, nil
}
func parseStart(value string, location *time.Location) (time.Time, error) {
	if !dateID.MatchString(value) {
		return time.Time{}, errShape
	}
	start, err := time.ParseInLocation("200601021504", value, location)
	if err != nil || start.Format("200601021504") != value || start.Year() < 1 {
		return time.Time{}, errShape
	}
	for _, shift := range []time.Duration{-time.Hour, time.Hour} {
		if start.Add(shift).In(location).Format("200601021504") == value {
			return time.Time{}, errShape
		}
	}
	return start, nil
}
func posterURL(raw, id string) string {
	if !schedule.ValidMajesticPosterURL(raw) {
		return ""
	}
	u, _ := url.Parse(raw)
	if globalID.MatchString(id) && strings.Contains(u.Path, "/FR"+id+"/") {
		return strings.Replace(raw, "/movie_poster/120/", "/movie_poster/600/", 1)
	}
	if localID.MatchString(id) && u.Path == "/ems_spectacle/120/"+id[4:8]+"/"+id[8:]+".jpg" {
		return raw
	}
	return ""
}
func clean(value string) string { return strings.Join(strings.Fields(value), " ") }
