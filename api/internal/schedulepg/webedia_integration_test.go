package schedulepg

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
	"messeances/api/internal/webedia"
)

type webediaFixtureGetter map[string][]byte

func (g webediaFixtureGetter) Get(_ context.Context, _ syncproxy.Operation, raw string) ([]byte, error) {
	u, _ := url.Parse(raw)
	return g[u.Path], nil
}
func (webediaFixtureGetter) RequestCount() int { return 4 }

func webediaTestDataset(t *testing.T) Dataset {
	t.Helper()
	marshal := func(v any) []byte {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	show := func(id, start string, tags ...string) map[string]any {
		return map[string]any{"id": id, "startsAt": start, "tags": tags, "screen": map[string]any{"name": "Salle 1"},
			"data": map[string]any{"ticketing": []any{map[string]any{"provider": "default", "type": "DESKTOP", "urls": []string{"https://tickets.allocine.fr/cine/reserver/r/" + id}}}}}
	}
	g := webediaFixtureGetter{
		"/page-data/sq/d/2506275789.json": marshal(map[string]any{"data": map[string]any{"allTheater": map[string]any{"nodes": []any{map[string]any{"id": "P5385", "name": "Ciné Pôle Sud", "timeZone": schedule.Timezone,
			"practicalInfo": map[string]any{"location": map[string]any{"address": "Route de Clisson", "city": "Basse-Goulaine", "zip": "44115"}}}}}}}),
		"/api/gatsby-source-boxofficeapi/scheduledMovies": marshal(map[string]any{"movieIds": map[string]any{"titleAsc": []string{"204864", "c42"}, "releaseAsc": []string{"204864", "c42"}}, "scheduledDays": map[string]any{"204864": []string{"2026-10-16"}, "c42": []string{"2026-10-17"}}}),
		"/api/gatsby-source-boxofficeapi/movies":          marshal([]any{map[string]any{"id": "204864", "title": "Le Chat chapeauté", "runtime": 6840, "poster": "https://all.web.img.acsta.net/img/f.jpg"}, map[string]any{"id": "c42", "title": "Ciné-débat"}}),
		"/api/gatsby-source-boxofficeapi/schedule": marshal(map[string]any{"P5385": map[string]any{"schedule": map[string]any{
			"204864": map[string]any{"2026-10-16": []any{show("a", "2026-10-16T20:30:00", "Localization.Version.Original")}},
			"c42":    map[string]any{"2026-10-17": []any{show("b", "2026-10-17T14:00:00", "Localization.Language.French")}},
		}}}),
	}
	data, _, err := webedia.Sync(t.Context(), g, webedia.SyncOptions{From: "2026-10-16", Now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC), Sources: []webedia.Source{{TheaterID: "P5385", Origin: "https://www.cinepolesud.fr/"}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The Webedia provider, registered by migration 087, publishes, persists and
// reloads like the built-in providers.
func TestWebediaRoundTripIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	d := webediaTestDataset(t)
	if _, err := store.Replace(t.Context(), []Dataset{d}); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Showtimes) != 2 || len(loaded.Theaters) != 1 || loaded.Theaters[0].ID != "webedia-P5385" {
		t.Fatalf("loaded=%+v", loaded)
	}
	for i, s := range loaded.Showtimes {
		want := d.Showtimes[i]
		if s.ID != want.ID || s.BookingURL != want.BookingURL || !s.StartTime.Equal(want.StartTime) || !s.EndTime.Equal(want.EndTime) || s.Language != want.Language || s.Room != want.Room || s.Movie.ProviderID != want.Movie.ProviderID || s.Movie.PublicMovieID <= 0 {
			t.Fatalf("showtime %d=%+v want=%+v", i, s, want)
		}
	}
	historyCount(t, pool, `SELECT count(*) FROM screening_history_showtimes WHERE provider='webedia'`, 2)
	again, _, err := store.Load(t.Context())
	if err != nil || !reflect.DeepEqual(loaded, again) {
		t.Fatal("reload changed", err)
	}
	if schedule.ValidateDataset(d, true) != nil {
		t.Fatal("fixture invalid")
	}
}
