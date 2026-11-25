package webedia

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/boxoffice"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type fixtureGetter struct {
	bodies map[string][]byte // keyed by host + path
	urls   []string
	fail   string
}

func (g *fixtureGetter) Get(_ context.Context, _ syncproxy.Operation, raw string) ([]byte, error) {
	g.urls = append(g.urls, raw)
	u, err := url.Parse(raw)
	if err != nil || !allowedURL(u) {
		return nil, errors.New("disallowed URL")
	}
	if g.fail != "" && strings.Contains(raw, g.fail) {
		return nil, errors.New("synthetic transport failure")
	}
	return g.bodies[u.Host+u.Path], nil
}

func (g *fixtureGetter) RequestCount() int { return len(g.urls) }

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func session(id, start string, tags ...string) map[string]any {
	return map[string]any{"id": id, "startsAt": start, "tags": tags, "screen": map[string]any{"name": "Salle 2"},
		"data": map[string]any{"ticketing": []any{
			map[string]any{"provider": "relay", "type": "DESKTOP", "urls": []string{"https://relay.example/x"}},
			map[string]any{"provider": "default", "type": "DESKTOP", "urls": []string{"https://tickets.allocine.fr/cine/reserver/r/" + id}},
		}}}
}

func siteFixtures(t *testing.T, host, theater string) map[string][]byte {
	t.Helper()
	catalog := map[string]any{"data": map[string]any{"allTheater": map[string]any{"nodes": []any{
		map[string]any{"id": "Z9999", "name": "Autre", "timeZone": schedule.Timezone},
		map[string]any{"id": theater, "name": " Ciné  Sud ", "timeZone": schedule.Timezone, "practicalInfo": map[string]any{
			"coordinates": map[string]any{"latitude": 47.1, "longitude": -1.4},
			"location":    map[string]any{"address": "Route de Clisson", "city": "Basse-Goulaine", "zip": "44115"}}},
	}}}}
	program := map[string]any{"movieIds": map[string]any{"titleAsc": []any{"204864", "c42"}, "releaseAsc": []any{"c42", "204864"}},
		"scheduledDays": map[string]any{"204864": []string{"2026-11-25", "2026-11-26"}, "c42": []string{"2026-11-26"}}}
	movies := []any{
		map[string]any{"id": "204864", "title": "Le Chat chapeauté", "runtime": 6840, "poster": "https://all.web.img.acsta.net/img/f6/50/f.jpg", "genres": "Animation, Aventure", "releaseDate": "2026-11-19T00:00:00Z", "synopsis": "Un chat."},
		map[string]any{"id": "c42", "title": "Ciné-débat", "runtime": nil, "poster": "https://evil.example/p.jpg", "genres": []string{"Débat"}},
	}
	sched := map[string]any{theater: map[string]any{"schedule": map[string]any{
		"204864": map[string]any{
			"2026-11-25": []any{session("a", "2026-11-25T20:30:00", "Localization.Version.Original", "Format.Projection.3d"), session("a2", "2026-11-25T14:00:00", "Unknown.Tag")},
			"2026-11-26": []any{session("b", "2026-11-27T00:15:00", "Localization.Language.French")},
		},
		"c42":    map[string]any{"2026-11-26": []any{session("c", "2026-11-26T18:00:00", "Localization.Language.French", "Showtime.Accessibility.OpenCaption")}},
		"999999": map[string]any{"2026-11-26": []any{session("d", "2026-11-26T18:00:00", "Localization.Language.French")}},
	}}}
	return map[string][]byte{
		host + boxoffice.CatalogPath: mustJSON(t, catalog),
		host + programPath:           mustJSON(t, program),
		host + moviesPath:            mustJSON(t, movies),
		host + schedulePath:          mustJSON(t, sched),
	}
}

func run(g *fixtureGetter, sources ...Source) (schedule.Dataset, SyncSummary, error) {
	return Sync(context.Background(), g, SyncOptions{From: "2026-11-25", Now: time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC), Sources: sources})
}

