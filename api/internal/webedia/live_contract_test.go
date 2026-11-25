package webedia

import (
	"context"
	"errors"
	"os"
	"regexp"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

// seededSources is the migration that seeds the disabled Webedia sources.
const seededSources = "../database/migrations/087_webedia_sources.sql"

// This opt-in contract synchronizes every seeded cinema through proxies, one
// snapshot per cinema then combined, without publishing anything.
func TestProxySeededSourcesContractIntegration(t *testing.T) {
	path := os.Getenv("WEBEDIA_LIVE_PROXY_FILE")
	if path == "" {
		t.Skip("set WEBEDIA_LIVE_PROXY_FILE for the seeded sources proxy contract")
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
	for _, m := range regexp.MustCompile(`\('webedia', '([A-Z0-9]{5})', '(?:[^']|'')*', '([^']+)'`).FindAllStringSubmatch(string(migration), -1) {
		sources = append(sources, Source{TheaterID: m[1], Origin: m[2]})
	}
	if len(sources) == 0 {
		t.Fatal("no seeded cinema")
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal("timezone unavailable")
	}
	now := time.Now()
	from := now.In(location).Format(time.DateOnly)
	failed, empty := 0, 0
	for _, source := range sources {
		client, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 30 * time.Second})
		if err != nil {
			t.Fatal("client unavailable")
		}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		_, summary, err := Sync(ctx, client, SyncOptions{From: from, Now: now, Sources: []Source{source}})
		if errors.Is(err, schedule.ErrDatasetValidation) {
			// A cinema without any programme cannot form a snapshot alone.
			if st, fetchErr := fetchSite(ctx, client, source, from, now.AddDate(0, 0, maxWindowDays).Format(time.DateOnly), location); fetchErr == nil && len(st.program) == 0 {
				cancel()
				empty++
				continue
			}
		}
		cancel()
		if err != nil {
			failed++
			t.Errorf("site %s requests=%d: %v", source.TheaterID, summary.Requests, err)
			continue
		}
		t.Logf("site %s movies=%d sessions=%d skipped=%d", source.TheaterID, summary.Movies, summary.Showtimes, summary.Skipped)
	}
	t.Logf("sites=%d failed=%d empty=%d", len(sources), failed, empty)
	if failed > 0 {
		return
	}
	client, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal("client unavailable")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	dataset, summary, err := Sync(ctx, client, SyncOptions{From: from, Now: now, Sources: sources})
	if err != nil {
		t.Fatalf("combined requests=%d: %v", summary.Requests, err)
	}
	if _, err := schedule.PreparePublication(dataset); err != nil {
		t.Fatalf("combined snapshot cannot be published: %v", err)
	}
	t.Logf("combined cinemas=%d movies=%d conflicts=%d sessions=%d skipped=%d requests=%d", summary.Cinemas, summary.Movies, summary.MovieConflicts, summary.Showtimes, summary.Skipped, summary.Requests)
}
