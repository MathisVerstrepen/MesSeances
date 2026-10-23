package veo

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

// This opt-in contract exercises every advertised session without publication.
func TestProxyFullSyncContractIntegration(t *testing.T) {
	path := os.Getenv("VEO_LIVE_PROXY_FILE")
	if path == "" {
		t.Skip("set VEO_LIVE_PROXY_FILE for full proxy contract")
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
	c, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal("client unavailable")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Minute)
	defer cancel()
	now := time.Now()
	loc, _ := time.LoadLocation(schedule.Timezone)
	g := &fullSyncProgressGetter{Getter: c, t: t}
	d, s, err := Sync(ctx, g, SyncOptions{From: serviceDate(now.In(loc)), Now: now})
	t.Logf("requests=%d cinemas=%d movies=%d sessions=%d checkouts=%d checkout_fetches=%d exact_ends=%d retries=%d recoveries=%d", s.Requests, s.Cinemas, s.Movies, s.Showtimes, s.Checkouts, g.completed.Load(), s.ExactEnds, s.CheckoutRetryAttempts, s.CheckoutRecoveries)
	if err != nil {
		var requestErr *RequestError
		if errors.As(err, &requestErr) {
			// Only the public source URL and attempt counter; never log transport
			// causes, proxy configuration or provider responses.
			parts := strings.SplitN(requestErr.SyncFailureDetail(), ";", 3)
			if len(parts) >= 2 {
				t.Logf("source=%s;%s", parts[0], parts[1])
			}
		}
		var rejected *CheckoutParseError
		if errors.As(err, &rejected) {
			t.Fatalf("cinema=%s showing=%s reason=%s form=%s action_bits=%d", rejected.CinemaID, rejected.ShowingID, rejected.Reason, rejected.FormSubreason(), rejected.ActionDifferences())
		}
		t.Fatalf("sync failed: %v", err)
	}
	if len(d.Theaters) == 0 || len(d.Showtimes) == 0 {
		t.Fatal("empty snapshot")
	}
}

type fullSyncProgressGetter struct {
	Getter
	t         *testing.T
	completed atomic.Int64
}

func (g *fullSyncProgressGetter) Checkout(ctx context.Context, raw string, start time.Time) (CheckoutResult, error) {
	r, err := g.Getter.Checkout(ctx, raw, start)
	if completed := g.completed.Add(1); completed%100 == 0 {
		g.t.Logf("checkout_fetches=%d requests=%d", completed, g.RequestCount())
	}
	return r, err
}
