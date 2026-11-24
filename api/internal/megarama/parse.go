package megarama

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/net/html"
	"messeances/api/internal/schedule"
	"messeances/api/internal/ticketingcine"
)

func parseConfig(body []byte) ([]cinema, error) {
	match := configAssignment.FindIndex(body)
	if match == nil {
		return nil, errShape
	}
	var config struct {
		SiteID string `json:"site_id"`
		Chain  struct {
			Sites []cinema `json:"sites"`
		} `json:"chain"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body[match[1]:]))
	if decoder.Decode(&config) != nil || config.SiteID != "CHN0042" || len(config.Chain.Sites) == 0 {
		return nil, errShape
	}
	seen := map[string]bool{}
	for i := range config.Chain.Sites {
		c := &config.Chain.Sites[i]
		if seen[c.ID] || !cinemaID.MatchString(c.ID) || !decimal.MatchString(c.ProgramID) || c.Timezone != schedule.Timezone || c.Website != "" && !schedule.ValidMegaramaBookingURL(c.Website, c.ID, "") {
			return nil, errShape
		}
		// A populated portal URL without a root is not absent website data.
		if c.Website == "" && c.FullWebsite != "" {
			return nil, errShape
		}
		seen[c.ID] = true
		c.Name = clean(c.Name)
	}
	return config.Chain.Sites, nil
}

// parseSiteWebsite recovers only the root from the matching authoritative site.
// website_full_url is not a Referer source: it may be a query-based portal URL.
func parseSiteWebsite(body []byte, c cinema) (string, error) {
	match := configAssignment.FindIndex(body)
	if match == nil {
		return "", errShape
	}
	var config struct {
		Site cinema `json:"site"`
	}
	if json.NewDecoder(bytes.NewReader(body[match[1]:])).Decode(&config) != nil || config.Site.ID != c.ID || config.Site.ProgramID != c.ProgramID {
		return "", errShape
	}
	website := config.Site.Website
	if !strings.Contains(website, "://") {
		website = "https://" + website
	}
	if !schedule.ValidMegaramaBookingURL(website, c.ID, "") {
		return "", errShape
	}
	return strings.TrimSuffix(website, "/") + "/", nil
}

func parseProgram(body []byte, c cinema) (program, error) {
	p, err := ticketingcine.DecodeProgram(body)
	if errors.Is(err, ticketingcine.ErrFieldTypes) {
		return program{}, fmt.Errorf("%w: program field types", errShape)
	}
	if err != nil {
		return program{}, fmt.Errorf("%w: program envelope", errShape)
	}
	if p.Events == nil || p.ID != c.ProgramID || p.EMS.ID != "" && p.EMS.ID != strings.TrimPrefix(c.ID, "EMS") {
		return program{}, fmt.Errorf("%w: program identity or events", errShape)
	}
	seen := map[string]bool{}
	for i := range p.Events {
		e := &p.Events[i]
		validID := globalID.MatchString(e.ID) || localID.MatchString(e.ID) && strings.HasPrefix(e.ID, "emsx"+c.ID[3:])
		if !validID || len(e.ID)+len(c.ID)+1 > 114 || seen[e.ID] || clean(e.Title) == "" || e.Sessions == nil {
			return program{}, fmt.Errorf("%w: event identity or metadata", errShape)
		}
		seen[e.ID] = true
		e.Title = clean(e.Title)
		for _, s := range e.Sessions {
			if !sessionID.MatchString(s.ID) || !strings.HasPrefix(s.ID, "emsx"+c.ID[3:]) || clean(s.HallName) == "" || !dateID.MatchString(s.Date) {
				return program{}, fmt.Errorf("%w: session identity, room or date", errShape)
			}
			if _, _, err := attributes(s); err != nil {
				return program{}, err
			}
		}
	}
	return p, nil
}

func attributes(s session) (schedule.Language, schedule.Format, error) {
	version := clean(s.Version)
	if version != "VF" && version != "VO" && version != "VOF" && version != "VOST" {
		return "", "", fmt.Errorf("%w: session version", errShape)
	}
	features := make(map[string]bool, len(s.Features)+3)
	for _, feature := range s.Features {
		features[feature] = true
	}
	for _, token := range strings.Split(s.Formats, ",") {
		token = strings.TrimSpace(token)
		switch token {
		case "", "ST", "OCAP", "3D", "4K", "7.1", "ATMOS", "AURO3D", "HFR", "LAZER", "VI":
		default:
			return "", "", fmt.Errorf("%w: session format", errShape)
		}
		features[token] = true
	}
	// TicketingCine also publishes VOST directly, without a separate subtitle flag.
	language := schedule.Language(version)
	if version == "VOF" {
		language = schedule.LanguageVO
	}
	if version == "VOST" || features["ST"] || features["subtitle"] {
		if version == "VF" {
			language = schedule.LanguageVFSTF
		} else {
			language = schedule.LanguageVOSTFR
		}
	}
	format := schedule.Format2D
	switch {
	case features["video_imax"]:
		format = schedule.FormatIMAX
	case features["video_motion"]:
		format = schedule.Format4DX
	case features["video_3d"] || features["3D"]:
		format = schedule.Format3D
	}
	return language, format, nil
}

func parseStart(value string, location *time.Location) (time.Time, error) {
	start, ok := ticketingcine.ParseStart(value, location)
	if !ok {
		return time.Time{}, errShape
	}
	return start, nil
}

func posterURL(raw, id string) string { return ticketingcine.PosterURL(raw, id) }

func parsePoster(body []byte, id string) string {
	tokens := html.NewTokenizer(bytes.NewReader(body))
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			return ""
		case html.StartTagToken, html.SelfClosingTagToken:
			t := tokens.Token()
			if t.Data != "meta" {
				continue
			}
			property, content := "", ""
			for _, a := range t.Attr {
				if a.Key == "property" {
					property = a.Val
				}
				if a.Key == "content" {
					content = a.Val
				}
			}
			if property == "og:image" {
				return posterURL(content, id)
			}
		}
	}
}

func clean(value string) string { return ticketingcine.Clean(value) }
func fallback(value, alternative string) string {
	if clean(value) != "" {
		return clean(value)
	}
	return clean(alternative)
}
