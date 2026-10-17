//go:build cineode_live

package cineode

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type liveGetter struct {
	*Client
	mu         sync.Mutex
	programmes map[string]int
	invalid    bool
}

func (g *liveGetter) Page(ctx context.Context, id, route string) ([]byte, error) {
	g.mu.Lock()
	if !schedule.ValidCineodeIdentity("theater", id) || route != "seances" && route != "infos-cine" && route != "contact" {
		g.invalid = true
	}
	if route == "seances" {
		g.programmes[id]++
	}
	g.mu.Unlock()
	return g.Client.Page(ctx, id, route)
}

func TestProxyFullSyncContractIntegration(t *testing.T) {
	path := os.Getenv("CINEODE_LIVE_PROXY_FILE")
	if path == "" {
		t.Skip("CINEODE_LIVE_PROXY_FILE not set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("proxy file unavailable")
	}
	proxies, parseErr := syncproxy.Parse(f)
	closeErr := f.Close()
	if parseErr != nil || closeErr != nil {
		t.Fatal("proxy file invalid")
	}
	c, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal("client unavailable")
	}
	g := &liveGetter{Client: c, programmes: map[string]int{}}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal("timezone unavailable")
	}
	now := time.Now()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	d, summary, err := Sync(ctx, g, SyncOptions{From: now.In(location).Format(time.DateOnly), Now: now})
	if err != nil {
		t.Fatalf("contract sync failed: %v", err)
	}
	if err := schedule.ValidateDataset(d, true); err != nil {
		t.Fatal("invalid full dataset")
	}
	want := map[string]bool{}
	for _, id := range cinemaIDs {
		want[id] = true
	}
	counts := map[string]int{}
	for _, s := range d.Showtimes {
		counts[s.TheaterID]++
	}
	empty := 0
	for _, th := range d.Theaters {
		if !want[th.ProviderID] || g.programmes[th.ProviderID] != 1 {
			t.Fatal("unexpected or duplicate cinema identity/request")
		}
		delete(want, th.ProviderID)
		if counts[th.ID] == 0 {
			empty++
			if len(th.AvailableDates) != 0 || th.AvailableDates == nil {
				t.Fatal("empty cinema dates not preserved")
			}
		}
	}
	if len(want) != 0 || len(d.Theaters) != 37 || len(g.programmes) != 37 || g.invalid {
		t.Fatal("incomplete cinema census or invalid routes")
	}
	t.Logf("cinemas=%d movies=%d showtimes=%d empty_cinemas=%d requests=%d", summary.Cinemas, summary.Movies, summary.Showtimes, empty, summary.Requests)
}
