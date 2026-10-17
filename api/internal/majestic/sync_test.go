package majestic

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

type fixtureGetter struct {
	mu     sync.Mutex
	bodies map[string][]byte
	calls  map[string]int
	fail   string
}

func (g *fixtureGetter) Program(_ context.Context, site string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls[site]++
	if site == g.fail {
		return nil, errors.New("synthetic transport failure")
	}
	return g.bodies[site], nil
}
func (g *fixtureGetter) RequestCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, count := range g.calls {
		n += count
	}
	return n
}
func fixtures(t *testing.T) *fixtureGetter {
	t.Helper()
	g := &fixtureGetter{bodies: map[string][]byte{}, calls: map[string]int{}}
	for _, site := range sites {
		p := program{Name: "Source cinema", Address: "1 rue", City: "Ville", ZipCode: "60000", Events: []event{{ID: "ABCDE", Title: "Film", Duration: 100, BillURL: "https://images.monnaie-services.com/movie_poster/120/FRABCDE/ABCDEFG1.webp", Sessions: []session{{ID: "emsx" + site[3:] + "00000001", Date: "202610162330", HallName: "Salle source", Version: "VO", Features: []string{"subtitle", "audio_atmos"}, FirstPartDuration: 10, BookingURL: "https://www.ticketingcine.com/" + site + "/FRABCDE#showsession?id=emsx" + site[3:] + "00000001&ps=ticketingcine"}}}}}
		setProgram(t, g, site, p)
	}
	return g
}
func setProgram(t *testing.T, g *fixtureGetter, site string, p program) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"schedule": p}})
	if err != nil {
		t.Fatal(err)
	}
	g.bodies[site] = body
}
func runFixture(t *testing.T, g *fixtureGetter) (schedule.Dataset, SyncSummary, error) {
	t.Helper()
	return Sync(t.Context(), g, SyncOptions{From: "2026-10-16", Now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)})
}

func TestMajesticSevenSitesCalendarAndSourceMapping(t *testing.T) {
	g := fixtures(t)
	p, err := parseProgram(g.bodies[sites[0]], sites[0])
	if err != nil {
		t.Fatal(err)
	}
	p.Events[0].Sessions = append(p.Events[0].Sessions, session{ID: "emsx070000000002", Date: "202701010030", HallName: "ICE source", Version: "VF", Features: []string{"video_3d"}})
	p.Events[0].Sessions = append(p.Events[0].Sessions, session{ID: "emsx070000000003", Date: "202610160600", HallName: "Morning source", Version: "VF"})
	setProgram(t, g, sites[0], p)
	d, summary, err := runFixture(t, g)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Cinemas != 7 || summary.Requests != 7 || summary.Showtimes != 9 || summary.Movies != 1 || d.Window.Through != "2027-01-01" {
		t.Fatalf("counts/window %+v %+v", summary, d.Window)
	}
	for _, site := range sites {
		if g.calls[site] != 1 {
			t.Fatal("missing or repeated logical request", site)
		}
	}
	r := d.Showtimes[0]
	if r.EndTime.Sub(r.StartTime) != 110*time.Minute || r.EndTime.In(r.StartTime.Location()).Day() != 17 || r.FirstPartDurationMinutes != 10 || r.Language != schedule.LanguageVOSTFR || r.ProviderVersion != "VOSTF" || r.Format != schedule.FormatDolby || r.Room != "Salle source" || !strings.Contains(r.Movie.PosterURL, "/600/") {
		t.Fatalf("source mapping %+v", r)
	}
	if !reflect.DeepEqual(d.Theaters[0].AvailableDates, []string{"2026-10-16", "2027-01-01"}) || len(d.Coverage) != 0 {
		t.Fatal("invented/shifted dates or coverage")
	}
	alreadyStarted := false
	for _, r := range d.Showtimes {
		if r.ProviderShowingID == "emsx070000000003" {
			alreadyStarted = true
		}
	}
	if !alreadyStarted {
		t.Fatal("already-started session on included date dropped")
	}
	second, _, err := runFixture(t, g)
	if err != nil || !reflect.DeepEqual(d, second) {
		t.Fatal("non-deterministic source mapping", err)
	}
}

