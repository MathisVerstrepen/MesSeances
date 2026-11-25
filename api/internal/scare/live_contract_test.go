package scare

import (
	"context"
	"os"
	"regexp"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

// seededSources is the migration that seeds the disabled SCARE sources.
const seededSources = "../database/migrations/086_scare_sources.sql"

// This opt-in contract synchronizes every seeded cinema through proxies
// without publishing anything.
func TestProxySeededSourcesContractIntegration(t *testing.T) {
	path := os.Getenv("SCARE_LIVE_PROXY_FILE")
	if path == "" {
		t.Skip("set SCARE_LIVE_PROXY_FILE for the seeded sources proxy contract")
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
	var sources []Source
	for _, match := range regexp.MustCompile(`\('scare', '([0-9]+)'`).FindAllStringSubmatch(string(migration), -1) {
		sources = append(sources, Source{CinemaID: match[1]})
	}
	if len(sources) == 0 {
		t.Fatal("no seeded cinema")
	}
	client, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 60 * time.Second})
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
	dataset, summary, err := Sync(ctx, client, SyncOptions{From: now.In(location).Format(time.DateOnly), Now: now, Sources: sources})
	if err != nil {
		t.Fatalf("requests=%d sync failed: %v", summary.Requests, err)
	}
	if len(dataset.Showtimes) == 0 {
		t.Fatal("empty live snapshot")
	}
	t.Logf("sources=%d cinemas=%d empty=%d movies=%d conflicts=%d sessions=%d skipped=%d requests=%d", len(sources), summary.Cinemas, summary.EmptyCinemas, summary.Movies, summary.MovieConflicts, summary.Showtimes, summary.Skipped, summary.Requests)
}
