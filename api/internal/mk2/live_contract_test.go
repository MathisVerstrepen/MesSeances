package mk2

import (
	"context"
	"os"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

func TestProxyFullSyncContractIntegration(t *testing.T) {
	path := os.Getenv("MK2_LIVE_PROXY_FILE")
	if path == "" {
		t.Skip("MK2_LIVE_PROXY_FILE not set")
	}
	if path != "/home/mathis/Documents/Dev/movieflow/tmp/proxies.txt" {
		t.Fatal("live contract requires approved proxy path")
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
	client, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal("client unavailable")
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal("timezone unavailable")
	}
	now := time.Now()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	dataset, summary, err := Sync(ctx, client, SyncOptions{From: now.In(location).Format(time.DateOnly), Now: now})
	if err != nil {
		t.Fatalf("contract sync failed: %v", err)
	}
	if err := schedule.ValidateDataset(dataset, true); err != nil {
		t.Fatal("invalid dataset")
	}
	if summary.Cinemas != len(dataset.Theaters) || summary.Showtimes != len(dataset.Showtimes) || summary.Movies == 0 || summary.Requests != client.RequestCount() {
		t.Fatal("inconsistent sync summary")
	}
	for _, r := range dataset.Showtimes {
		if !r.EndTime.Equal(r.StartTime) {
			t.Fatal("canonical end must remain unknown")
		}
	}
	t.Logf("cinemas=%d movies=%d showtimes=%d requests=%d through=%s", summary.Cinemas, summary.Movies, summary.Showtimes, summary.Requests, dataset.Window.Through)
}
