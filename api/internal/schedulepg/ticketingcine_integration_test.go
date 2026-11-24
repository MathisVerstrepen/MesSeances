package schedulepg

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/ticketingcine"
)

type ticketingcineFixtureGetter map[string][]byte

func (g ticketingcineFixtureGetter) Program(_ context.Context, site string) ([]byte, error) {
	return g[site], nil
}
func (ticketingcineFixtureGetter) RequestCount() int { return 0 }

func ticketingcineTestDataset(t *testing.T) Dataset {
	t.Helper()
	program := ticketingcine.Program{Name: "Cinéma Lumière", Address: "1 rue du Cinéma", City: "Lille", ZipCode: "59000", Events: []ticketingcine.Event{
		{ID: "ABCDE", Title: "Film national", Duration: 100, Sessions: []ticketingcine.Session{{ID: "emsx123400000001", Date: "202610162030", HallName: "Salle 1", Version: "VF", FirstPartDuration: 10}}},
		{ID: "emsx1234HC42", Title: "Ciné-débat", Sessions: []ticketingcine.Session{{ID: "emsx123400000002", Date: "202610171400", Version: "VOST"}}},
	}}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"schedule": program}})
	if err != nil {
		t.Fatal(err)
	}
	data, _, err := ticketingcine.Sync(t.Context(), ticketingcineFixtureGetter{"EMS1234": body}, ticketingcine.SyncOptions{From: "2026-10-16", Now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC), Sources: []ticketingcine.Source{{SiteID: "EMS1234"}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A provider registered only through the providers table and sync sources
// publishes, persists and reloads like the built-in providers.
func TestTicketingcineRoundTripIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	d := ticketingcineTestDataset(t)
	if _, err := store.Replace(t.Context(), []Dataset{d}); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Showtimes) != 2 || len(loaded.Theaters) != 1 || loaded.Theaters[0].ID != "ticketingcine-EMS1234" {
		t.Fatalf("loaded=%+v", loaded)
	}
	for i, s := range loaded.Showtimes {
		want := d.Showtimes[i]
		if s.ID != want.ID || s.BookingURL != want.BookingURL || !s.StartTime.Equal(want.StartTime) || !s.EndTime.Equal(want.EndTime) || s.Language != want.Language || s.Movie.ProviderID != want.Movie.ProviderID || s.Movie.PublicMovieID <= 0 {
			t.Fatalf("showtime %d=%+v want=%+v", i, s, want)
		}
	}
	historyCount(t, pool, `SELECT count(*) FROM screening_history_showtimes WHERE provider='ticketingcine'`, 2)
	historyCount(t, pool, `SELECT count(*) FROM public_movie_sources WHERE source_provider='ticketingcine'`, 2)
	again, _, err := store.Load(t.Context())
	if err != nil || !reflect.DeepEqual(loaded, again) {
		t.Fatal("reload changed", err)
	}
	if schedule.ValidateDataset(d, true) != nil {
		t.Fatal("fixture invalid")
	}
}
