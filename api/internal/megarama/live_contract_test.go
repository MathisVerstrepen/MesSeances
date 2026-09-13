package megarama

import (
	"context"
	"os"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

// This test never opens a database or publishes a snapshot. Network bodies and
// proxy details stay in memory; diagnostics contain only bounded counters.
func TestProxyFullSyncContractIntegration(t *testing.T) {
	path := os.Getenv("MEGARAMA_LIVE_PROXY_FILE")
	if path == "" {
		t.Skip("set MEGARAMA_LIVE_PROXY_FILE for proxy-only live verification")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("open live proxy file failed")
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error("close live proxy file failed")
		}
	})
	proxies, err := syncproxy.Parse(f)
	if err != nil {
		t.Fatal("parse live proxies failed")
	}
	client, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal("create proxy client failed")
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal("load timezone failed")
	}
	now := time.Now()
	day := now.In(location)
	if day.Hour() < 3 {
		day = day.AddDate(0, 0, -1)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	dataset, summary, err := Sync(ctx, client, SyncOptions{From: day.Format("2006-01-02"), Now: now})
	if err != nil {
		t.Fatalf("Megarama live sync: cinemas=%d movies=%d requests=%d showtimes=%d error=%v", summary.Cinemas, summary.Movies, summary.Requests, summary.Showtimes, err)
	}
	if len(dataset.Theaters) == 0 || len(dataset.Showtimes) == 0 {
		t.Fatal("empty live dataset")
	}
	t.Logf("cinemas=%d movies=%d requests=%d showtimes=%d", summary.Cinemas, summary.Movies, summary.Requests, summary.Showtimes)
}
