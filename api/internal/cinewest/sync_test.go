package cinewest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

// All data and tokens below are synthetic. No live response fixtures are stored.
type fixtureFetcher struct {
	catalogs                  map[string][]byte
	programs                  map[string][]byte
	theater, calendar, movies []byte
}

func (f *fixtureFetcher) RequestCount() int { return 44 }
func (f *fixtureFetcher) Bootstrap(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []byte(`<meta name="api_token" content="synthetic-company">`), nil
}
func (f *fixtureFetcher) Catalog(ctx context.Context, kind, token string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b, ok := f.catalogs[token+"/"+kind]
	if !ok {
		return nil, errShape
	}
	return b, nil
}
func (f *fixtureFetcher) Program(_ context.Context, id string) ([]byte, error) {
	return f.programs[id], nil
}
func (f *fixtureFetcher) Theater(context.Context) ([]byte, error) { return f.theater, nil }
func (f *fixtureFetcher) Schedule(_ context.Context, from, to string) ([]byte, error) {
	if from != "2026-09-14" || to != "2027-09-14" {
		return nil, errShape
	}
	return f.calendar, nil
}
func (f *fixtureFetcher) Movies(context.Context, []string) ([]byte, error) { return f.movies, nil }
func jsonFixture(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
func newFixture() *fixtureFetcher {
	f := &fixtureFetcher{catalogs: map[string][]byte{}, programs: map[string][]byte{}}
	cs := []officeCinema{{ID: "cinewest"}}
	for _, id := range schedule.CinewestTheaterIDs() {
		if !strings.HasPrefix(id, "cineoffice-") {
			continue
		}
		slug := strings.TrimPrefix(id, "cineoffice-")
		c := officeCinema{ID: slug, Name: "Cinema " + slug, Address: "1 Rue du Cinema", City: "Royan", Postcode: "17200", Token: "synthetic-" + slug}
		cs = append(cs, c)
		f.catalogs[c.Token+"/media"] = jsonFixture([]officeMovie{{ID: "1", CinemaID: slug, Title: "Office film", Duration: 5400}})
		f.catalogs[c.Token+"/screens"] = jsonFixture([]officeScreen{{ID: "1", CinemaID: slug, Number: 1}})
		f.catalogs[c.Token+"/mediaoptions"] = jsonFixture([]officeOption{{ID: "1", CinemaID: slug, Label: "VERSION_LOCAL"}, {ID: "2", CinemaID: slug, Label: "PICTURE_2K"}})
		s := officeShow{ID: "1", CinemaID: slug, Movie: officeRef{ID: "1"}, Screen: officeRef{ID: "1"}, Language: "1", Format: "2", Start: "2026-09-14T20:00:00.123456+0200", End: "2026-09-14T23:07:00.123456+0200"}
		f.catalogs[c.Token+"/shows"] = jsonFixture([]officeShow{s})
	}
	f.catalogs["synthetic-company/cinemas"] = jsonFixture(cs)
	for site, program := range map[string]string{"EMS1185": "5053122", "EMS1317": "7075033", "EMS0042": "4411540"} {
		p := ticketProgram{ID: program, Name: "Partner", Address: "1 Rue du Cinema", City: "Bethune", Zip: "62400"}
		p.EMS.ID = site[3:]
		p.Events = []ticketEvent{{ID: "ABCDE", Title: "Ticket film", Duration: 90, Sessions: []ticketSession{{ID: "emsx" + site[3:] + "00000001", Date: "202609142000", Room: "Salle 1", Version: "VF", FirstPart: 15}}}}
		f.programs[site] = jsonFixture(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"schedule": p}})
	}
	f.theater = []byte(`{"data":{"allTheater":{"nodes":[{"id":"W8400","name":"Capitole","timeZone":"Europe/Paris","practicalInfo":{"location":{"address":"1 Avenue","city":"Le Pontet","zip":"84130"}}}]}}}`)
	f.calendar = []byte(`{"W8400":{"schedule":{"1":{"2026-09-14":[{"id":"1 arbitrary composite source ID","startsAt":"2026-09-14T20:00:00+02:00","tags":["Localization.Language.French"],"screen":{"name":"1"}}]}}}}`)
	f.movies = []byte(`[{"id":1,"title":"Capitole film","runtime":5400}]`)
	return f
}
func syncFixture(t *testing.T, f *fixtureFetcher) (schedule.Dataset, error) {
	t.Helper()
	d, _, err := Sync(t.Context(), f, SyncOptions{From: "2026-09-14", Now: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)})
	return d, err
}

