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
	"messeances/api/internal/trailer"
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
	Visa         scalar          `json:"visa"`
	Title        string          `json:"titre_cotecine"`
	Metadata     json.RawMessage `json:"movie_data"`
	Dates        dateList        `json:"dates"`
	alternateVAD bool
}

func (f *film) UnmarshalJSON(b []byte) error {
	*f = film{}
	if isAlternateVADFilm(b) || isUnavailableVADFilm(b) {
		f.alternateVAD = true
		return nil
	}
	type standardFilm film
	var standard standardFilm
	if err := json.Unmarshal(b, &standard); err != nil {
		return err
	}
	*f = film(standard)
	return nil
}

// suppliedFilmVisa remembers any non-null visa, including duplicate or differently
// cased keys matched by encoding/json. A later null must not hide a supplied ID.
type suppliedFilmVisa bool

func (v *suppliedFilmVisa) UnmarshalJSON(b []byte) error {
	if !bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		*v = true
	}
	return nil
}

func isAlternateVADFilm(b []byte) bool {
	var candidate struct {
		Visa   suppliedFilmVisa `json:"visa"`
		Number string           `json:"num_visa"`
		ID     *string          `json:"idfilm_cotecine"`
		Dates  []struct {
			Showtimes []struct {
				ID        json.RawMessage `json:"id_seance"`
				Room      json.RawMessage `json:"salle"`
				ForSale   json.RawMessage `json:"places_en_vente_vad"`
				Remaining json.RawMessage `json:"places_vad_restantes"`
			} `json:"showtimes"`
		} `json:"dates"`
	}
	if json.Unmarshal(b, &candidate) != nil || candidate.Visa || strings.TrimSpace(candidate.Number) == "" || candidate.ID == nil || candidate.Dates == nil {
		return false
	}
	hasSessions := false
	for _, date := range candidate.Dates {
		if date.Showtimes == nil {
			return false
		}
		for _, s := range date.Showtimes {
			if len(s.ID) != 0 || len(s.Room) != 0 || len(s.ForSale) == 0 || len(s.Remaining) == 0 {
				return false
			}
			hasSessions = true
		}
	}
	return hasSessions
}

// The provider also publishes VAD placeholders without a film or session ID.
// They cannot identify a showing; accept only the observed zero/null sentinels,
// never a standard film with a supplied visa or a mixture of real sessions.
func isUnavailableVADFilm(b []byte) bool {
	var candidate struct {
		Visa   suppliedFilmVisa `json:"visa"`
		Number json.RawMessage  `json:"num_visa"`
		ID     json.RawMessage  `json:"idfilm_cotecine"`
		Dates  []struct {
			Showtimes []struct {
				ID        json.RawMessage `json:"id_seance"`
				Room      json.RawMessage `json:"salle"`
				ForSale   json.RawMessage `json:"places_en_vente_vad"`
				Remaining json.RawMessage `json:"places_vad_restantes"`
			} `json:"showtimes"`
		} `json:"dates"`
	}
	if json.Unmarshal(b, &candidate) != nil || bool(candidate.Visa) ||
		!bytes.Equal(bytes.TrimSpace(candidate.ID), []byte("null")) || candidate.Dates == nil {
		return false
	}
	number := string(bytes.TrimSpace(candidate.Number))
	if number != "0" && !schedule.ValidCinevilleIdentity("movie", number) {
		return false
	}
	hasSessions := false
	for _, date := range candidate.Dates {
		if date.Showtimes == nil {
			return false
		}
		for _, s := range date.Showtimes {
			if !bytes.Equal(bytes.TrimSpace(s.ID), []byte("0")) || len(s.Room) != 0 ||
				!bytes.Equal(bytes.TrimSpace(s.ForSale), []byte("0")) || len(s.Remaining) == 0 {
				return false
			}
			hasSessions = true
		}
	}
	return hasSessions
}

type dateList []programDate

func (d *dateList) UnmarshalJSON(b []byte) error {
	*d = nil
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return errPayload
	}
	if b[0] == '[' {
		var dates []programDate
		if err := json.Unmarshal(b, &dates); err != nil {
			return err
		}
		*d = dates
		return nil
	}
	if b[0] != '{' {
		return errPayload
	}
	var sentinel map[string]json.RawMessage
	if json.Unmarshal(b, &sentinel) != nil || len(sentinel) != 2 || !bytes.Equal(bytes.TrimSpace(sentinel["result"]), []byte("false")) {
		return errPayload
	}
	var message *string
	if json.Unmarshal(sentinel["message"], &message) != nil || message == nil {
		return errPayload
	}
	// An unavailable film is empty, not missing; never retain the provider message.
	*d = dateList{}
	return nil
}

type programDate struct {
	Date      scalar      `json:"date"`
	Showtimes sessionList `json:"showtimes"`
}

type sessionList []session