func TestSyncMapsConfiguredSites(t *testing.T) {
	bodies := siteFixtures(t, "www.cine-sud.fr", "P5385")
	for k, v := range siteFixtures(t, "www.autre.fr", "W0001") {
		bodies[k] = v
	}
	g := &fixtureGetter{bodies: bodies}
	data, summary, err := run(g, Source{TheaterID: "P5385", Origin: "https://www.cine-sud.fr/"}, Source{TheaterID: "W0001", Origin: "https://www.autre.fr/"})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Cinemas != 2 || summary.Showtimes != 6 || summary.Skipped != 4 || summary.Requests != 8 || summary.Movies != 3 {
		t.Fatalf("summary=%+v", summary)
	}
	scheduleURL := ""
	for _, u := range g.urls {
		if strings.Contains(u, "www.cine-sud.fr"+schedulePath) {
			scheduleURL = u
		}
	}
	if !strings.Contains(scheduleURL, "theaters=%7B%22id%22%3A%22P5385%22") || !strings.Contains(scheduleURL, "to=2026-11-27T03%3A00%3A00") {
		t.Fatalf("schedule url=%s", scheduleURL)
	}
	th := data.Theaters[0]
	if th.ID != "webedia-P5385" || th.Name != "Ciné Sud" || th.PostalCode != "44115" || th.Latitude == nil || len(th.AvailableDates) != 2 {
		t.Fatalf("theater=%+v", th)
	}
	byMovie := map[string][]schedule.ShowtimeRecord{}
	for _, s := range data.Showtimes {
		byMovie[s.Movie.ProviderID] = append(byMovie[s.Movie.ProviderID], s)
	}
	national := byMovie["204864"]
	if len(national) != 4 || national[0].Movie.RuntimeMinutes != 114 || national[0].Movie.PosterURL == "" || national[0].Movie.ReleaseDate != "2026-11-19" || len(national[0].Movie.Genres) != 2 {
		t.Fatalf("national=%+v", national)
	}
	for _, s := range national {
		switch {
		case s.StartTime.Hour() == 20:
			if s.Language != schedule.LanguageVOSTFR || s.Format != schedule.Format3D || s.Room != "Salle 2" || !strings.HasPrefix(s.BookingURL, "https://tickets.allocine.fr/") || !s.EndTime.Equal(s.StartTime.Add(114*time.Minute)) {
				t.Fatalf("evening=%+v", s)
			}
		case s.StartTime.Hour() == 0:
			if s.ServiceDate != "2026-11-26" || s.Language != schedule.LanguageVF {
				t.Fatalf("overnight=%+v", s)
			}
		}
	}
	local := byMovie["P5385-c42"]
	if len(local) != 1 || local[0].Language != schedule.LanguageVFSTF || local[0].Movie.PosterURL != "" || !local[0].EndTime.Equal(local[0].StartTime) {
		t.Fatalf("local=%+v", local)
	}
	if len(byMovie["W0001-c42"]) != 1 {
		t.Fatal("site-local films must be scoped by theater")
	}
}

func TestSyncFailsAtomicallyAndValidatesSources(t *testing.T) {
	good := Source{TheaterID: "P5385", Origin: "https://www.cine-sud.fr/"}
	if _, _, err := run(&fixtureGetter{}); !errors.Is(err, ErrNoSources) {
		t.Fatalf("no sources err=%v", err)
	}
	for _, bad := range [][]Source{
		{{TheaterID: "p5385", Origin: good.Origin}},
		{{TheaterID: "P5385", Origin: "http://www.cine-sud.fr/"}},
		{{TheaterID: "P5385", Origin: "https://www.cine-sud.fr/films"}},
		{{TheaterID: "P5385", Origin: "https://127.0.0.1/"}},
		{good, good},
	} {
		g := &fixtureGetter{}
		if _, _, err := run(g, bad...); !errors.Is(err, ErrShape) || len(g.urls) != 0 {
			t.Fatalf("sources=%+v err=%v", bad, err)
		}
	}
	g := &fixtureGetter{bodies: siteFixtures(t, "www.cine-sud.fr", "P5385"), fail: "schedule"}
	if data, _, err := run(g, good); err == nil || len(data.Showtimes) != 0 {
		t.Fatal("transport failure published a snapshot")
	}
	g = &fixtureGetter{bodies: siteFixtures(t, "www.cine-sud.fr", "P5385")}
	if _, _, err := run(g, Source{TheaterID: "P0001", Origin: good.Origin}); !errors.Is(err, ErrShape) {
		t.Fatalf("theater missing from catalog err=%v", err)
	}
	g = &fixtureGetter{bodies: siteFixtures(t, "www.cine-sud.fr", "P5385")}
	g.bodies["www.cine-sud.fr"+schedulePath] = []byte(`{"P0001":{"schedule":{}}}`)
	if _, _, err := run(g, good); !errors.Is(err, ErrShape) {
		t.Fatalf("foreign schedule err=%v", err)
	}
}

func TestClientRejectsForeignURLsBeforeNetwork(t *testing.T) {
	c := &Client{}
	for _, raw := range []string{
		"https://www.cine-sud.fr/api/other",
		"http://www.cine-sud.fr" + boxoffice.CatalogPath,
		"https://www.cine-sud.fr" + boxoffice.CatalogPath + "?x=1",
		"https://www.cine-sud.fr" + programPath,
		"https://user@www.cine-sud.fr" + programPath + "?theaterId=P5385",
	} {
		_, err := c.Get(t.Context(), OperationProgram, raw)
		var requestErr *syncproxy.RequestError
		if !errors.As(err, &requestErr) || requestErr.Kind != syncproxy.FailureInvalidURL {
			t.Fatalf("url=%s err=%v", raw, err)
		}
	}
	if _, err := NewClient(ClientConfig{}); err == nil {
		t.Fatal("client without proxies accepted")
	}
}
