package veo

import (
	"context"
	"os"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

// This opt-in contract fetches catalogs and complete advertised programmes only.
// No checkout, database publication, TMDB call or raw response persistence occurs.
func TestProxyAcquisitionContractIntegration(t *testing.T) {
	path := os.Getenv("VEO_ACQUISITION_PROXY_FILE")
	if path == "" {
		t.Skip("VEO_ACQUISITION_PROXY_FILE not set")
	}
	if path != "/home/mathis/Documents/Dev/movieflow/tmp/proxies.txt" {
		t.Fatal("approved proxy path required")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("proxy unavailable")
	}
	proxies, err := syncproxy.Parse(f)
	_ = f.Close()
	if err != nil {
		t.Fatal("proxy invalid")
	}
	client, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	now := time.Now()
	loc, _ := time.LoadLocation(schedule.Timezone)
	summary := SyncSummary{}
	data, err := acquire(ctx, client, SyncOptions{From: now.In(loc).Format(time.DateOnly), Now: now}, &summary)
	if err != nil {
		t.Fatalf("acquisition rejected: %v requests=%d", err, client.RequestCount())
	}
	if schedule.ValidateDataset(data, true) != nil {
		t.Fatal("acquisition dataset invalid")
	}
	t.Logf("cinemas=%d films=%d showtimes=%d requests=%d", len(data.Theaters), summary.Movies, len(data.Showtimes), client.RequestCount())
}
