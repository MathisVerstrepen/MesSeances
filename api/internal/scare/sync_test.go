package scare

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type fixtureGetter struct {
	pages   [][]byte
	queries []Query
	fail    bool
}

func (g *fixtureGetter) Lines(_ context.Context, q Query) ([]byte, error) {
	g.queries = append(g.queries, q)
	if g.fail {
		return nil, errors.New("synthetic transport failure")
	}
	return g.pages[len(g.queries)-1], nil
}

func (g *fixtureGetter) RequestCount() int { return len(g.queries) }

func row(cinema, show, film, title, start string) map[string]any {
	return map[string]any{
		"cineid": json.Number(cinema), "cinenom": "Le  Studio", "cineadresse": "2 rue X", "cinecp": "6000", "cineville": "Nice",
		"_coords.lat": 43.7, "_coords.lon": 7.26, "filmid": film, "filmtitle": title, "filmduration": 6180,
		"filmstoryline": "Une histoire.", "filmgenre": "Drame, Comédie", "showid": show, "showstart": start,
		"showend": "", "filmversion": "VERSION_ORIGINAL", "filmaudio": "7.1", "showurl": "https://studio.cine.boutique/media/" + film + "?showId=" + show,
	}
}

func page(t *testing.T, next string, rows ...map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"total": len(rows), "next": next, "results": rows})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func run(g *fixtureGetter, cinemas ...string) (schedule.Dataset, SyncSummary, error) {
	sources := make([]Source, len(cinemas))
	for i, c := range cinemas {
		sources[i] = Source{CinemaID: c}
	}
	return Sync(context.Background(), g, SyncOptions{From: "2026-11-25", Now: time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC), Sources: sources})
}

func TestSyncPaginatesAndMapsRows(t *testing.T) {
	next := "https://datacinesindes.fr/data-fair/api/v1/datasets/lyxgxtgaz5zfv0zu0fa11xq8/lines?size=10000&after=1854%2C1795628700000%2C40%2C190619"
	silent := row("1854", "13", "", "Nosferatu", "2026-11-26T20:00:00+01:00")
	silent["filmversion"], silent["filmduration"] = "VERSION_MUET", 94
	local := row("1854", "12", "F2", "Film local", "2026-11-27T00:30:00+0100")
	local["filmversion"], local["filmaudio"], local["showend"] = "VERSION_LOCAL", "ST,7.1", "2026-11-27T02:10:00+0100"
	g := &fixtureGetter{pages: [][]byte{
		page(t, next, row("1854", "11", "F1", "  Grand  film ", "2026-11-25T14:00:00+0100"), local),
		page(t, "", silent, row("1000035", "a99", "F1", "Autre", "2026-11-25T18:00:00+01:00")),
	}}
	data, summary, err := run(g, "1854", "1000035")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.queries) != 2 || g.queries[1].After != "1854,1795628700000,40,190619" || !strings.Contains(g.queries[0].URL(), "cineid_in=1854%2C1000035") || !strings.Contains(g.queries[0].URL(), "showstart_gte=2026-11-25") {
		t.Fatalf("queries=%+v", g.queries)
	}
	if summary.Cinemas != 2 || summary.Showtimes != 4 || summary.Skipped != 0 || summary.Requests != 2 || len(data.Theaters) != 2 {
		t.Fatalf("summary=%+v", summary)
	}
	theater := data.Theaters[0]
	if theater.ID != "scare-1854" || theater.Name != "Le Studio" || theater.PostalCode != "06000" || theater.Latitude == nil {
		t.Fatalf("theater=%+v", theater)
	}
	byID := map[string]schedule.ShowtimeRecord{}
	for _, s := range data.Showtimes {
		byID[s.ProviderShowingID] = s
	}
	first := byID["1854-11"]
	if first.Movie.ProviderID != "1854-F1" || first.Movie.Title != "Grand film" || first.Movie.RuntimeMinutes != 103 || first.Language != schedule.LanguageVOSTFR || first.ProviderVersion != "VERSION_ORIGINAL" || !first.EndTime.Equal(first.StartTime.Add(103*time.Minute)) || len(first.Movie.Genres) != 2 || first.BookingURL == "" {
		t.Fatalf("first=%+v", first)
	}
	overnight := byID["1854-12"]
	if overnight.ServiceDate != "2026-11-26" || overnight.Language != schedule.LanguageVFSTF || !overnight.EndTime.Equal(overnight.StartTime.Add(100*time.Minute)) {
		t.Fatalf("overnight=%+v", overnight)
	}
	mute := byID["1854-13"]
	if mute.Language != "" || mute.ProviderVersion != "VERSION_MUET" || !strings.HasPrefix(mute.Movie.ProviderID, "1854-t") || mute.Movie.RuntimeMinutes != 94 {
		t.Fatalf("silent=%+v", mute)
	}
	if other := byID["1000035-a99"]; other.Movie.ProviderID != "1000035-F1" || other.TheaterID != "scare-1000035" {
		t.Fatalf("film ids must be scoped by cinema: %+v", other)
	}
}

// A film completed by a later row (runtime) must give every session the same
// record, or publication rejects the snapshot.
func TestSyncBindsSessionsToTheMergedFilm(t *testing.T) {
	first := row("1854", "1", "F1", "Film", "2026-11-25T14:00:00+01:00")
	first["filmduration"] = 0
	second := row("1854", "2", "F1", "Film", "2026-11-25T18:00:00+01:00")
	g := &fixtureGetter{pages: [][]byte{page(t, "", first, second)}}
	data, _, err := run(g, "1854")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schedule.PreparePublication(data); err != nil {
		t.Fatalf("publication: %v", err)
	}
	for _, s := range data.Showtimes {
		if s.Movie.RuntimeMinutes != 103 || !s.EndTime.Equal(s.StartTime.Add(103*time.Minute)) {
			t.Fatalf("session=%+v", s)
		}
	}
}

