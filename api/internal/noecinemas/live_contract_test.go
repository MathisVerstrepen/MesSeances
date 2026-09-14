package noecinemas

import (
	"context"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
	"os"
	"testing"
	"time"
)

// Opt-in only. No raw responses, publication, or full booking crawl.
func TestProxyScheduleContractIntegration(t *testing.T) {
	path, ok := os.LookupEnv("NOECINEMAS_LIVE_PROXY_FILE")
	if !ok {
		t.Skip("set NOECINEMAS_LIVE_PROXY_FILE for proxy-only contract")
	}
	if path != "/home/mathis/Documents/Dev/movieflow/tmp/proxies.txt" {
		t.Fatal("live contract requires approved proxy path")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("approved proxy file unavailable")
	}
	defer func() { _ = f.Close() }()
	proxies, err := syncproxy.Parse(f)
	if err != nil {
		t.Fatal("invalid proxy configuration")
	}
	c, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	l, _ := time.LoadLocation(schedule.Timezone)
	now := time.Now()
	from := now.In(l)
	if from.Hour() < 3 {
		from = from.AddDate(0, 0, -1)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	d, s, err := syncSnapshot(ctx, c, SyncOptions{From: from.Format(time.DateOnly), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if err = enrichRooms(ctx, c, d.Showtimes, &s, time.Minute, 8); err != nil {
		t.Fatal(err)
	}
	if err = schedule.ValidateDataset(d, true); err != nil {
		t.Fatal("invalid live dataset")
	}
	t.Logf("cinemas=%d movies=%d sessions=%d requests=%d from=%s through=%s rooms_attempted=%d recovered=%d unresolved=%d skipped=%d", s.Cinemas, s.Movies, s.Showtimes, c.RequestCount(), d.Window.From, d.Window.Through, s.RoomsAttempted, s.RoomsRecovered, s.RoomsUnresolved, s.RoomsBudgetSkipped)
}
