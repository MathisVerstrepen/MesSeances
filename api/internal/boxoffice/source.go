// Package boxoffice handles shared Gatsby Boxoffice programme data.
package boxoffice

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"messeances/api/internal/schedule"
)

const CatalogPath = "/page-data/sq/d/2506275789.json"
const MaxResponseBytes = 16 << 20

var ErrShape = errors.New("invalid Boxoffice response structure")
var ErrSnapshotChanged = errors.New("advertised Boxoffice index and schedule disagree")

type SourceID string

func (id *SourceID) UnmarshalJSON(b []byte) error {
	var s string
	if len(b) > 0 && b[0] == '"' {
		if json.Unmarshal(b, &s) != nil {
			return ErrShape
		}
	} else {
		s = string(b)
		for _, c := range s {
			if c < '0' || c > '9' {
				return ErrShape
			}
		}
	}
	if s == "" || len(s) > 2048 || strings.ContainsRune(s, 0) {
		return ErrShape
	}
	*id = SourceID(s)
	return nil
}

type programResponse struct {
	MovieIDs      *struct{ TitleAsc, ReleaseAsc []SourceID } `json:"movieIds"`
	ScheduledDays map[string][]string                        `json:"scheduledDays"`
}
type Movie struct {
	ID      SourceID `json:"id"`
	Title   string   `json:"title"`
	Runtime *int     `json:"runtime"`
	Poster  string   `json:"poster"`
}
type Showtime struct {
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
type Schedule map[string]struct {
	Schedule map[string]map[string][]Showtime `json:"schedule"`
}

func DecodeJSON(b []byte, out any) error {
	if len(b) == 0 || len(b) > MaxResponseBytes {
		return ErrShape
	}
	d := json.NewDecoder(bytes.NewReader(b))
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return ErrShape
	}
	return nil
}
func ParseProgram(b []byte, from string, loc *time.Location) (map[string][]string, error) {
	var r programResponse
	if DecodeJSON(b, &r) != nil || r.MovieIDs == nil || r.ScheduledDays == nil {
		return nil, ErrShape
	}
	for _, index := range [][]SourceID{r.MovieIDs.TitleAsc, r.MovieIDs.ReleaseAsc} {
		if index == nil {
			return nil, ErrShape
		}
		seen := map[string]bool{}
		for _, raw := range index {
			id := string(raw)
			if !schedule.ValidGrandEcranIdentity("movie", id) || seen[id] {
				return nil, ErrShape
			}
			seen[id] = true
		}
		if len(seen) != len(r.ScheduledDays) {
			return nil, ErrSnapshotChanged
		}
		for id := range r.ScheduledDays {
			if !seen[id] {
				return nil, ErrSnapshotChanged
			}
		}
	}
	result := map[string][]string{}
	for id, dates := range r.ScheduledDays {
		if !schedule.ValidGrandEcranIdentity("movie", id) || dates == nil {
			return nil, ErrShape
		}
		seen := map[string]bool{}
		for _, raw := range dates {
			date := raw
			if len(raw) > 10 {
				if _, err := time.Parse(time.RFC3339, raw); err != nil {
					return nil, ErrShape
				}
				date = raw[:10]
			}
			parsed, err := time.ParseInLocation(time.DateOnly, date, loc)
			if err != nil || parsed.Format(time.DateOnly) != date {
				return nil, ErrShape
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
func LocalTime(layout, raw string, loc *time.Location) (time.Time, error) {
	t, err := time.ParseInLocation(layout, raw, loc)
	if err != nil || t.IsZero() || t.Format(layout) != raw {
		return time.Time{}, ErrShape
	}
	// A wall clock without offset cannot disambiguate a repeated autumn hour.
	for _, delta := range []time.Duration{-time.Hour, time.Hour} {
		if t.Add(delta).In(loc).Format(layout) == raw {
			return time.Time{}, ErrShape
		}
	}
	return t, nil
}
func ParseStart(raw string, loc *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		local := t.In(loc)
		if local.Format(time.RFC3339) == t.Format(time.RFC3339) {
			return local, nil
		}
		return time.Time{}, ErrShape
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := LocalTime(layout, raw, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, ErrShape
}
func ServiceDate(t time.Time) string {
	if t.Hour() < 3 {
		t = t.AddDate(0, 0, -1)
	}
	return t.Format(time.DateOnly)
}
func NormalizeVersion(tags []string) (schedule.Language, string, error) {
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
	return "", "", fmt.Errorf("unsupported Boxoffice session language")
}
func NormalizeFormat(tags []string) schedule.Format {
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
func ProgramURL(origin, id string) string {
	return origin + "/api/gatsby-source-boxofficeapi/scheduledMovies?" + url.Values{"theaterId": {id}}.Encode()
}
func MoviesURL(origin string, ids []string) string {
	return origin + "/api/gatsby-source-boxofficeapi/movies?" + url.Values{"ids": ids}.Encode()
}
func ScheduleURL(origin, id, from, through string) string {
	t, _ := json.Marshal(struct {
		ID       string `json:"id"`
		TimeZone string `json:"timeZone"`
	}{id, schedule.Timezone})
	end, _ := time.Parse(time.DateOnly, through)
	return origin + "/api/gatsby-source-boxofficeapi/schedule?" + url.Values{"theaters": {string(t)}, "includeAllMovies": {"true"}, "from": {from + "T03:00:00"}, "to": {end.AddDate(0, 0, 1).Format(time.DateOnly) + "T03:00:00"}}.Encode()
}