func TestWebediaInfinityVisionSessionIsolation(t *testing.T) {
	const infinity = "Auditorium.Experience.InfinityVision"
	const ice = "Auditorium.Experience.Ice"
	const dolby = "Auditorium.Experience.DolbyAtmos"
	cases := []struct {
		tags []string
		want schedule.Format
	}{
		{[]string{infinity}, schedule.FormatInfinityVision},
		{[]string{infinity, dolby}, schedule.FormatInfinityVision},
		{[]string{dolby, infinity, "Format.Projection.3d"}, schedule.FormatInfinityVision},
		{[]string{ice, infinity}, schedule.FormatInfinityVision},
		{[]string{infinity, ice}, schedule.FormatInfinityVision},
		{[]string{dolby}, schedule.FormatDolby},
		{[]string{ice}, schedule.FormatICE},
		{nil, schedule.Format2D},
		{[]string{infinity + "Extra"}, schedule.Format2D},
		{[]string{"auditorium.experience.infinityvision"}, schedule.Format2D},
	}
	f := newFixture()
	baseline, err := syncFixture(t, f)
	if err != nil {
		t.Fatal(err)
	}
	sessions := make([]map[string]any, 0, len(cases))
	wantByRoom := make(map[string]schedule.Format, len(cases))
	for i, tc := range cases {
		wantByRoom["Salle "+fmt.Sprint(i+1)] = tc.want
		sessions = append(sessions, map[string]any{"id": fmt.Sprint(i + 1), "startsAt": "2026-09-14T20:00:00+02:00", "tags": append([]string{"Localization.Language.French"}, tc.tags...), "screen": map[string]string{"name": fmt.Sprint(i + 1)}})
	}
	f.calendar = jsonFixture(map[string]any{"W8400": map[string]any{"moviesTags": map[string]any{"1": []string{infinity}}, "schedule": map[string]any{"1": map[string]any{"2026-09-14": sessions}}}})
	d, err := syncFixture(t, f)
	if err != nil || len(d.Showtimes) != 12+len(cases) {
		t.Fatal(d, err)
	}
	seen := 0
	for _, showing := range d.Showtimes {
		if strings.HasPrefix(showing.ProviderShowingID, "webediamovies-") {
			seen++
			want, ok := wantByRoom[showing.Room]
			if !ok || showing.Format != want || showing.Language != schedule.LanguageVF || !showing.EndTime.Equal(showing.StartTime) {
				t.Fatal(showing)
			}
			delete(wantByRoom, showing.Room)
			continue
		}
		matched := false
		for _, before := range baseline.Showtimes {
			if before.ID == showing.ID {
				matched = reflect.DeepEqual(before, showing)
			}
		}
		if !matched {
			t.Fatal("other adapter changed", showing)
		}
	}
	if seen != len(cases) || len(wantByRoom) != 0 {
		t.Fatal("Webedia sessions missing", seen)
	}
}

