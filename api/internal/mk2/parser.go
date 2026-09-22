package mk2

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"time"

	"messeances/api/internal/schedule"
)

type cinema struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Address     string `json:"address2"`
	City        string `json:"city"`
	ComplexSlug string `json:"complexSlug"`
}
type film struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Runtime     *int   `json:"runTime"`
	Poster      string `json:"graphicUrl"`
	Synopsis    string `json:"synopsis"`
	OpeningDate string `json:"openingDate"`
	Genres      []struct {
		Name string `json:"name"`
	} `json:"genres"`
}
type attribute struct {
	ShortName string `json:"shortName"`
}
type session struct {
	ID              string      `json:"id"`
	CinemaID        string      `json:"cinemaId"`
	SessionID       string      `json:"sessionId"`
	FilmID          string      `json:"filmId"`
	ScheduledFilmID string      `json:"scheduledFilmId"`
	ShowTime        string      `json:"showTime"`
	Attributes      []attribute `json:"attributes"`
}
type group struct {
	Cinema   cinema    `json:"cinema"`
	Film     film      `json:"film"`
	Sessions []session `json:"sessions"`
}
type sessionType struct {
	Groups []group `json:"sessionsByFilmAndCinema"`
}
type complex struct {
	Slug    string        `json:"slug"`
	Zipcode string        `json:"zipcode"`
	Cinemas []cinema      `json:"cinemas"`
	Types   []sessionType `json:"sessionsByType"`
}

func parseCatalog[T any](body []byte, op Operation) ([]T, error) {
	var envelope struct {
		Data []T `json:"data"`
	}
	if len(body) > MaxBodySize || json.Unmarshal(body, &envelope) != nil || envelope.Data == nil {
		return nil, payloadError(op)
	}
	return envelope.Data, nil
}
func parseComplex(body []byte, slug string) (complex, error) {
	var c complex
	if len(body) > MaxBodySize || json.Unmarshal(body, &c) != nil || c.Slug != slug || strings.TrimSpace(c.Zipcode) == "" || c.Cinemas == nil || c.Types == nil {
		return complex{}, payloadError(OperationComplex)
	}
	for _, typ := range c.Types {
		if typ.Groups == nil {
			return complex{}, payloadError(OperationComplex)
		}
		for _, g := range typ.Groups {
			if g.Sessions == nil {
				return complex{}, payloadError(OperationComplex)
			}
		}
	}
	return c, nil
}
func normalizeCinema(c cinema) (cinema, error) {
	c.Name, c.Address, c.City = strings.TrimSpace(c.Name), strings.TrimSpace(c.Address), strings.TrimSpace(c.City)
	if strings.EqualFold(c.City, "paris") {
		c.City = "Paris"
	}
	if !schedule.ValidMK2Identity("theater", c.ID) || !validComplexSlug(c.ComplexSlug) || c.Name == "" || c.Address == "" || c.City == "" {
		return cinema{}, payloadError(OperationCinemas)
	}
	if !strings.EqualFold(c.Name, "MK2") && !strings.HasPrefix(strings.ToUpper(c.Name), "MK2 ") {
		c.Name = "MK2 " + c.Name
	}
	return c, nil
}
func parseMovie(f film) (schedule.MovieRecord, error) {
	m := schedule.MovieRecord{Provider: schedule.ProviderMK2, ProviderID: f.ID, Slug: "mk2-film-" + f.ID, Title: strings.TrimSpace(f.Title), Overview: strings.TrimSpace(f.Synopsis)}
	if !schedule.ValidMK2Identity("movie", f.ID) {
		return m, payloadError(OperationFilms)
	}
	if f.Runtime != nil {
		m.RuntimeMinutes = *f.Runtime
		if _, ok := schedule.RuntimeDuration(m.RuntimeMinutes); !ok && m.RuntimeMinutes != 0 {
			return m, payloadError(OperationFilms)
		}
	}
	if schedule.ValidMK2PosterURL(f.Poster) && f.Poster == schedule.MK2PosterPrefix+f.ID {
		m.PosterURL = f.Poster
	}
	if date, err := time.Parse(time.RFC3339, f.OpeningDate); err == nil {
		m.ReleaseDate = date.Format(time.DateOnly)
	}
	seen := map[string]bool{}
	for _, g := range f.Genres {
		name := strings.TrimSpace(g.Name)
		if name != "" && len(name) <= 256 && !seen[name] {
			m.Genres = append(m.Genres, name)
			seen[name] = true
		}
	}
	sort.Strings(m.Genres)
	return m, nil
}