func (s *sessionList) UnmarshalJSON(b []byte) error {
	*s = nil
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return errPayload
	}
	if b[0] == '[' {
		var sessions []session
		if err := json.Unmarshal(b, &sessions); err != nil {
			return err
		}
		*s = sessions
		return nil
	}
	if b[0] != '{' {
		return errPayload
	}
	var sentinel map[string]json.RawMessage
	if json.Unmarshal(b, &sentinel) != nil || len(sentinel) != 2 || !bytes.Equal(bytes.TrimSpace(sentinel["result"]), []byte("false")) {
		return errPayload
	}
	var message *string
	if json.Unmarshal(sentinel["message"], &message) != nil || message == nil {
		return errPayload
	}
	// An unavailable date is empty, not missing; never retain the provider message.
	*s = sessionList{}
	return nil
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
var bareMinuteRuntime = regexp.MustCompile(`^([0-9]+)$`)

func parseRuntime(raw string) (int, error) {
	v := strings.ToLower(strings.Join(strings.Fields(raw), ""))
	if v == "" || v == "nc" {
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
			parts = bareMinuteRuntime.FindStringSubmatch(strings.TrimSpace(raw))
		}
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
	entries, err := parseMovieMetadata(raw)
	if err != nil {
		return m, err
	}
	if len(entries) > 0 {
		v := entries[0]
		m.SourceTrailer, m.TrailerObserved = trailer.Cineville(v.VideoID, v.ShowVideo)
		var metadataVisa scalar
		if json.Unmarshal(v.Visa, &metadataVisa) != nil || string(metadataVisa) != id {
			m.SourceTrailer = nil
			m.TrailerObserved = false
		}
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
	if m.Title == "" {
		return m, errPayload
	}
	return m, nil
}

// movie_data is published either as a legacy array or a single metadata object.
type movieMetadata struct {
	VideoID   json.RawMessage `json:"video_id"`
	ShowVideo json.RawMessage `json:"show_video"`
	Visa      json.RawMessage `json:"visa"`
	Title     string          `json:"titre"`
	Runtime   string          `json:"duree"`
	Poster    string          `json:"affichette"`
	Overview  string          `json:"synopsis"`
	Genre     string          `json:"genreprincipal"`
	Release   *string         `json:"datedesortie"`
}

func parseMovieMetadata(raw []byte) ([]movieMetadata, error) {
	// The current source uses null when metadata is unavailable. The required
	// source title still identifies the film; optional fields remain unknown.
	if bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	if raw[0] == '[' {
		var entries []movieMetadata
		if json.Unmarshal(raw, &entries) != nil {
			return nil, errPayload
		}
		return entries, nil
	}
	if raw[0] != '{' {
		return nil, errPayload
	}
	var object struct {
		movieMetadata
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(raw, &object) != nil {
		return nil, errPayload
	}
	if len(object.Result) > 0 {
		if !bytes.Equal(bytes.TrimSpace(object.Result), []byte("false")) {
			return nil, errPayload
		}
		return nil, nil
	}
	// Distinguish the observed metadata object from an arbitrary/error object.
	if strings.TrimSpace(object.Title) == "" {
		return nil, errPayload
	}
	return []movieMetadata{object.movieMetadata}, nil
}

func mergeMovie(first, next schedule.MovieRecord) schedule.MovieRecord {
	if first.ProviderID == "" {
		return next
	}
	if next.TrailerObserved {
		first.TrailerObserved = true
		if first.SourceTrailer == nil || next.SourceTrailer != nil && next.SourceTrailer.URL < first.SourceTrailer.URL {
			first.SourceTrailer = next.SourceTrailer
		}
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
	if next.SourcePageURL != "" && (first.SourcePageURL == "" || next.SourcePageURL < first.SourcePageURL) {
		first.SourcePageURL = next.SourcePageURL
	}
	return first
}

func sourceMoviePageURL(f film, c cinema) string {
	route := schedule.CinevilleRoute(string(c.ID))
	if route == "" || route != c.Route {
		return ""
	}
	rawMetadata := bytes.TrimSpace(f.Metadata)
	if len(rawMetadata) == 0 {
		return ""
	}
	entries, err := parseMovieMetadata(rawMetadata)
	if err != nil || len(entries) == 0 {
		return ""
	}
	var visa scalar
	if json.Unmarshal(entries[0].Visa, &visa) != nil || visa != f.Visa {
		return ""
	}
	// React turns numeric visas into DOM IDs through JavaScript's Number.
	// String visas preserve int64 precision; unsafe numeric values do not.
	if bytes.TrimSpace(entries[0].Visa)[0] != '"' {
		n, err := strconv.ParseInt(string(visa), 10, 64)
		if err != nil || n < -9007199254740991 || n > 9007199254740991 {
			return ""
		}
	}
	raw := "https://www.cineville.fr/programmes/" + route + "#" + string(visa)
	if !schedule.ValidSourceMoviePageURL(schedule.ProviderCineville, string(f.Visa), raw) {
		return ""
	}
	return raw
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
	case "VOF":
		language = schedule.LanguageVO
		if s.Subtitles == "1" || s.Subtitles == "true" {
			language = schedule.LanguageVOSTFR
		}
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
