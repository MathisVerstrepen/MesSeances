package cineville

import (
	"context"
	"os"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

func TestProxyFullSyncContractIntegration(t *testing.T) {
	path := os.Getenv("CINEVILLE_LIVE_PROXY_FILE")
	if path == "" {
		t.Skip("CINEVILLE_LIVE_PROXY_FILE not set")
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
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	fetcher := &liveCatalogFetcher{Client: client}
	dataset, summary, err := Sync(ctx, fetcher, SyncOptions{From: now.In(location).Format(time.DateOnly), Now: now})
	if err != nil {
		t.Fatalf("contract sync failed: %v", err)
	}
	if schedule.ValidateDataset(dataset, true) != nil || len(dataset.Theaters) != len(fetcher.catalog) {
		t.Fatal("invalid or incomplete snapshot")
	}
	for i, theater := range dataset.Theaters {
		if theater.ProviderID != string(fetcher.catalog[i].ID) {
			t.Fatal("unexpected cinema identity")
		}
	}
	for _, showing := range dataset.Showtimes {
		if !showing.EndTime.Equal(showing.StartTime) || showing.FirstPartDurationMinutes != 0 {
			t.Fatal("source end must remain unknown")
		}
	}
	if summary.Cinemas != len(dataset.Theaters) || summary.Showtimes != len(dataset.Showtimes) || summary.Movies == 0 || summary.Requests < len(dataset.Theaters)+1 {
		t.Fatal("inconsistent acquisition summary")
	}
	t.Logf("cinemas=%d movies=%d showtimes=%d requests=%d", summary.Cinemas, summary.Movies, summary.Showtimes, summary.Requests)
}

// Retain only the latest parsed catalog, including after a full acquisition restart.
type liveCatalogFetcher struct {
	*Client
	catalog []cinema
}

func (f *liveCatalogFetcher) Fetch(ctx context.Context) ([]byte, error) {
	body, err := f.Client.Fetch(ctx)
	if err == nil {
		_, f.catalog, err = parseBootstrap(body)
		if err != nil {
			return nil, payloadError(OperationBootstrap)
		}
	}
	return body, err
}
