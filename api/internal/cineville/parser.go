package cineville

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"messeances/api/internal/schedule"
)

var errPayload = errors.New("invalid Cineville payload")

// scalar accepts exact JSON integers or strings, never floating-point conversion.
type scalar string

func (s *scalar) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		return errPayload
	}
	if b[0] == '"' {
		var text string
		if err := json.Unmarshal(b, &text); err != nil {
			return errPayload
		}
		*s = scalar(text)
		return nil
	}
	*s = scalar(string(b))
	return nil
}

type cinema struct {
	ID      scalar `json:"id"`
	Route   string `json:"cine"`
	Name    string `json:"nom_cine_public"`
	Address string `json:"adresse"`
	City    string `json:"adresse_ville"`
	Postal  string `json:"code_postal_1"`
}
type film struct {
	Visa     scalar          `json:"visa"`
	Title    string          `json:"titre_cotecine"`
	Metadata json.RawMessage `json:"movie_data"`
	Dates    []programDate   `json:"dates"`
}
type programDate struct {
	Date      scalar    `json:"date"`
	Showtimes []session `json:"showtimes"`
}
type session struct {
	Cinema     scalar `json:"id_cinema"`
	ID         scalar `json:"id_seance"`
	Bordereau  scalar `json:"id_bordereau"`
	Room       scalar `json:"salle"`
	Time       string `json:"heure"`
	Version    string `json:"version"`
	Subtitles  scalar `json:"vfstrfr"`
	Relief     string `json:"relief"`
	Attributes string `json:"attributs"`
}
type pageProps struct {
	Cinemas    []cinema          `json:"cines"`
	CinemaID   scalar            `json:"cinemaId"`
	Program    []film            `json:"prog"`
	Events     []film            `json:"progWithEvents"`
	Attributes []json.RawMessage `json:"attributs"`
}

