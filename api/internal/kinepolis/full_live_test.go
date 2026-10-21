package kinepolis

import (
	"context"
	"os"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

func TestProxyFullSyncContractIntegration(t *testing.T) {
	path := os.Getenv("KINEPOLIS_LIVE_PROXY_FILE")
	if path == "" {
		t.Skip("set KINEPOLIS_LIVE_PROXY_FILE for full proxy contract")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("proxy file unavailable")
	}
	defer func() { _ = f.Close() }()
	proxies, err := syncproxy.Parse(f)
	if err != nil {
		t.Fatal("invalid proxies")
	}
	c, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 20 * time.Second, RequestInterval: time.Second})
	if err != nil {
		t.Fatal("client unavailable")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	now := time.Now()
	loc, _ := time.LoadLocation(schedule.Timezone)
	d, s, err := Sync(ctx, c, SyncOptions{From: now.In(loc).Format(time.DateOnly), Now: now})
	if err != nil {
		t.Fatalf("requests=%d sync failed: %v", c.RequestCount(), err)
	}
	t.Logf("requests=%d cinemas=%d sessions=%d", c.RequestCount(), s.Cinemas, s.Showtimes)
	if len(d.Theaters) == 0 || len(d.Showtimes) == 0 {
		t.Fatal("empty snapshot")
	}
}
