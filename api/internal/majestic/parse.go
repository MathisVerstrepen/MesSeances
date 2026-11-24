package majestic

import (
	"fmt"
	"strings"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/ticketingcine"
)

func parseProgram(body []byte, site string) (program, error) {
	p, err := ticketingcine.DecodeProgram(body)
	if err != nil {
		return program{}, errShape
	}
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
	if version != "VF" && version != "VO" && version != "VOST" {
		return "", "", "", fmt.Errorf("%w: version", errShape)
	}
	features := map[string]bool{}
	for _, f := range s.Features {
		features[f] = true
	}
	for _, f := range strings.Split(s.Formats, ",") {
		features[strings.TrimSpace(f)] = true
	}
	// TicketingCine also publishes VOST directly, without a separate subtitle flag.
	language := schedule.Language(version)
	if version == "VOST" {
		language = schedule.LanguageVOSTFR
	} else if features["subtitle"] || features["ST"] {
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
	start, ok := ticketingcine.ParseStart(value, location)
	if !ok {
		return time.Time{}, errShape
	}
	return start, nil
}
func posterURL(raw, id string) string {
	if !schedule.ValidMajesticPosterURL(raw) {
		return ""
	}
	return ticketingcine.PosterURL(raw, id)
}
func clean(value string) string { return ticketingcine.Clean(value) }
