package cinewest

import (
	"context"
	"os"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

func TestProxyFullSyncContractIntegration(t *testing.T) {
	path := os.Getenv("CINEWEST_LIVE_PROXY_FILE")
	if path == "" {
		t.Skip("CINEWEST_LIVE_PROXY_FILE not set")
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
	client, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal("client unavailable")
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal("timezone unavailable")
	}
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	dataset, summary, err := Sync(ctx, client, SyncOptions{From: now.In(location).Format("2006-01-02"), Now: now})
	if err != nil {
		t.Fatalf("contract sync failed: %v", err)
	}
	want := map[string]bool{}
	for _, id := range schedule.CinewestTheaterIDs() {
		want[id] = true
	}
	for _, theater := range dataset.Theaters {
		if !want[theater.ProviderID] {
			t.Fatal("unexpected cinema identity")
		}
		delete(want, theater.ProviderID)
	}
	if len(want) != 0 || len(dataset.Theaters) != 13 {
		t.Fatal("incomplete cinema manifest")
	}
	t.Logf("cinemas=%d movies=%d showtimes=%d requests=%d", summary.Cinemas, summary.Movies, summary.Showtimes, summary.Requests)
}
