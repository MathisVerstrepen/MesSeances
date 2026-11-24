package ticketingcine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type fixtureGetter struct {
	mu     sync.Mutex
	bodies map[string][]byte
	calls  int
	fail   string
}

func (g *fixtureGetter) Program(_ context.Context, site string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	if site == g.fail {
		return nil, errors.New("synthetic transport failure")
	}
	return g.bodies[site], nil
}

func (g *fixtureGetter) RequestCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

func setProgram(t *testing.T, g *fixtureGetter, site string, p Program) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"schedule": p}})
	if err != nil {
		t.Fatal(err)
	}
	g.bodies[site] = body
}

// siteProgram has a national film and a local event; sessions cover a bound
// source booking, a foreign booking, an after-midnight start and a past date.
func siteProgram(site string) Program {
	n := SiteNumber(site)
	return Program{Name: "Cinéma " + site, Address: "1 rue du Cinéma", City: "Ville", ZipCode: "59000", Events: []Event{
		{ID: "ABCDE", Title: " Film  national ", Duration: 100, BillURL: "https://images.monnaie-services.com/movie_poster/120/FRABCDE/ABCDEFG1.webp", Sessions: []Session{
			{ID: "emsx" + n + "00000001", Date: "202610162030", HallName: "Salle 1", Version: "VOST", FirstPartDuration: 10, BookingURL: "https://www.ticketingcine.com/" + site + "/FRABCDE#showsession?id=emsx" + n + "00000001&ps=ticketingcine"},
			{ID: "emsx" + n + "00000002", Date: "202610170030", HallName: "Salle ICE", Version: "VF", BookingURL: "https://cinema.example/reserver"},
			{ID: "emsx" + n + "00000003", Date: "202610151800", HallName: "Salle 1", Version: "VF"},
		}},
		{ID: "emsx" + n + "HC42", Title: "Ciné-débat", Sessions: []Session{
			{ID: "emsx" + n + "00000004", Date: "202610181400", HallName: "", Version: "VF", Features: []string{"video_3d"}},
		}},
	}}
}

func fixtures(t *testing.T, sites ...string) *fixtureGetter {
	t.Helper()
	g := &fixtureGetter{bodies: map[string][]byte{}}
	for _, site := range sites {
		setProgram(t, g, site, siteProgram(site))
	}
	return g
}

func sources(sites ...string) []Source {
	result := make([]Source, len(sites))
	for i, site := range sites {
		result[i] = Source{SiteID: site}
	}
	return result
}

func run(t *testing.T, g *fixtureGetter, sites ...string) (schedule.Dataset, SyncSummary, error) {
	t.Helper()
	return Sync(t.Context(), g, SyncOptions{From: "2026-10-16", Now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC), Sources: sources(sites...)})
}

func TestSyncBuildsValidatedSnapshotFromSources(t *testing.T) {
	g := fixtures(t, "EMS1234", "EMS0042")
	data, summary, err := run(t, g, "EMS1234", "EMS0042")
	if err != nil {
		t.Fatal(err)
	}
	if data.Provider != schedule.ProviderTicketingcine || len(data.Theaters) != 2 || data.Theaters[0].ID != "ticketingcine-EMS1234" || data.Theaters[1].ID != "ticketingcine-EMS0042" {
		t.Fatalf("theaters=%+v", data.Theaters)
	}
	if summary.Cinemas != 2 || summary.Movies != 3 || summary.Showtimes != 6 || summary.Requests != 2 || len(data.Showtimes) != 6 {
		t.Fatalf("summary=%+v showtimes=%d", summary, len(data.Showtimes))
	}
	if data.Window.From != "2026-10-16" || data.Window.Through != "2026-10-18" {
		t.Fatalf("window=%+v", data.Window)
	}
	byID := map[string]schedule.ShowtimeRecord{}
	for _, s := range data.Showtimes {
		byID[s.ProviderShowingID] = s
	}
	bound := byID["emsx123400000001"]
	if bound.BookingURL != "https://www.ticketingcine.com/EMS1234/FRABCDE#showsession?id=emsx123400000001&ps=ticketingcine" || bound.Language != schedule.LanguageVOSTFR || bound.Movie.ProviderID != "ABCDE" || bound.Movie.Title != "Film national" || !bound.EndTime.Equal(bound.StartTime.Add(110*time.Minute)) {
		t.Fatalf("bound session=%+v", bound)
	}
	overnight := byID["emsx123400000002"]
	if overnight.ServiceDate != "2026-10-16" || overnight.Format != schedule.FormatICE || overnight.BookingURL != "https://www.ticketingcine.com?EMS1234#showsession?id=emsx123400000002" {
		t.Fatalf("overnight session=%+v", overnight)
	}
	if _, ok := byID["emsx123400000003"]; ok {
		t.Fatal("past session published")
	}
	local := byID["emsx004200000004"]
	if local.Movie.ProviderID != "EMS0042-emsx0042HC42" || local.Format != schedule.Format3D || local.Room != "" || !local.EndTime.Equal(local.StartTime) {
		t.Fatalf("local event session=%+v", local)
	}
	if !strings.HasPrefix(local.Movie.Slug, "ticketingcine-film-") || local.ID != "ticketingcine-showing-emsx004200000004" {
		t.Fatalf("local identities=%+v", local)
	}
}

