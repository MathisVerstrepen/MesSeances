package cinewest

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"messeances/api/internal/schedule"
)

func webProgram(ctx context.Context, f Fetcher, from time.Time, location *time.Location) (cinemaProgram, error) {
	body, err := f.Theater(ctx)
	if err != nil {
		return cinemaProgram{}, err
	}
	var theaters struct {
		Data struct {
			AllTheater struct {
				Nodes []webTheater `json:"nodes"`
			} `json:"allTheater"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &theaters) != nil || len(theaters.Data.AllTheater.Nodes) != 1 {
		return cinemaProgram{}, errShape
	}
	c := theaters.Data.AllTheater.Nodes[0]
	if c.ID != "W8400" || c.Timezone != schedule.Timezone {
		return cinemaProgram{}, errShape
	}
	body, err = f.Schedule(ctx, from.Format("2006-01-02"), from.AddDate(1, 0, 0).Format("2006-01-02"))
	if err != nil {
		return cinemaProgram{}, err
	}
	var programs map[string]struct {
		Schedule map[string]map[string][]webShow `json:"schedule"`
	}
	if json.Unmarshal(body, &programs) != nil || len(programs) != 1 || programs[c.ID].Schedule == nil {
		return cinemaProgram{}, errShape
	}
	p := programs[c.ID].Schedule
	ids := make([]string, 0, len(p))
	for id, days := range p {
		if !schedule.ValidCinewestIdentity("movie", "webediamovies-"+id) || days == nil {
			return cinemaProgram{}, errShape
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	movies := map[string]schedule.MovieRecord{}
	for offset := 0; offset < len(ids); offset += 50 {
		batch := ids[offset:min(offset+50, len(ids))]
		body, err := f.Movies(ctx, batch)
		if err != nil {
			return cinemaProgram{}, err
		}
		var rows []webMovie
		if json.Unmarshal(body, &rows) != nil || rows == nil {
			return cinemaProgram{}, errShape
		}
		want := map[string]bool{}
		for _, id := range batch {
			want[id] = true
		}
		for _, m := range rows {
			id := string(m.ID)
			if !want[id] {
				return cinemaProgram{}, errShape
			}
			delete(want, id)
			runtime, err := secondsRuntime(m.Runtime)
			if err != nil {
				return cinemaProgram{}, err
			}
			parsed, err := movie("webediamovies-"+id, m.Title, m.Poster, runtime)
			if err != nil {
				return cinemaProgram{}, err
			}
			movies[id] = parsed
		}
		if len(want) != 0 {
			return cinemaProgram{}, errShape
		}
	}
	id := "webediamovies-" + c.ID
	address := c.PracticalInfo.Location
	result := cinemaProgram{theater: theater(id, c.Name, address.Address, address.City, address.Zip)}
	for _, movieID := range ids {
		days := p[movieID]
		dates := make([]string, 0, len(days))
		for date := range days {
			dates = append(dates, date)
		}
		sort.Strings(dates)
		for _, date := range dates {
			parsed, err := time.Parse("2006-01-02", date)
			if err != nil || parsed.Format("2006-01-02") != date || days[date] == nil {
				return cinemaProgram{}, errShape
			}
			for _, s := range days[date] {
				start, err := webStart(s.Start, location)
				if err != nil {
					return cinemaProgram{}, err
				}
				language, version, format, err := webAttributes(s.Tags)
				if err != nil {
					return cinemaProgram{}, err
				}
				room := ""
				if s.Screen != nil {
					room = clean(s.Screen.Name)
				}
				if n, err := strconv.Atoi(room); err == nil && n > 0 {
					room = "Salle " + strconv.Itoa(n)
				}
				booking := ""
				for _, entry := range s.Data.Ticketing {
					for _, link := range entry.URLs {
						if link != "" && schedule.ValidCinewestBookingURL(link, id, "") {
							booking = link
							break
						}
					}
					if booking != "" {
						break
					}
				}
				row, err := showing(id, s.ID, movies[movieID], start, start, language, version, format, room, booking, 0)
				if err != nil || row.ServiceDate != date {
					return cinemaProgram{}, errShape
				}
				result.shows = append(result.shows, row)
			}
		}
	}
	return result, nil
}
func webStart(raw string, location *time.Location) (time.Time, error) {
	if strings.HasSuffix(raw, "Z") || len(raw) > 19 {
		return offsetTime(raw, location)
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if start, err := localTime(layout, raw, location); err == nil {
			return start, nil
		}
	}
	return time.Time{}, errShape
}
func webAttributes(tags []string) (schedule.Language, string, schedule.Format, error) {
	flags := map[string]bool{}
	for _, tag := range tags {
		flags[tag] = true
	}
	language, version := schedule.Language(""), ""
	switch {
	case flags["Localization.Version.Original"] && flags["Showtime.Accessibility.Subtitled"]:
		language, version = schedule.LanguageVOSTFR, "VOSTFR"
	case flags["Localization.Language.French"]:
		language, version = schedule.LanguageVF, "VF"
	case flags["Localization.Version.Original"]:
		language, version = schedule.LanguageVO, "VO"
	default:
		return "", "", "", errShape
	}
	format := schedule.Format2D
	switch {
	case flags["Auditorium.Experience.InfinityVision"]:
		format = schedule.FormatInfinityVision
	case flags["Auditorium.Experience.Ice"]:
		format = schedule.FormatICE
	case flags["Auditorium.Experience.DolbyAtmos"]:
		format = schedule.FormatDolby
	case flags["Format.Projection.3d"]:
		format = schedule.Format3D
	}
	return language, version, format, nil
}
