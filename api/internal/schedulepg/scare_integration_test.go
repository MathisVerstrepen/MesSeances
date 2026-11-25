package schedulepg

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"messeances/api/internal/scare"
	"messeances/api/internal/schedule"
)

type scareFixtureGetter []byte

func (g scareFixtureGetter) Lines(context.Context, scare.Query) ([]byte, error) { return g, nil }
func (scareFixtureGetter) RequestCount() int                                    { return 1 }

func scareTestDataset(t *testing.T) Dataset {
	t.Helper()
	row := func(show, film, version, start, url string) map[string]any {
		return map[string]any{"cineid": 1854, "cinenom": "Café des Images", "cineadresse": "4 square du Théâtre", "cinecp": "14200", "cineville": "Hérouville-Saint-Clair",
			"_coords.lat": 49.2, "_coords.lon": -0.33, "filmid": film, "filmtitle": "Film " + film, "filmduration": 5400, "showid": show, "showstart": start, "filmversion": version, "showurl": url}
	}
	body, err := json.Marshal(map[string]any{"total": 3, "results": []any{
		row("11", "F1", "VERSION_ORIGINAL", "2026-10-16T20:30:00+0200", "https://cafedesimages-vad.cotecine.fr/reserver/F1/D1/"),
		row("12", "F2", "VF", "2026-10-17T14:00:00+02:00", ""),
		row("13", "F3", "VERSION_MUET", "2026-10-17T18:00:00+02:00", ""),
	}})
	if err != nil {
		t.Fatal(err)
	}
	data, _, err := scare.Sync(t.Context(), scareFixtureGetter(body), scare.SyncOptions{From: "2026-10-16", Now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC), Sources: []scare.Source{{CinemaID: "1854"}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The SCARE provider, registered by migration 086, publishes, persists and
// reloads like the built-in providers, including bookings on third-party
// platforms and silent films.
func TestScareRoundTripIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	d := scareTestDataset(t)
	if _, err := store.Replace(t.Context(), []Dataset{d}); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Showtimes) != 3 || len(loaded.Theaters) != 1 || loaded.Theaters[0].ID != "scare-1854" {
		t.Fatalf("loaded=%+v", loaded)
	}
	for i, s := range loaded.Showtimes {
		want := d.Showtimes[i]
		if s.ID != want.ID || s.BookingURL != want.BookingURL || !s.StartTime.Equal(want.StartTime) || !s.EndTime.Equal(want.EndTime) || s.Language != want.Language || s.ProviderVersion != want.ProviderVersion || s.Movie.ProviderID != want.Movie.ProviderID || s.Movie.PublicMovieID <= 0 {
			t.Fatalf("showtime %d=%+v want=%+v", i, s, want)
		}
	}
	historyCount(t, pool, `SELECT count(*) FROM screening_history_showtimes WHERE provider='scare'`, 3)
	again, _, err := store.Load(t.Context())
	if err != nil || !reflect.DeepEqual(loaded, again) {
		t.Fatal("reload changed", err)
	}
	if schedule.ValidateDataset(d, true) != nil {
		t.Fatal("fixture invalid")
	}
}
