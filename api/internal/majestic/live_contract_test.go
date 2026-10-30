package majestic

import (
	"context"
	"os"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

// This opt-in contract fetches through proxies without publishing a snapshot.
func TestProxyFullSyncContractIntegration(t *testing.T) {
	path := os.Getenv("MAJESTIC_LIVE_PROXY_FILE")
	if path == "" {
		t.Skip("set MAJESTIC_LIVE_PROXY_FILE for full proxy contract")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("proxy file unavailable")
	}
	proxies, parseErr := syncproxy.Parse(f)
	closeErr := f.Close()
	if parseErr != nil || closeErr != nil {
		t.Fatal("invalid proxies")
	}
	client, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal("client unavailable")
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal("timezone unavailable")
	}
	now := time.Now()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	dataset, summary, err := Sync(ctx, client, SyncOptions{From: now.In(location).Format(time.DateOnly), Now: now})
	if err != nil {
		t.Fatalf("requests=%d sync failed: %v", summary.Requests, err)
	}
	if len(dataset.Theaters) != len(sites) || len(dataset.Showtimes) == 0 {
		t.Fatal("incomplete live snapshot")
	}
	t.Logf("requests=%d cinemas=%d movies=%d sessions=%d", summary.Requests, summary.Cinemas, summary.Movies, summary.Showtimes)
}
