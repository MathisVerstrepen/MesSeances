package cinewest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"messeances/api/internal/schedule"
)

func bootstrapToken(body []byte) (string, error) {
	tokens := html.NewTokenizer(bytes.NewReader(body))
	value := ""
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			if value == "" {
				return "", errShape
			}
			return value, nil
		case html.StartTagToken, html.SelfClosingTagToken:
			tag := tokens.Token()
			if tag.Data != "meta" {
				continue
			}
			name, content := "", ""
			for _, a := range tag.Attr {
				if a.Key == "name" {
					name = a.Val
				}
				if a.Key == "content" {
					content = a.Val
				}
			}
			if name == "api_token" {
				if content == "" || len(content) > 1024 || value != "" || strings.ContainsAny(content, "\r\n\x00") {
					return "", errShape
				}
				value = content
			}
		}
	}
}
func parseOfficeCinemas(body []byte) (map[string]officeCinema, error) {
	var rows []officeCinema
	if json.Unmarshal(body, &rows) != nil || len(rows) != 10 {
		return nil, errShape
	}
	result := map[string]officeCinema{}
	company := false
	for _, c := range rows {
		if c.ID == "cinewest" {
			if company {
				return nil, errShape
			}
			company = true
			continue
		}
		if !schedule.ValidCinewestIdentity("theater", "cineoffice-"+c.ID) || c.Token == "" || len(c.Token) > 1024 || clean(c.Name) == "" || clean(c.Address) == "" || clean(c.City) == "" || c.Postcode == "" {
			return nil, errShape
		}
		if _, ok := result[c.ID]; ok {
			return nil, errShape
		}
		result[c.ID] = c
	}
	if !company || len(result) != 9 {
		return nil, errShape
	}
	return result, nil
}
func officeProgram(ctx context.Context, f Fetcher, cinema officeCinema, location *time.Location) (cinemaProgram, error) {
	var movies []officeMovie
	var screens []officeScreen
	var options []officeOption
	var shows []officeShow
	for _, catalog := range []struct {
		name   string
		target any
	}{{"media", &movies}, {"screens", &screens}, {"mediaoptions", &options}, {"shows", &shows}} {
		body, err := f.Catalog(ctx, catalog.name, cinema.Token)
		if err != nil {
			return cinemaProgram{}, err
		}
		if json.Unmarshal(body, catalog.target) != nil {
			return cinemaProgram{}, fmt.Errorf("cine office %s catalog: %w", catalog.name, errShape)
		}
	}
	if movies == nil || screens == nil || options == nil || shows == nil {
		return cinemaProgram{}, errShape
	}
	mm := map[sourceID]schedule.MovieRecord{}
	for _, m := range movies {
		if m.CinemaID != cinema.ID {
			return cinemaProgram{}, errShape
		}
		runtime, err := secondsRuntime(m.Duration)
		if err != nil {
			return cinemaProgram{}, err
		}
		parsed, err := movie("cineoffice-"+string(m.ID), m.Title, m.Poster, runtime)
		if err != nil {
			return cinemaProgram{}, err
		}
		if old, ok := mm[m.ID]; ok && !reflect.DeepEqual(old, parsed) {
			return cinemaProgram{}, errShape
		}
		mm[m.ID] = parsed
	}
	sm := map[sourceID]string{}
	for _, s := range screens {
		if !numericID(s.ID) || s.CinemaID != cinema.ID {
			return cinemaProgram{}, fmt.Errorf("cine office screen identity: %w", errShape)
		}
		label := clean(s.Label)
		if label == "" && s.Number > 0 {
			label = "Salle " + strconv.Itoa(s.Number)
		}
		if label == "" {
			return cinemaProgram{}, errShape
		}
		if old, ok := sm[s.ID]; ok && old != label {
			return cinemaProgram{}, errShape
		}
		sm[s.ID] = label
	}
	om := map[sourceID]string{}
	for _, o := range options {
		if !numericID(o.ID) || o.CinemaID != cinema.ID || o.Label == "" {
			return cinemaProgram{}, fmt.Errorf("cine office option identity: %w", errShape)
		}
		if old, ok := om[o.ID]; ok && old != o.Label {
			return cinemaProgram{}, errShape
		}
		om[o.ID] = o.Label
	}
	id := "cineoffice-" + cinema.ID
	result := cinemaProgram{theater: theater(id, cinema.Name, cinema.Address, cinema.City, cinema.Postcode)}
	for _, s := range shows {
		m, ok := mm[s.Movie.ID]
		if !ok || !numericID(s.ID) || s.CinemaID != cinema.ID || sm[s.Screen.ID] == "" {
			return cinemaProgram{}, fmt.Errorf("cine office showing association: %w", errShape)
		}
		start, err := offsetTime(s.Start, location)
		if err != nil {
			return cinemaProgram{}, err
		}
		end, err := offsetTime(s.End, location)
		if err != nil || !end.After(start) {
			return cinemaProgram{}, errShape
		}
		language, version, format, err := officeAttributes(s, om)
		if err != nil {
			return cinemaProgram{}, err
		}
		row, err := showing(id, string(s.ID), m, start, end, language, version, format, sm[s.Screen.ID], "", 0)
		if err != nil {
			return cinemaProgram{}, err
		}
		result.shows = append(result.shows, row)
	}
	return result, nil
}
func officeAttributes(s officeShow, options map[sourceID]string) (schedule.Language, string, schedule.Format, error) {
	flags := map[string]bool{}
	ids := []sourceID{s.Language, s.Format}
	for _, o := range s.Options {
		ids = append(ids, o.Option.ID)
	}
	for _, id := range ids {
		label, ok := options[id]
		if !ok {
			return "", "", "", errShape
		}
		flags[label] = true
	}
	version := options[s.Language]
	language := schedule.LanguageVF
	switch version {
	case "VERSION_ORIGINAL":
		language = schedule.LanguageVOSTFR
	case "VERSION_LOCAL", "VERSION_ORIGINAL_LOCAL":
		if flags["SUBTITLE_NORMAL"] || flags["SUBTITLE_OCAP"] || flags["SUBTITLE_CCAP"] {
			language = schedule.LanguageVFSTF
		}
	case "VERSION_MUET":
		language = ""
	default:
		return "", "", "", errShape
	}
	for flag := range flags {
		if strings.HasPrefix(flag, "VERSION_") && flag != version {
			return "", "", "", errShape
		}
	}
	format := schedule.Format2D
	switch {
	case flags["PICTURE_IMAX"]:
		format = schedule.FormatIMAX
	case flags["PICTURE_SCREENX"]:
		format = schedule.FormatScreenX
	case flags["PICTURE_3D"] || s.ThreeD:
		format = schedule.Format3D
	}
	return language, version, format, nil
}
