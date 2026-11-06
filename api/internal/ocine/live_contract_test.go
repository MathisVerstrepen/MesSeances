package ocine

import (
	"context"
	"errors"
	"os"
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
	var selected *schedule.ShowtimeRecord
	films, sessions := 0, 0
	for _, id := range cinemaIDs {
		body, err := client.Page(ctx, id, "seances")
		if err != nil {
			t.Fatal("programme unavailable")
		}
		p, err := parseProgramme(body, id, from, now, loc)
		if err != nil {
			t.Fatal("programme rejected")
		}
		films += len(p.movies)
		sessions += len(p.showtimes)
		body, err = client.Page(ctx, id, "plan-d-acces")
		if err != nil {
			t.Fatal("address unavailable")
		}
		if _, _, _, err := parseAddress(body, id); err != nil {
			t.Fatal("address rejected")
		}
		if id == "310" {
			for _, r := range p.showtimes {
				if r.StartTime.After(now) && r.BookingURL != "" {
					copy := r
					selected = &copy
					break
				}
			}
		}
	}
	if selected == nil {
		t.Fatal("no future Saint-Omer checkout to verify")
	}
	body, err := client.Checkout(ctx, selected.BookingURL)
	if err != nil {
		var requestErr *RequestError
		if ctx.Err() == nil && errors.As(err, &requestErr) && (requestErr.Kind == syncproxy.FailureTransport || requestErr.Kind == syncproxy.FailureResponseRead || requestErr.Kind == syncproxy.FailureServer) {
			t.Logf("cinemas=3 films=%d showtimes=%d checkout_transient=true requests=%d", films, sessions, client.RequestCount())
			return
		}
		t.Fatal("checkout unavailable")
	}
	end, room, err := parseCheckout(body, *selected, "310", loc)
	if err != nil {
		t.Fatal("checkout rejected")
	}
	t.Logf("cinemas=3 films=%d showtimes=%d end_published=%t room_present=%t requests=%d", films, sessions, end.After(selected.StartTime), room != "", client.RequestCount())
}