func TestSyncRequiresSourcesAndValidatesThem(t *testing.T) {
	g := fixtures(t, "EMS1234")
	if _, _, err := run(t, g); !errors.Is(err, ErrNoSources) {
		t.Fatalf("no sources err=%v", err)
	}
	for _, bad := range [][]string{{"EMS12"}, {"ems1234"}, {"EMS1234", "EMS1234"}} {
		if _, _, err := run(t, g, bad...); !errors.Is(err, ErrShape) {
			t.Fatalf("sources=%v err=%v", bad, err)
		}
	}
	if g.calls != 0 {
		t.Fatal("invalid sources reached the network")
	}
}

func TestSyncFailsAtomically(t *testing.T) {
	g := fixtures(t, "EMS1234", "EMS0042")
	g.fail = "EMS0042"
	if data, _, err := run(t, g, "EMS1234", "EMS0042"); err == nil || len(data.Showtimes) != 0 {
		t.Fatal("transport failure published a partial snapshot")
	}

	g = fixtures(t, "EMS1234", "EMS0042")
	p := siteProgram("EMS0042")
	p.Address = " "
	setProgram(t, g, "EMS0042", p)
	if _, _, err := run(t, g, "EMS1234", "EMS0042"); !errors.Is(err, ErrShape) || !strings.Contains(err.Error(), "EMS0042") {
		t.Fatalf("missing address err=%v", err)
	}

	g = fixtures(t, "EMS1234")
	p = siteProgram("EMS1234")
	p.Events[1].Sessions[0].ID = p.Events[0].Sessions[0].ID
	setProgram(t, g, "EMS1234", p)
	if _, _, err := run(t, g, "EMS1234"); !errors.Is(err, schedule.ErrDatasetValidation) {
		t.Fatalf("duplicate session err=%v", err)
	}

	g = fixtures(t, "EMS1234")
	p = siteProgram("EMS1234")
	p.Events[0].Sessions[0].ID = "emsx999900000001"
	setProgram(t, g, "EMS1234", p)
	if _, _, err := run(t, g, "EMS1234"); !errors.Is(err, ErrShape) {
		t.Fatalf("foreign session err=%v", err)
	}

	g = fixtures(t, "EMS1234")
	g.bodies["EMS1234"] = []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":1}}`)
	if _, _, err := run(t, g, "EMS1234"); !errors.Is(err, ErrEnvelope) {
		t.Fatalf("envelope err=%v", err)
	}
}

func TestAttributes(t *testing.T) {
	for _, test := range []struct {
		session  Session
		language schedule.Language
		format   schedule.Format
		version  string
	}{
		{Session{Version: "VF"}, schedule.LanguageVF, schedule.Format2D, "VF"},
		{Session{Version: "VO", Features: []string{"subtitle"}}, schedule.LanguageVOSTFR, schedule.Format2D, "VOSTF"},
		{Session{Version: "VF", Formats: "ST"}, schedule.LanguageVFSTF, schedule.Format2D, "VFSTF"},
		{Session{Version: "VOST", Features: []string{"video_imax"}}, schedule.LanguageVOSTFR, schedule.FormatIMAX, "VOST"},
		{Session{Version: "VOF", Features: []string{"video_motion"}}, schedule.LanguageVO, schedule.Format4DX, "VOF"},
		{Session{Version: "VF", Formats: "ATMOS, UNKNOWN"}, schedule.LanguageVF, schedule.FormatDolby, "VF"},
	} {
		language, format, version, err := Attributes(test.session)
		if err != nil || language != test.language || format != test.format || version != test.version {
			t.Fatalf("session=%+v got=%s/%s/%s err=%v", test.session, language, format, version, err)
		}
	}
	if _, _, _, err := Attributes(Session{Version: "VFQ"}); !errors.Is(err, ErrShape) {
		t.Fatal("unknown version accepted")
	}
}

func TestClientRejectsInvalidSiteBeforeNetwork(t *testing.T) {
	c := &Client{}
	_, err := c.Program(t.Context(), "EMS12")
	var requestErr *syncproxy.RequestError
	if !errors.As(err, &requestErr) || requestErr.Kind != syncproxy.FailureInvalidURL || requestErr.Operation.Phase != syncproxy.PhaseProgram {
		t.Fatalf("err=%v", err)
	}
	if _, err := NewClient(ClientConfig{}); err == nil {
		t.Fatal("client without proxies accepted")
	}
}
