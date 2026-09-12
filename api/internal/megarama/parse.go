package megarama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
	"messeances/api/internal/schedule"
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
		if seen[c.ID] || !decimal.MatchString(c.ProgramID) || c.Timezone != schedule.Timezone || !schedule.ValidMegaramaBookingURL(c.Website, c.ID, "") {
			return nil, errShape
		}
		seen[c.ID] = true
		c.Name = clean(c.Name)
	}
	return config.Chain.Sites, nil
}

func parseProgram(body []byte, c cinema) (program, error) {
	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Error   json.RawMessage `json:"error"`
		Result  *struct {
			Schedule *program `json:"schedule"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &response) != nil {
		return program{}, fmt.Errorf("%w: program field types", errShape)
	}
	if response.JSONRPC != "2.0" || response.ID != 1 || len(response.Error) != 0 || response.Result == nil || response.Result.Schedule == nil {
		return program{}, fmt.Errorf("%w: program envelope", errShape)
	}
	p := *response.Result.Schedule
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
	if version != "VF" && version != "VO" {
		return "", "", fmt.Errorf("%w: session version", errShape)
	}
	features := make(map[string]bool, len(s.Features)+3)
	for _, feature := range s.Features {
		features[feature] = true
	}
	for _, token := range strings.Split(s.Formats, ",") {
		token = strings.TrimSpace(token)
		switch token {
		case "", "ST", "OCAP", "3D", "4K", "7.1", "ATMOS", "HFR":
		default:
			return "", "", fmt.Errorf("%w: session format", errShape)
		}
		features[token] = true
	}
	language := schedule.Language(version)
	if features["ST"] || features["subtitle"] {
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
	if !dateID.MatchString(value) {
		return time.Time{}, errShape
	}
	start, err := time.ParseInLocation("200601021504", value, location)
	if err != nil || start.Format("200601021504") != value || start.Year() < 1 {
		return time.Time{}, errShape
	}
	// Paris folds differ by one hour. Never choose an arbitrary repeated instant.
	for _, shift := range []time.Duration{-time.Hour, time.Hour} {
		if start.Add(shift).In(location).Format("200601021504") == value {
			return time.Time{}, errShape
		}
	}
	return start, nil
}

func posterURL(raw, id string) string {
	if !schedule.ValidMegaramaPosterURL(raw) {
		return ""
	}
	u, _ := url.Parse(raw)
	if globalID.MatchString(id) {
		if !strings.Contains(u.Path, "/FR"+id+"/") {
			return ""
		}
		return strings.Replace(raw, "/movie_poster/120/", "/movie_poster/600/", 1)
	}
	if localID.MatchString(id) && u.Path == "/ems_spectacle/120/"+id[4:8]+"/"+id[8:]+".jpg" {
		return raw
	}
	return ""
}

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

func clean(value string) string { return strings.Join(strings.Fields(value), " ") }
func fallback(value, alternative string) string {
	if clean(value) != "" {
		return clean(value)
	}
	return clean(alternative)
}