// Missing optional values complement each other. The first nonempty synopsis wins;
// Titles may differ only by case, preserving the first spelling. All other
// conflicting supplied values abort.
func mergeMovie(a, b schedule.MovieRecord) (schedule.MovieRecord, error) {
	if a.ProviderID == "" {
		return b, nil
	}
	if a.ProviderID != b.ProviderID {
		return a, payloadError(OperationFilms)
	}
	if a.Title != "" && b.Title != "" && !strings.EqualFold(a.Title, b.Title) {
		return a, payloadError(OperationFilms)
	}
	if a.Title == "" {
		a.Title = b.Title
	}
	for _, pair := range []struct {
		dst   *string
		value string
	}{{&a.PosterURL, b.PosterURL}, {&a.ReleaseDate, b.ReleaseDate}} {
		if *pair.dst != "" && pair.value != "" && *pair.dst != pair.value {
			return a, payloadError(OperationFilms)
		}
		if *pair.dst == "" {
			*pair.dst = pair.value
		}
	}
	if a.RuntimeMinutes != 0 && b.RuntimeMinutes != 0 && a.RuntimeMinutes != b.RuntimeMinutes {
		return a, payloadError(OperationFilms)
	}
	if a.RuntimeMinutes == 0 {
		a.RuntimeMinutes = b.RuntimeMinutes
	}
	if len(a.Genres) != 0 && len(b.Genres) != 0 && !reflect.DeepEqual(a.Genres, b.Genres) {
		return a, payloadError(OperationFilms)
	}
	if len(a.Genres) == 0 {
		a.Genres = b.Genres
	}
	if a.Overview == "" {
		a.Overview = b.Overview
	}
	return a, nil
}
func parseAttributes(attributes []attribute) (schedule.Language, string, schedule.Format, error) {
	tokens := map[string]bool{}
	for _, a := range attributes {
		tokens[a.ShortName] = true
	}
	var language schedule.Language
	var version string
	spokenVersions := 0
	for _, token := range []string{"VF", "VO", "VOF"} {
		if tokens[token] {
			spokenVersions++
		}
	}
	// A spoken version takes precedence over the provider's Muet token.
	silent := spokenVersions == 0 && tokens["Muet"]
	if spokenVersions > 1 || spokenVersions == 0 && !silent || silent && tokens["STFR"] {
		return "", "", "", payloadError(OperationComplex)
	}
	switch {
	case silent:
		version = "Muet"
	case tokens["VF"]:
		language, version = schedule.LanguageVF, "VF"
		if tokens["STFR"] {
			language, version = "VFSTF", "VF+STFR"
		}
	case tokens["VO"]:
		language, version = schedule.LanguageVO, "VO"
		if tokens["STFR"] {
			language, version = schedule.LanguageVOSTFR, "VO+STFR"
		}
	case tokens["VOF"]:
		language, version = schedule.LanguageVO, "VOF"
		if tokens["STFR"] {
			language, version = schedule.LanguageVOSTFR, "VOF+STFR"
		}
	}
	var format schedule.Format
	if tokens["2D"] {
		format = schedule.Format2D
	}
	if tokens["3D"] {
		format = schedule.Format3D
	}
	if tokens["2D"] && tokens["3D"] || tokens["IMAX"] && tokens["4DX"] {
		return "", "", "", payloadError(OperationComplex)
	}
	if tokens["IMAX"] {
		format = schedule.FormatIMAX
	}
	if tokens["4DX"] {
		format = schedule.Format4DX
	}
	if format == "" {
		return "", "", "", payloadError(OperationComplex)
	}
	return language, version, format, nil
}