func TestSyncThirteenVenuesAndPlatformEnds(t *testing.T) {
	f := newFixture()
	d, err := syncFixture(t, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Theaters) != 13 || len(d.Showtimes) != 13 {
		t.Fatal("incomplete coverage")
	}
	for _, s := range d.Showtimes {
		switch {
		case strings.HasPrefix(s.ProviderShowingID, "cineoffice-"):
			if s.EndTime.Sub(s.StartTime) != 187*time.Minute || s.EndTime.Nanosecond() != 123456000 || s.FirstPartDurationMinutes != 0 {
				t.Fatal("published end overwritten")
			}
		case strings.HasPrefix(s.ProviderShowingID, "ticketingcine-"):
			if s.EndTime.Sub(s.StartTime) != 105*time.Minute || s.FirstPartDurationMinutes != 15 {
				t.Fatal("ticket end")
			}
		default:
			if !s.EndTime.Equal(s.StartTime) {
				t.Fatal("Capitole end synthesized")
			}
		}
	}
	again, err := syncFixture(t, f)
	if err != nil || !reflect.DeepEqual(d, again) {
		t.Fatal("nondeterministic snapshot")
	}
	f.catalogs["synthetic-royanlelido/shows"] = []byte(`[]`)
	d, err = syncFixture(t, f)
	if err != nil || len(d.Theaters) != 13 || len(d.Showtimes) != 12 {
		t.Fatal("empty cinema lost", err)
	}
	for _, c := range d.Theaters {
		if c.ProviderID == "cineoffice-royanlelido" && len(c.AvailableDates) != 0 {
			t.Fatal("empty dates")
		}
	}
}
func TestSyncRejectsIncompleteCatalogsAtomically(t *testing.T) {
	for name, mutate := range map[string]func(*fixtureFetcher){
		"company only":           func(f *fixtureFetcher) { f.catalogs["synthetic-company/cinemas"] = []byte(`[{"id":"cinewest"}]`) },
		"wrong token":            func(f *fixtureFetcher) { delete(f.catalogs, "synthetic-royanlelido/media") },
		"null shows":             func(f *fixtureFetcher) { f.catalogs["synthetic-royanlelido/shows"] = []byte(`null`) },
		"null media":             func(f *fixtureFetcher) { f.catalogs["synthetic-royanlelido/media"] = []byte(`null`) },
		"orphan room":            func(f *fixtureFetcher) { f.catalogs["synthetic-royanlelido/screens"] = []byte(`[]`) },
		"orphan option":          func(f *fixtureFetcher) { f.catalogs["synthetic-royanlelido/mediaoptions"] = []byte(`[]`) },
		"wrong referer response": func(f *fixtureFetcher) { f.programs["EMS1185"] = f.programs["EMS1317"] },
		"missing movies":         func(f *fixtureFetcher) { f.movies = []byte(`[]`) },
		"wrong theater":          func(f *fixtureFetcher) { f.calendar = []byte(`{"W0000":{"schedule":{}}}`) },
		"missing end": func(f *fixtureFetcher) {
			f.catalogs["synthetic-royanlelido/shows"] = []byte(strings.ReplaceAll(string(f.catalogs["synthetic-royanlelido/shows"]), "2026-09-14T23:07:00.123456+0200", ""))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			mutate(f)
			d, err := syncFixture(t, f)
			if err == nil || len(d.Theaters) != 0 || len(d.Showtimes) != 0 {
				t.Fatal("partial snapshot accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err := Sync(ctx, newFixture(), SyncOptions{From: "2026-09-14", Now: time.Now()})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation", err)
	}
}

func fixtureOfficeShows(t *testing.T, f *fixtureFetcher, cinema string) []officeShow {
	t.Helper()
	var rows []officeShow
	if err := json.Unmarshal(f.catalogs["synthetic-"+cinema+"/shows"], &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestSyncSkipsOnlyAbsentOfficeFilmsAndRestoresSessions(t *testing.T) {
	f := newFixture()
	baseline, err := syncFixture(t, f)
	if err != nil {
		t.Fatal(err)
	}
	const cinema = "royanlelido"
	rows := fixtureOfficeShows(t, f, cinema)
	orphan := rows[0]
	orphan.ID, orphan.Movie.ID = "2", "4710"
	orphan.Start, orphan.End = "2026-11-20T20:00:00+0100", "2026-11-20T22:00:00+0100"
	f.catalogs["synthetic-"+cinema+"/shows"] = jsonFixture(append(rows, orphan))
	options := SyncOptions{From: "2026-09-14", Now: baseline.GeneratedAt}
	d, summary, err := Sync(t.Context(), f, options)
	if err != nil || !reflect.DeepEqual(d, baseline) || summary.Skipped != 1 || summary.Cinemas != 13 || summary.Showtimes != 13 {
		t.Fatal("orphan changed valid sessions or cinema coverage", summary, err)
	}
	loc, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	p, err := officeProgram(t.Context(), f, officeCinema{ID: cinema, Token: "synthetic-" + cinema}, loc)
	if err != nil || p.skipped != 1 || len(p.shows) != 1 {
		t.Fatal("per-program omission count", err)
	}
	// An entirely orphaned cinema must remain present, with no available dates.
	f.catalogs["synthetic-aurillaclecristal/media"] = []byte(`[]`)
	d, summary, err = Sync(t.Context(), f, options)
	if err != nil || summary.Skipped != 2 || summary.Cinemas != 13 || len(d.Showtimes) != 12 {
		t.Fatal("omissions not aggregated or empty cinema lost", summary, err)
	}
	found := false
	for _, theater := range d.Theaters {
		if theater.ProviderID == "cineoffice-aurillaclecristal" {
			found = true
			if len(theater.AvailableDates) != 0 {
				t.Fatal("orphan supplied an available date")
			}
		}
	}
	if !found {
		t.Fatal("orphan-only cinema missing")
	}
	// Source metadata returning automatically restores the exact session identity.
	f.catalogs["synthetic-aurillaclecristal/media"] = newFixture().catalogs["synthetic-aurillaclecristal/media"]
	f.catalogs["synthetic-"+cinema+"/media"] = jsonFixture([]officeMovie{
		{ID: "1", CinemaID: cinema, Title: "Office film", Duration: 5400},
		{ID: "4710", CinemaID: cinema, Title: "Restored film", Duration: 5280},
	})
	d, summary, err = Sync(t.Context(), f, options)
	if err != nil || summary.Skipped != 0 || summary.Cinemas != 13 || len(d.Showtimes) != 14 || d.Window.Through != "2026-11-20" {
		t.Fatal("restored metadata not included", summary, err)
	}
	expectedID, _ := schedule.CinewestShowingID("cineoffice-"+cinema, "2")
	found = false
	for _, show := range d.Showtimes {
		if show.ProviderShowingID == expectedID {
			found = true
			if show.Movie.ProviderID != "cineoffice-4710" || show.Movie.Title != "Restored film" || show.EndTime.Sub(show.StartTime) != 2*time.Hour || show.Language != schedule.LanguageVF {
				t.Fatal("restored session normalization changed")
			}
			continue
		}
		matched := false
		for _, before := range baseline.Showtimes {
			if before.ID == show.ID {
				matched = reflect.DeepEqual(before, show)
			}
		}
		if !matched {
			t.Fatal("valid session changed on metadata restoration")
		}
	}
	if !found {
		t.Fatal("restored session missing")
	}
}

func TestOfficeOrphanSessionsStillRejectMalformedDataAtomically(t *testing.T) {
	for name, mutate := range map[string]func(*officeShow){
		"missing show id":        func(s *officeShow) { s.ID = "" },
		"nonnumeric show id":     func(s *officeShow) { s.ID = "invalid" },
		"zero show id":           func(s *officeShow) { s.ID = "0" },
		"oversized show id":      func(s *officeShow) { s.ID = sourceID(strings.Repeat("1", 115)) },
		"missing film reference": func(s *officeShow) { s.Movie.ID = "" },
		"nonnumeric film":        func(s *officeShow) { s.Movie.ID = "invalid" },
		"zero film":              func(s *officeShow) { s.Movie.ID = "0" },
		"leading zero film":      func(s *officeShow) { s.Movie.ID = "04710" },
		"oversized film":         func(s *officeShow) { s.Movie.ID = sourceID(strings.Repeat("1", 105)) },
		"wrong cinema":           func(s *officeShow) { s.CinemaID = "aurillaclecristal" },
		"missing screen":         func(s *officeShow) { s.Screen.ID = "" },
		"orphan screen":          func(s *officeShow) { s.Screen.ID = "40" },
		"missing start":          func(s *officeShow) { s.Start = "" },
		"invalid start":          func(s *officeShow) { s.Start = "not-a-time" },
		"start without offset":   func(s *officeShow) { s.Start = "2026-09-14T20:00:00" },
		"missing end":            func(s *officeShow) { s.End = "" },
		"invalid end":            func(s *officeShow) { s.End = "not-a-time" },
		"end without offset":     func(s *officeShow) { s.End = "2026-09-14T23:07:00" },
		"equal end":              func(s *officeShow) { s.End = s.Start },
		"end before start":       func(s *officeShow) { s.End = "2026-09-14T19:00:00+0200" },
		"missing language":       func(s *officeShow) { s.Language = "" },
		"orphan language":        func(s *officeShow) { s.Language = "154" },
		"missing format":         func(s *officeShow) { s.Format = "" },
		"orphan format":          func(s *officeShow) { s.Format = "150" },
		"orphan extra option": func(s *officeShow) {
			if err := json.Unmarshal([]byte(`[{"mediaoptionsid":{"id":99}}]`), &s.Options); err != nil {
				panic(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			rows := fixtureOfficeShows(t, f, "royanlelido")
			orphan := rows[0]
			orphan.ID, orphan.Movie.ID = "2", "4710"
			mutate(&orphan)
			f.catalogs["synthetic-royanlelido/shows"] = jsonFixture(append(rows, orphan))
			d, err := syncFixture(t, f)
			if err == nil || len(d.Theaters) != 0 || len(d.Showtimes) != 0 {
				t.Fatal("malformed orphan silently skipped or partial snapshot accepted")
			}
		})
	}
}

func TestOfficeOrphansDoNotHideCatalogCorruption(t *testing.T) {
	for name, mutate := range map[string]func(*fixtureFetcher){
		"invalid movie title": func(f *fixtureFetcher) {
			f.catalogs["synthetic-royanlelido/media"] = []byte(`[{"id":1,"cinemaid":"royanlelido","title":"","duration":5400}]`)
		},
		"invalid movie id": func(f *fixtureFetcher) {
			f.catalogs["synthetic-royanlelido/media"] = []byte(`[{"id":"bad","cinemaid":"royanlelido","title":"Office film","duration":5400}]`)
		},
		"invalid movie duration": func(f *fixtureFetcher) {
			f.catalogs["synthetic-royanlelido/media"] = []byte(`[{"id":1,"cinemaid":"royanlelido","title":"Office film","duration":1}]`)
		},
		"wrong movie cinema": func(f *fixtureFetcher) {
			f.catalogs["synthetic-royanlelido/media"] = []byte(`[{"id":1,"cinemaid":"aurillaclecristal","title":"Office film","duration":5400}]`)
		},
		"conflicting movies": func(f *fixtureFetcher) {
			f.catalogs["synthetic-royanlelido/media"] = jsonFixture([]officeMovie{{ID: "1", CinemaID: "royanlelido", Title: "One"}, {ID: "1", CinemaID: "royanlelido", Title: "Two"}})
		},
		"missing screens": func(f *fixtureFetcher) { f.catalogs["synthetic-royanlelido/screens"] = []byte(`[]`) },
		"wrong screen cinema": func(f *fixtureFetcher) {
			f.catalogs["synthetic-royanlelido/screens"] = jsonFixture([]officeScreen{{ID: "1", CinemaID: "aurillaclecristal", Number: 1}})
		},
		"missing options": func(f *fixtureFetcher) { f.catalogs["synthetic-royanlelido/mediaoptions"] = []byte(`[]`) },
		"unknown language": func(f *fixtureFetcher) {
			f.catalogs["synthetic-royanlelido/mediaoptions"] = jsonFixture([]officeOption{{ID: "1", CinemaID: "royanlelido", Label: "VERSION_UNKNOWN"}, {ID: "2", CinemaID: "royanlelido", Label: "PICTURE_2K"}})
		},
		"conflicting language": func(f *fixtureFetcher) {
			f.catalogs["synthetic-royanlelido/mediaoptions"] = jsonFixture([]officeOption{{ID: "1", CinemaID: "royanlelido", Label: "VERSION_LOCAL"}, {ID: "2", CinemaID: "royanlelido", Label: "VERSION_ORIGINAL"}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			rows := fixtureOfficeShows(t, f, "royanlelido")
			rows[0].Movie.ID = "4710"
			f.catalogs["synthetic-royanlelido/shows"] = jsonFixture(rows)
			mutate(f)
			d, err := syncFixture(t, f)
			if err == nil || len(d.Theaters) != 0 || len(d.Showtimes) != 0 {
				t.Fatal("corrupt catalog hidden by orphan omission")
			}
		})
	}
}

func TestOfficeOrphanDuplicatePolicy(t *testing.T) {
	for _, name := range []string{"identical orphan", "conflicting orphan time", "conflicting orphan film", "conflicting valid session"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			rows := fixtureOfficeShows(t, f, "royanlelido")
			orphan := rows[0]
			orphan.ID, orphan.Movie.ID = "2", "4710"
			duplicate := orphan
			switch name {
			case "conflicting orphan time":
				duplicate.End = "2026-09-14T23:08:00.123456+0200"
			case "conflicting orphan film":
				duplicate.Movie.ID = "4711"
			case "conflicting valid session":
				duplicate.Movie.ID = "1"
			}
			f.catalogs["synthetic-royanlelido/shows"] = jsonFixture(append(rows, orphan, duplicate))
			d, summary, err := Sync(t.Context(), f, SyncOptions{From: "2026-09-14", Now: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)})
			if name == "identical orphan" {
				if err != nil || summary.Skipped != 1 || len(d.Theaters) != 13 || len(d.Showtimes) != 13 {
					t.Fatal("identical orphan not deduplicated", summary, err)
				}
			} else if err == nil || len(d.Theaters) != 0 || len(d.Showtimes) != 0 {
				t.Fatal("conflicting session hidden by omission")
			}
		})
	}
}

func TestTimeAndLanguageContracts(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	for _, raw := range []string{"202603290230", "202610250230"} {
		if _, err := localTime("200601021504", raw, loc); err == nil {
			t.Fatal("DST ambiguity accepted")
		}
	}
	for _, raw := range []string{"2026-09-14T20:00:00", "2026-09-14T20:00:00+25:00"} {
		if _, err := offsetTime(raw, loc); err == nil {
			t.Fatal("wrong offset accepted")
		}
	}
	utc, err := offsetTime("2026-09-14T18:00:00.123456Z", loc)
	if err != nil || utc.Hour() != 20 || utc.Nanosecond() != 123456000 {
		t.Fatal("offset instant not normalized to Paris")
	}
	for _, v := range []struct {
		code string
		want schedule.Language
	}{{"VERSION_LOCAL", schedule.LanguageVF}, {"VERSION_ORIGINAL_LOCAL", schedule.LanguageVF}, {"VERSION_ORIGINAL", schedule.LanguageVOSTFR}, {"VERSION_MUET", ""}} {
		l, version, format, err := officeAttributes(officeShow{Language: "1", Format: "2"}, map[sourceID]string{"1": v.code, "2": "PICTURE_SCREENX"})
		if err != nil || l != v.want || version != v.code || format != schedule.FormatScreenX {
			t.Fatal("office attributes")
		}
	}
	if _, _, _, err := officeAttributes(officeShow{Language: "1", Format: "2"}, map[sourceID]string{"1": "VERSION_UNKNOWN", "2": "PICTURE_2K"}); err == nil {
		t.Fatal("unknown version accepted")
	}
}

func TestOfficeDuplicatesAndPublishedHorizon(t *testing.T) {
	const key = "synthetic-royanlelido/shows"
	for _, conflict := range []bool{false, true} {
		f := newFixture()
		var rows []officeShow
		if err := json.Unmarshal(f.catalogs[key], &rows); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, rows[0])
		if conflict {
			rows[1].End = "2026-09-14T23:08:00.123456+0200"
		}
		f.catalogs[key] = jsonFixture(rows)
		d, err := syncFixture(t, f)
		if conflict && err == nil || !conflict && (err != nil || len(d.Showtimes) != 13) {
			t.Fatal("duplicate policy", err)
		}
	}
	f := newFixture()
	var rows []officeShow
	if err := json.Unmarshal(f.catalogs[key], &rows); err != nil {
		t.Fatal(err)
	}
	base := rows[0]
	rows = nil
	for i, stamp := range []struct{ start, end string }{{"2026-09-14T02:00:00+0200", "2026-09-14T04:00:00+0200"}, {"2026-09-15T02:00:00+0200", "2026-09-15T04:00:00+0200"}, {"2027-06-27T20:00:00+0200", "2027-06-27T23:07:00+0200"}} {
		s := base
		s.ID, s.Start, s.End = sourceID(string(rune('1'+i))), stamp.start, stamp.end
		rows = append(rows, s)
	}
	f.catalogs[key] = jsonFixture(rows)
	d, err := syncFixture(t, f)
	if err != nil || d.Window.Through != "2027-06-27" || len(d.Showtimes) != 14 {
		t.Fatal("published horizon or expired service day", err)
	}
	for _, c := range d.Theaters {
		if c.ProviderID == "cineoffice-royanlelido" && !reflect.DeepEqual(c.AvailableDates, []string{"2026-09-14", "2027-06-27"}) {
			t.Fatal("rollover date union")
		}
	}
	f.catalogs["synthetic-royanlelido/media"] = []byte(`[{"id":1,"cinemaid":"royanlelido","title":"Office film","duration":0}]`)
	// Shared global movie metadata must agree even across cinemas.
	if _, err := syncFixture(t, f); err == nil {
		t.Fatal("conflicting global movie runtime accepted")
	}
}

func TestSourceIDAndBootstrapRejectMalformedInput(t *testing.T) {
	for _, raw := range []string{`true`, `{}`, `[]`, `null`, `""`, `-1`, `1.5`, `"a\u0000b"`} {
		var id sourceID
		if json.Unmarshal([]byte(raw), &id) == nil {
			t.Fatal("malformed identity accepted")
		}
	}
	for _, raw := range []string{`<html></html>`, `<meta name="api_token" content="">`, `<meta name="api_token" content="synthetic"><meta name="api_token" content="synthetic">`} {
		if _, err := bootstrapToken([]byte(raw)); err == nil {
			t.Fatal("ambiguous bootstrap accepted")
		}
	}
}