func TestMajesticLocalMissingPosterAndExplicitEmpty(t *testing.T) {
	g := fixtures(t)
	site := sites[0]
	p, err := parseProgram(g.bodies[site], site)
	if err != nil {
		t.Fatal(err)
	}
	e := &p.Events[0]
	e.ID = "emsx0700HC12"
	e.Duration = 0
	e.BillURL = "https://images.monnaie-services.com/ems_spectacle/120/0700/HC12.jpg?ts=123"
	e.Sessions[0].BookingURL = "https://www.ticketingcine.com?EMS0700#showsession?id=" + e.Sessions[0].ID
	setProgram(t, g, site, p)
	empty := program{Name: "Not Majestic", Address: "2 rue", City: "Ville", ZipCode: "59000", Events: []event{}}
	setProgram(t, g, sites[1], empty)
	d, _, err := runFixture(t, g)
	if err != nil {
		t.Fatal(err)
	}
	r := d.Showtimes[0]
	if r.Movie.ProviderID != "EMS0700-emsx0700HC12" || r.Movie.PosterURL != e.BillURL || !r.EndTime.Equal(r.StartTime) || r.FirstPartDurationMinutes != 10 || len(d.Theaters[1].AvailableDates) != 0 || d.Theaters[1].Name != "Not Majestic" {
		t.Fatal("local/empty source fidelity")
	}
	e.BillURL = ""
	setProgram(t, g, site, p)
	d, _, err = runFixture(t, g)
	if err != nil || d.Showtimes[0].Movie.PosterURL != "" {
		t.Fatal("missing poster synthesized", err)
	}
}

func TestMajesticInvalidCinemaAbortsWholeDataset(t *testing.T) {
	for _, mutation := range []struct {
		name string
		edit func(*program)
	}{
		{"DST fold", func(p *program) { p.Events[0].Sessions[0].Date = "202610250230" }},
		{"DST gap", func(p *program) { p.Events[0].Sessions[0].Date = "202703280230" }},
		{"cross site", func(p *program) { p.Events[0].Sessions[0].ID = "emsx065400000001" }},
		{"cross film", func(p *program) {
			p.Events[0].Sessions[0].BookingURL = strings.ReplaceAll(p.Events[0].Sessions[0].BookingURL, "FRABCDE", "FRZZZZZ")
		}},
		{"bad poster", func(p *program) { p.Events[0].BillURL = "https://evil.example/poster" }},
		{"missing census", func(p *program) { p.Events = nil }},
		{"duplicate session", func(p *program) { p.Events[0].Sessions = append(p.Events[0].Sessions, p.Events[0].Sessions[0]) }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			g := fixtures(t)
			p, err := parseProgram(g.bodies[sites[0]], sites[0])
			if err != nil {
				t.Fatal(err)
			}
			mutation.edit(&p)
			setProgram(t, g, sites[0], p)
			d, _, err := runFixture(t, g)
			if err == nil || len(d.Theaters) != 0 || len(d.Showtimes) != 0 {
				t.Fatal("partial/invalid snapshot returned")
			}
		})
	}
	g := fixtures(t)
	g.fail = sites[3]
	if d, _, err := runFixture(t, g); err == nil || len(d.Theaters) != 0 {
		t.Fatal("transport partial accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := Sync(ctx, fixtures(t), SyncOptions{From: "2026-10-16", Now: time.Now()}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}

func TestMajesticLanguageFormatPriority(t *testing.T) {
	for _, test := range []struct {
		features      []string
		formats, room string
		want          schedule.Format
	}{
		{[]string{"video_imax", "video_3d", "video_immersif", "audio_atmos"}, "", "ICE", schedule.FormatIMAX},
		{[]string{"video_immersif", "audio_atmos"}, "3D", "ICE", schedule.Format3D},
		{[]string{"audio_atmos"}, "", "ICE", schedule.FormatICE},
		{[]string{"video_motion", "4dx"}, "", "4DX DOLBY", schedule.Format2D},
	} {
		_, format, _, err := attributes(session{Version: "VF", Features: test.features, Formats: test.formats, HallName: test.room})
		if err != nil || format != test.want {
			t.Fatal("format priority", format, err)
		}
	}
	for _, version := range []string{"VF", "VO"} {
		language, _, source, err := attributes(session{Version: version, Features: []string{"subtitle"}})
		if err != nil || version == "VF" && (language != schedule.LanguageVFSTF || source != "VFSTF") || version == "VO" && (language != schedule.LanguageVOSTFR || source != "VOSTF") {
			t.Fatal("subtitle vocabulary")
		}
	}
}

func TestMajesticSevenExplicitEmptyProgramsPublish(t *testing.T) {
	g := fixtures(t)
	for _, site := range sites {
		p, err := parseProgram(g.bodies[site], site)
		if err != nil {
			t.Fatal(err)
		}
		p.Events = []event{}
		setProgram(t, g, site, p)
	}
	d, summary, err := runFixture(t, g)
	if err != nil || summary.Requests != 7 || len(d.Theaters) != 7 || len(d.Showtimes) != 0 {
		t.Fatal("explicit seven-site empty census rejected", err)
	}
	if err := schedule.ValidateDataset(d, true); err != nil {
		t.Fatal("empty census cannot reload", err)
	}
	d.Theaters = d.Theaters[:6]
	if schedule.ValidateDataset(d, true) == nil {
		t.Fatal("incomplete empty census accepted")
	}
}