func TestSyncSkipsUnmappableRows(t *testing.T) {
	bad := []map[string]any{
		row("1854", "", "F1", "Sans séance", "2026-11-25T14:00:00+01:00"),
		row("1854", "2", "F1", " ", "2026-11-25T14:00:00+01:00"),
		row("1854", "3", "F1", "Sans date", ""),
		row("1854", "1", "F1", "Doublon", "2026-11-25T16:00:00+01:00"),
	}
	unknown := row("1854", "4", "F1", "Version", "2026-11-25T18:00:00+01:00")
	unknown["filmversion"] = "VFQ"
	badURL := row("1854", "5", "F9", "Lien", "2026-11-25T20:00:00+01:00")
	badURL["showurl"] = "https://user@evil.example/x"
	rows := append([]map[string]any{row("1854", "1", "F1", "Film", "2026-11-25T14:00:00+01:00")}, bad...)
	rows = append(rows, unknown, badURL)
	g := &fixtureGetter{pages: [][]byte{page(t, "", rows...)}}
	data, summary, err := run(g, "1854", "2000001")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Showtimes != 2 || summary.Skipped != 5 || summary.EmptyCinemas != 1 || summary.Cinemas != 1 {
		t.Fatalf("summary=%+v", summary)
	}
	for _, s := range data.Showtimes {
		if s.ProviderShowingID == "1854-5" && s.BookingURL != "" {
			t.Fatal("unsafe booking URL kept")
		}
	}
}

func TestSyncRejectsInvalidSourcesPagesAndTransport(t *testing.T) {
	g := &fixtureGetter{}
	if _, _, err := run(g); !errors.Is(err, ErrNoSources) {
		t.Fatalf("no sources err=%v", err)
	}
	for _, bad := range [][]string{{"0"}, {"12a"}, {"1854", "1854"}} {
		if _, _, err := run(g, bad...); !errors.Is(err, ErrShape) {
			t.Fatalf("sources=%v err=%v", bad, err)
		}
	}
	if len(g.queries) != 0 {
		t.Fatal("invalid sources reached the network")
	}
	for _, body := range [][]byte{
		[]byte(`{"total":1}`),
		[]byte(`not json`),
		page(t, "https://evil.example/data-fair/api/v1/datasets/x/lines?after=1", row("1854", "1", "F1", "Film", "2026-11-25T14:00:00+01:00")),
		page(t, "", row("999", "1", "F1", "Film", "2026-11-25T14:00:00+01:00")),
	} {
		g = &fixtureGetter{pages: [][]byte{body}}
		if _, _, err := run(g, "1854"); !errors.Is(err, ErrShape) {
			t.Fatalf("body=%s err=%v", body, err)
		}
	}
	g = &fixtureGetter{fail: true}
	if data, _, err := run(g, "1854"); err == nil || len(data.Showtimes) != 0 {
		t.Fatal("transport failure published a snapshot")
	}
}

func TestRuntimeAndAttributes(t *testing.T) {
	for raw, want := range map[string]int{"": 0, "0": 0, "-3": 0, "96": 96, "400": 400, "6180": 103, "4860": 81, "x": 0} {
		if got := RuntimeMinutes(json.Number(raw)); got != want {
			t.Fatalf("runtime %q=%d want %d", raw, got, want)
		}
	}
	for _, test := range []struct {
		version, audio string
		language       schedule.Language
		known          bool
	}{
		{"VF", "", schedule.LanguageVF, true}, {"VO", "", schedule.LanguageVO, true}, {"VO", "ST", schedule.LanguageVOSTFR, true},
		{"VOST", "ST,7.1", schedule.LanguageVOSTFR, true}, {"VERSION_ORIGINAL_LOCAL", "5.1", schedule.LanguageVF, true},
		{"VERSION_LOCAL", "ST", schedule.LanguageVFSTF, true}, {"", "", "", true}, {"VERSION_MUET", "", "", true}, {"VFQ", "", "", false},
	} {
		language, _, known := Attributes(Row{Version: test.version, Audio: test.audio})
		if language != test.language || known != test.known {
			t.Fatalf("%+v got %s %v", test, language, known)
		}
	}
}

func TestClientRejectsInvalidQueryBeforeNetwork(t *testing.T) {
	c := &Client{}
	for _, q := range []Query{{From: "2026-11-25"}, {Cinemas: []string{"x"}, From: "2026-11-25"}, {Cinemas: []string{"1"}, From: "25/11"}, {Cinemas: []string{"1"}, From: "2026-11-25", After: "a&b=c"}} {
		_, err := c.Lines(t.Context(), q)
		var requestErr *syncproxy.RequestError
		if !errors.As(err, &requestErr) || requestErr.Kind != syncproxy.FailureInvalidURL {
			t.Fatalf("query=%+v err=%v", q, err)
		}
	}
	if _, err := NewClient(ClientConfig{}); err == nil {
		t.Fatal("client without proxies accepted")
	}
}

func TestGenresDropsNullPlaceholder(t *testing.T) {
	for raw, want := range map[string][]string{
		"null":                nil,
		" NULL ":              nil,
		"Drame, null,Comédie": {"Drame", "Comédie"},
		"Nullité":             {"Nullité"},
		"":                    nil,
	} {
		if got := Genres(raw); !slices.Equal(got, want) {
			t.Fatalf("Genres(%q)=%q want %q", raw, got, want)
		}
	}
}
