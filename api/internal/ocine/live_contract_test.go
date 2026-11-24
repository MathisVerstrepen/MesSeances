package ocine

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

// Read-only, bounded programme and one checkout contract; never publishes data.
func TestProxyProgramAndCheckoutContractIntegration(t *testing.T) {
	path := os.Getenv("OCINE_LIVE_PROXY_FILE")
	if path == "" {
		t.Skip("OCINE_LIVE_PROXY_FILE not set")
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
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	loc, _ := time.LoadLocation(schedule.Timezone)
	now := time.Now()
	from := now.In(loc).Format(time.DateOnly)
	if os.Getenv("OCINE_LIVE_FULL") == "1" {
		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Minute)
		defer cancel()
		_, s, err := Sync(ctx, client, SyncOptions{From: from, Now: now})
		if err != nil {
			t.Fatalf("full Ocine sync failed: %v, requests=%d", err, s.Requests)
		}
		t.Logf("cinemas=%d films=%d showtimes=%d ends=%d transient=%d requests=%d", s.Cinemas, s.Movies, s.Showtimes, s.EndsRecovered, s.CheckoutTransientFailures, s.Requests)
		return
	}
	films, sessions, ends, transient := 0, 0, 0, 0
	for _, id := range cinemaIDs {
		p, err := loadBoxofficeProgramme(ctx, client, id, from, loc)
		if err != nil {
			t.Fatalf("programme %s rejected: %v", id, err)
		}
		films += len(p.movies)
		sessions += len(p.showtimes)
		for _, row := range p.showtimes {
			if !row.StartTime.After(now.Add(3*time.Hour)) || row.BookingURL == "" {
				continue
			}
			body, canonical, err := client.CheckoutBoxoffice(ctx, row, id)
			if err != nil {
				var requestErr *syncproxy.RequestError
				if ctx.Err() == nil && errors.As(err, &requestErr) && (requestErr.Kind == syncproxy.FailureTransport || requestErr.Kind == syncproxy.FailureResponseRead || requestErr.Kind == syncproxy.FailureServer) {
					transient++
					break
				}
				t.Fatalf("checkout %s unavailable: %v", id, err)
			}
			film, epoch, _, _, ok := canonicalCheckoutParts(canonical, id)
			if !ok || !strings.HasPrefix(row.BookingURL, "https://achat.ocine.fr/") {
				t.Fatalf("checkout %s binding rejected", id)
			}
			end, _, err := parseCheckoutFacts(body, row, id, film, epoch, loc)
			if err != nil {
				t.Fatalf("checkout %s rejected: %v", id, err)
			}
			if end.After(row.StartTime) {
				ends++
			}
			break
		}
	}
	t.Logf("cinemas=3 films=%d showtimes=%d ends=%d transient=%d requests=%d", films, sessions, ends, transient, client.RequestCount())
}
