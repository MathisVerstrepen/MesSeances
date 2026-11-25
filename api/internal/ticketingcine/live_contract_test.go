package ticketingcine

import (
	"context"
	"os"
	"regexp"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

// seededSources is the migration that seeds the disabled TicketingCiné sources.
const seededSources = "../database/migrations/085_ticketingcine_sources.sql"

// This opt-in contract synchronizes every seeded site through proxies, one
// snapshot per site, without publishing anything. It reports each site that
// would fail once enabled.
func TestProxySeededSourcesContractIntegration(t *testing.T) {
	path := os.Getenv("TICKETINGCINE_LIVE_PROXY_FILE")
	if path == "" {
		t.Skip("set TICKETINGCINE_LIVE_PROXY_FILE for the seeded sources proxy contract")
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
	migration, err := os.ReadFile(seededSources)
	if err != nil {
		t.Fatal("seeded sources migration unavailable")
	}
	sites := regexp.MustCompile(`'(EMS[0-9]{4})'`).FindAllStringSubmatch(string(migration), -1)
	if len(sites) == 0 {
		t.Fatal("no seeded site")
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal("timezone unavailable")
	}
	now := time.Now()
	failed := 0
	for _, match := range sites {
		site := match[1]
		client, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 20 * time.Second})
		if err != nil {
			t.Fatal("client unavailable")
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		dataset, summary, err := Sync(ctx, client, SyncOptions{From: now.In(location).Format(time.DateOnly), Now: now, Sources: []Source{{SiteID: site}}})
		cancel()
		if err != nil {
			failed++
			t.Errorf("site %s requests=%d: %v", site, summary.Requests, err)
			continue
		}
		t.Logf("site %s movies=%d sessions=%d", site, summary.Movies, len(dataset.Showtimes))
	}
	t.Logf("sites=%d failed=%d", len(sites), failed)
	if failed > 0 {
		return
	}
	// National films are shared across sites, so the combined snapshot also
	// checks that their metadata agree.
	sources := make([]Source, len(sites))
	for i, match := range sites {
		sources[i] = Source{SiteID: match[1]}
	}
	client, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal("client unavailable")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	_, summary, err := Sync(ctx, client, SyncOptions{From: now.In(location).Format(time.DateOnly), Now: now, Sources: sources})
	if err != nil {
		t.Fatalf("combined requests=%d: %v", summary.Requests, err)
	}
	t.Logf("combined cinemas=%d movies=%d conflicts=%d sessions=%d requests=%d", summary.Cinemas, summary.Movies, summary.MovieConflicts, summary.Showtimes, summary.Requests)
}