func parseBootstrap(body []byte) (string, []cinema, error) {
	if len(body) > MaxBodySize {
		return "", nil, errPayload
	}
	z := html.NewTokenizer(bytes.NewReader(body))
	var raw []byte
	for {
		token := z.Next()
		if token == html.ErrorToken {
			if !errors.Is(z.Err(), io.EOF) {
				return "", nil, errPayload
			}
			break
		}
		if token != html.StartTagToken {
			continue
		}
		t := z.Token()
		if t.Data != "script" {
			continue
		}
		for _, a := range t.Attr {
			if a.Key == "id" && a.Val == "__NEXT_DATA__" {
				if raw != nil || z.Next() != html.TextToken {
					return "", nil, errPayload
				}
				raw = bytes.Clone(z.Text())
			}
		}
	}
	var bootstrap struct {
		Build string `json:"buildId"`
		Props struct {
			Page struct {
				Cinemas []cinema `json:"cinemas"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if json.Unmarshal(raw, &bootstrap) != nil || !pathSegment.MatchString(bootstrap.Build) {
		return "", nil, errPayload
	}
	catalog, err := validateCatalog(bootstrap.Props.Page.Cinemas)
	return bootstrap.Build, catalog, err
}
func validateCatalog(rows []cinema) ([]cinema, error) {
	result := make([]cinema, 0, len(rows))
	ids, routes := map[scalar]bool{}, map[string]bool{}
	for _, c := range rows {
		if c.ID == "4676" || c.Route == "siege" {
			continue
		}
		if !schedule.ValidCinevilleIdentity("theater", string(c.ID)) || !pathSegment.MatchString(c.Route) || ids[c.ID] || routes[c.Route] || strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.City) == "" || strings.TrimSpace(c.Postal) == "" {
			return nil, errPayload
		}
		ids[c.ID], routes[c.Route] = true, true
		result = append(result, c)
	}
	if len(result) == 0 {
		return nil, errPayload
	}
	return result, nil
}
func parsePage(body []byte, expected cinema) (pageProps, error) {
	var page struct {
		Props pageProps `json:"pageProps"`
	}
	if len(body) > MaxBodySize || json.Unmarshal(body, &page) != nil {
		return pageProps{}, errPayload
	}
	p := page.Props
	if p.CinemaID != expected.ID || p.Program == nil || p.Events == nil || p.Attributes == nil {
		return pageProps{}, errPayload
	}
	catalog, err := validateCatalog(p.Cinemas)
	if err != nil {
		return pageProps{}, errPayload
	}
	for _, c := range catalog {
		if c.ID == expected.ID {
			if c != expected {
				return pageProps{}, errPayload
			}
			return p, nil
		}
	}
	return pageProps{}, errPayload
}

var hourRuntime = regexp.MustCompile(`^([0-9]+)h(?:([0-9]{1,2})(?:min)?)?$`)
var colonRuntime = regexp.MustCompile(`^([0-9]+):([0-9]{2})$`)
var minuteRuntime = regexp.MustCompile(`^([0-9]+)(?:mn|min)$`)

func parseRuntime(raw string) (int, error) {
	v := strings.ToLower(strings.Join(strings.Fields(raw), ""))
	if v == "" {
		return 0, nil
	}
	parts := hourRuntime.FindStringSubmatch(v)
	if parts == nil {
		parts = colonRuntime.FindStringSubmatch(v)
	}
	var hours, minutes int64
	var err error
	if parts != nil {
		hours, err = strconv.ParseInt(parts[1], 10, 32)
		if err != nil {
			return 0, errPayload
		}
		if parts[2] != "" {
			minutes, err = strconv.ParseInt(parts[2], 10, 32)
		}
		if err != nil || minutes >= 60 {
			return 0, errPayload
		}
	} else {
		parts = minuteRuntime.FindStringSubmatch(v)
		if parts == nil {
			return 0, errPayload
		}
		minutes, err = strconv.ParseInt(parts[1], 10, 32)
		if err != nil {
			return 0, errPayload
		}
	}
	total := hours*60 + minutes
	if total <= 0 || total > 2147483647 {
		return 0, errPayload
	}
	if _, ok := schedule.RuntimeDuration(int(total)); !ok {
		return 0, errPayload
	}
	return int(total), nil
}
func parseMovie(f film) (schedule.MovieRecord, error) {
	id := string(f.Visa)
	m := schedule.MovieRecord{Provider: schedule.ProviderCineville, ProviderID: id, Slug: "cineville-film-" + id, Title: strings.TrimSpace(f.Title)}
	if !schedule.ValidCinevilleIdentity("movie", id) {
		return m, errPayload
	}
	raw := bytes.TrimSpace(f.Metadata)
	if len(raw) == 0 {
		return m, errPayload
	}
	if raw[0] == '{' {
		var missing struct {
			Result *bool `json:"result"`
		}
		if json.Unmarshal(raw, &missing) != nil || missing.Result == nil || *missing.Result {
			return m, errPayload
		}
	} else {
		var entries []struct {
			Title    string  `json:"titre"`
			Runtime  string  `json:"duree"`
			Poster   string  `json:"affichette"`
			Overview string  `json:"synopsis"`
			Genre    string  `json:"genreprincipal"`
			Release  *string `json:"datedesortie"`
		}
		if raw[0] != '[' || json.Unmarshal(raw, &entries) != nil {
			return m, errPayload
		}
		if len(entries) > 0 {
			v := entries[0]
			if m.Title == "" {
				m.Title = strings.TrimSpace(v.Title)
			}
			var err error
			m.RuntimeMinutes, err = parseRuntime(v.Runtime)
			if err != nil {
				return m, err
			}
			if schedule.ValidCinevillePosterURL(schedule.CinevillePosterPrefix + v.Poster) {
				m.PosterURL = schedule.CinevillePosterPrefix + v.Poster
			}
			m.Overview = strings.TrimSpace(v.Overview)
			if g := strings.TrimSpace(v.Genre); g != "" {
				m.Genres = []string{g}
			}
			if v.Release != nil && *v.Release != "" {
				for _, layout := range []string{time.DateOnly, "20060102", "02/01/2006"} {
					if d, err := time.Parse(layout, *v.Release); err == nil && d.Format(layout) == *v.Release {
						m.ReleaseDate = d.Format(time.DateOnly)
						break
					}
				}
			}
		}
	}
	if m.Title == "" {
		return m, errPayload
	}
	return m, nil
}
func mergeMovie(first, next schedule.MovieRecord) schedule.MovieRecord {
	if first.ProviderID == "" {
		return next
	}
	if first.RuntimeMinutes == 0 {
		first.RuntimeMinutes = next.RuntimeMinutes
	}
	if first.PosterURL == "" {
		first.PosterURL = next.PosterURL
	}
	if first.Overview == "" {
		first.Overview = next.Overview
	}
	if first.ReleaseDate == "" {
		first.ReleaseDate = next.ReleaseDate
	}
	if len(first.Genres) == 0 {
		first.Genres = next.Genres
	}
	return first
}

func parseStart(date, clock string, location *time.Location) (time.Time, error) {
	wall := date + " " + clock
	t, err := time.ParseInLocation("20060102 15:04", wall, location)
	if err != nil || t.Format("20060102 15:04") != wall {
		return time.Time{}, errPayload
	}
	// Paris transitions are one hour. Both possible instants indicate an ambiguous wall time.
	if t.Add(-time.Hour).Format("20060102 15:04") == wall || t.Add(time.Hour).Format("20060102 15:04") == wall {
		return time.Time{}, errPayload
	}
	return t, nil
}
func parseAttributes(s session) (schedule.Language, schedule.Format, error) {
	var language schedule.Language
	if s.Subtitles != "0" && s.Subtitles != "1" && s.Subtitles != "false" && s.Subtitles != "true" {
		return "", "", errPayload
	}
	switch s.Version {
	case "VF":
		language = schedule.LanguageVF
		if s.Subtitles == "1" || s.Subtitles == "true" {
			language = schedule.LanguageVFSTF
		}
	case "VO":
		language = schedule.LanguageVOSTFR
	default:
		return "", "", errPayload
	}
	if s.Relief != "2D" && s.Relief != "3D" {
		return "", "", errPayload
	}
	attrs := map[string]bool{}
	for _, value := range strings.FieldsFunc(s.Attributes, func(r rune) bool {
		return r == ',' || r == ';' || r == '|' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		if !schedule.ValidCinevilleIdentity("theater", value) {
			return "", "", errPayload
		}
		attrs[value] = true
	}
	format := schedule.Format(s.Relief)
	if attrs["32"] {
		format = schedule.Format3D
	}
	if attrs["25"] {
		format = schedule.FormatDolby
	}
	if attrs["21"] {
		format = schedule.FormatIMAX
	}
	if attrs["10008"] {
		format = schedule.FormatInfinityVision
	}
	return language, format, nil
}
