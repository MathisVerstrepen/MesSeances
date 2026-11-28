// Package scare reads the SCARE open programme of independent cinemas
// (datacinesindes.fr, Licence Ouverte) and publishes the configured cinemas.
// The dataset aggregates many ticketing platforms, so rows that cannot be
// mapped safely are skipped and counted instead of failing the snapshot.
package scare

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"messeances/api/internal/schedule"
)

var (
	// ErrShape reports a page that does not match the expected model.
	ErrShape = errors.New("invalid SCARE response structure")
	// ErrNoSources reports a synchronization without any configured cinema.
	ErrNoSources = errors.New("no SCARE source is configured")
)

var (
	localFilmID = regexp.MustCompile(`^[A-Za-z0-9]{1,40}$`)
	showID      = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)
	postalCode  = regexp.MustCompile(`^[0-9]{4,5}$`)
)

// Row is one showing of the dataset.
type Row struct {
	CinemaID   json.Number `json:"cineid"`
	Cinema     string      `json:"cinenom"`
	Address    string      `json:"cineadresse"`
	PostalCode string      `json:"cinecp"`
	City       string      `json:"cineville"`
	Latitude   *float64    `json:"_coords.lat"`
	Longitude  *float64    `json:"_coords.lon"`
	FilmID     string      `json:"filmid"`
	Title      string      `json:"filmtitle"`
	Duration   json.Number `json:"filmduration"`
	Storyline  string      `json:"filmstoryline"`
	Genres     string      `json:"filmgenre"`
	ShowID     string      `json:"showid"`
	Start      string      `json:"showstart"`
	End        string      `json:"showend"`
	Version    string      `json:"filmversion"`
	Audio      string      `json:"filmaudio"`
	BookingURL string      `json:"showurl"`
}

// Page is one data-fair page.
type Page struct {
	Total   int    `json:"total"`
	Next    string `json:"next"`
	Results []Row  `json:"results"`
}

// DecodePage decodes a page and returns the cursor of the next one, or ""
// when it is the last page.
func DecodePage(body []byte) (Page, string, error) {
	var p Page
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if decoder.Decode(&p) != nil || p.Results == nil || p.Total < 0 {
		return Page{}, "", ErrShape
	}
	if p.Next == "" || len(p.Results) == 0 {
		return p, "", nil
	}
	u, err := url.Parse(p.Next)
	if err != nil || u.Scheme != "https" || u.Host != "datacinesindes.fr" || !strings.HasPrefix(u.Path, "/data-fair/api/v1/datasets/") {
		return Page{}, "", ErrShape
	}
	after := u.Query().Get("after")
	if !cursor.MatchString(after) {
		return Page{}, "", ErrShape
	}
	return p, after, nil
}

// Clean collapses whitespace.
func Clean(value string) string { return strings.Join(strings.Fields(value), " ") }

// ParseTime parses the dataset's ISO offsets, with or without a colon.
func ParseTime(value string, location *time.Location) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05-0700"} {
		if t, err := time.Parse(layout, value); err == nil && t.Year() > 2000 {
			return t.In(location), true
		}
	}
	return time.Time{}, false
}

// RuntimeMinutes reads a duration published in minutes by some platforms and
// in seconds by others. No feature runs past 400 minutes, so larger values are
// seconds; zero or invalid values are unknown.
func RuntimeMinutes(raw json.Number) int {
	n, err := strconv.Atoi(raw.String())
	switch {
	case err != nil || n <= 0:
		return 0
	case n > 400:
		return (n + 30) / 60
	default:
		return n
	}
}

// PostalCode restores the leading zero dropped by numeric exports.
func PostalCode(raw string) string {
	value := Clean(raw)
	if !postalCode.MatchString(value) {
		return ""
	}
	if len(value) == 4 {
		return "0" + value
	}
	return value
}

// MovieID scopes the cinema's film identifier, or a title digest when the
// platform publishes none.
func MovieID(cinema string, r Row) string {
	if id := Clean(r.FilmID); localFilmID.MatchString(id) {
		return cinema + "-" + id
	}
	sum := sha256.Sum256([]byte(strings.ToLower(Clean(r.Title))))
	return cinema + "-t" + hex.EncodeToString(sum[:8])
}

// ShowingID scopes the cinema's session identifier, or "" when invalid.
func ShowingID(cinema string, r Row) string {
	if id := Clean(r.ShowID); showID.MatchString(id) {
		return cinema + "-" + id
	}
	return ""
}

// Attributes maps the platform version and audio flags to the stored
// language and provider version. Silent films have no language; rows without
// version are unknown (both empty).
func Attributes(r Row) (schedule.Language, string, bool) {
	version := Clean(r.Version)
	audio := strings.ToUpper(r.Audio)
	subtitled := false
	for _, flag := range strings.FieldsFunc(audio, func(c rune) bool { return c == ',' || c == ' ' }) {
		if flag == "ST" || flag == "OCAP" {
			subtitled = true
		}
	}
	switch version {
	case "":
		return "", "", true
	case "VF", "VERSION_LOCAL", "VERSION_ORIGINAL_LOCAL":
		if subtitled {
			return schedule.LanguageVFSTF, version, true
		}
		return schedule.LanguageVF, version, true
	case "VO":
		if subtitled {
			return schedule.LanguageVOSTFR, version, true
		}
		return schedule.LanguageVO, version, true
	case "VOST", "VOSTF", "VERSION_ORIGINAL":
		return schedule.LanguageVOSTFR, version, true
	case "VERSION_MUET":
		return "", version, true
	default:
		return "", "", false
	}
}

// BookingURL keeps an absolute HTTPS booking link in canonical form, or "".
func BookingURL(raw string) string {
	if raw == "" || len(raw) > 2048 {
		return ""
	}
	if schedule.ValidScareBookingURL(raw) {
		return raw
	}
	return ""
}

// Genres splits the comma-separated genre list. The dataset serializes a
// missing genre as the literal "null", which is dropped.
func Genres(raw string) []string {
	var genres []string
	for _, genre := range strings.Split(raw, ",") {
		if genre = Clean(genre); genre != "" && !strings.EqualFold(genre, "null") && len(genres) < 8 {
			genres = append(genres, genre)
		}
	}
	return genres
}
