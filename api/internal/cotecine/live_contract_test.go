package cotecine

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

// seededSources is the migration that seeds the disabled Cotecine sources.
const seededSources = "../database/migrations/088_cotecine_sources.sql"

var seededRow = regexp.MustCompile(`\('cotecine', '([0-9]+)', '(?:[^']|'')*', '([^']+)', '[0-9]+', '((?:[^']|'')*)'::jsonb\)`)

// seeded reads the sources seeded by the migration.
func seeded(t *testing.T) []Source {
	t.Helper()
	migration, err := os.ReadFile(seededSources)
	if err != nil {
		t.Fatal("seeded sources migration unavailable")
	}
	var sources []Source
	for _, m := range seededRow.FindAllStringSubmatch(string(migration), -1) {
		var address struct {
			Address    string `json:"address"`
			PostalCode string `json:"postal_code"`
			City       string `json:"city"`
		}
		if err := json.Unmarshal([]byte(strings.ReplaceAll(m[3], "''", "'")), &address); err != nil {
			t.Fatalf("source %s params: %v", m[1], err)
		}
		sources = append(sources, Source{TheaterID: m[1], Website: m[2], Address: address.Address, PostalCode: address.PostalCode, City: address.City})
	}
	return sources
}

func TestSeededSourcesAreValid(t *testing.T) {
	sources := seeded(t)
	if len(sources) != 24 {
		t.Fatalf("seeded sources=%d", len(sources))
	}
	for i, s := range sources {
		if !validSource(s) {
			t.Fatalf("invalid seeded source %+v", s)
		}
		for _, o := range sources[:i] {
			if o.TheaterID == s.TheaterID || o.Website == s.Website {
				t.Fatalf("duplicate seeded source %s", s.TheaterID)
			}
		}
	}
}

// This opt-in contract synchronizes every seeded cinema through proxies, one
// snapshot per cinema then combined, without publishing anything.
func TestProxySeededSourcesContractIntegration(t *testing.T) {
	path := os.Getenv("COTECINE_LIVE_PROXY_FILE")
	if path == "" {
		t.Skip("set COTECINE_LIVE_PROXY_FILE for the seeded sources proxy contract")
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
	sources := seeded(t)
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal("timezone unavailable")
	}
	now := time.Now()
	from := now.In(location).Format(time.DateOnly)
	failed := 0
	for _, source := range sources {
		client, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 30 * time.Second})
		if err != nil {
			t.Fatal("client unavailable")
		}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		_, summary, err := Sync(ctx, client, SyncOptions{From: from, Now: now, Sources: []Source{source}})
		cancel()
		if err != nil {
			failed++
			t.Errorf("cinema %s requests=%d: %v", source.TheaterID, summary.Requests, err)
			continue
		}
		t.Logf("cinema %s movies=%d sessions=%d skipped=%d", source.TheaterID, summary.Movies, summary.Showtimes, summary.Skipped)
	}
	t.Logf("cinemas=%d failed=%d", len(sources), failed)
	if failed > 0 {
		return
	}
	client, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal("client unavailable")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
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
