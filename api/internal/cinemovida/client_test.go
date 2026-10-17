package cinemovida

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"messeances/api/internal/syncproxy"
)

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(r *http.Request, status int, body, media string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {media}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}
func clientWithTransport(t *testing.T, transport http.RoundTripper, count int) *Client {
	t.Helper()
	clients := make([]*http.Client, count)
	for i := range clients {
		clients[i] = &http.Client{Transport: transport}
	}
	c, err := newClient(clients, func(ctx context.Context, _ time.Duration) error { return ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRequestURLPolicyAndBatching(t *testing.T) {
	for _, raw := range []string{CinemasURL, programURL("P0176"), moviesURL([]string{"123", "cLocal_1"}), scheduleURL("P0176", "2026-10-17", "2027-01-01")} {
		u, _ := url.Parse(raw)
		if !allowedURL(u) {
			t.Fatal("supported request rejected", raw)
		}
	}
	for _, raw := range []string{
		strings.Replace(CinemasURL, "https:", "http:", 1), strings.Replace(CinemasURL, ".com/", ".com:443/", 1), CinemasURL + "?", programURL("P9999"),
		APIBaseURL + "/api/gatsby-source-boxofficeapi/scheduledMovies?theaterId=P0176&theaterId=P0218",
		APIBaseURL + "/api/gatsby-source-boxofficeapi/schedule?" + url.Values{"theaters": {`{"id":"P0176","timeZone":"Europe/Paris","extra":true}`}, "includeAllMovies": {"true"}, "from": {"2026-10-17T00:00:00"}, "to": {"2026-10-18T00:00:00"}}.Encode(),
		strings.Replace(scheduleURL("P0176", "2026-10-17", "2026-10-18"), "T00%3A00%3A00", "T03%3A00%3A00", 1),
		"https://relay.mvtx.us/ticketing/dbz", APIBaseURL + "/api/gatsby-source-boxofficeapi/../movies",
	} {
		u, err := url.Parse(raw)
		if err == nil && allowedURL(u) {
			t.Fatal("unsafe URL accepted", raw)
		}
	}
	ids := make([]string, 151)
	c := clientWithTransport(t, roundTripper(func(*http.Request) (*http.Response, error) { t.Fatal("fragment alias was sent"); return nil, nil }), 1)
	if _, err := c.Get(t.Context(), OperationCinemas, CinemasURL+"#"); err == nil {
		t.Fatal("bare fragment accepted")
	}
	for i := range ids {
		ids[i] = "c" + strings.Repeat("x", 106) + fmt.Sprint(i)
	}
	batches := batchMovieIDs(ids)
	n := 0
	for _, batch := range batches {
		n += len(batch)
		if len(batch) > 50 || len(moviesURL(batch)) > MaxRequestURLBytes {
			t.Fatal("batch limit exceeded")
		}
	}
	if n != len(ids) || len(batches) != 4 {
		t.Fatal("batch completeness")
	}
	u, _ := url.Parse(scheduleURL("P0176", "2026-10-17", "2026-10-18"))
	if u.Query().Get("from") != "2026-10-17T00:00:00" || u.Query().Get("to") != "2026-10-20T00:00:00" {
		t.Fatal("first or last calendar day truncated")
	}
}

func TestCheckoutRedirectBoundBeforeSend(t *testing.T) {
	start, _ := time.Parse(time.RFC3339, "2026-10-17T20:00:00+02:00")
	source := "https://achat.cinema-castillet.com/reserver/r/123"
	path := fmt.Sprintf("/reserver/F456/D%d/VF/789/", start.Unix())
	for _, tc := range []struct {
		name, destination string
		hops              int
	}{
		{"absolute", "https://achat.cinema-castillet.com" + path, 2},
		{"relative", path, 2},
		{"cross theater", "https://achat.cinema-megacastillet.com" + path, 1},
		{"wrong start", fmt.Sprintf("https://achat.cinema-castillet.com/reserver/F456/D%d/VF/789/", start.Unix()+60), 1},
		{"query", path + "?x=1", 1}, {"fragment", path + "#", 1},
		{"port", "https://achat.cinema-castillet.com:443" + path, 1},
		{"userinfo", "https://user@achat.cinema-castillet.com" + path, 1},
		{"dot alias", "/a/../" + strings.TrimPrefix(path, "/"), 1},
		{"escaped alias", strings.Replace(path, "/VF/", "/%56F/", 1), 1},
		{"other session", "/reserver/r/456", 1}, {"relay", "https://relay.mvtx.us/ticketing/dbz", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := clientWithTransport(t, roundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					if r.URL.String() != source {
						t.Fatal("original source changed")
					}
					resp := response(r, 302, "", "text/html")
					resp.Header.Set("Location", tc.destination)
					return resp, nil
				}
				if r.URL.Host != "achat.cinema-castillet.com" || r.URL.Path != path {
					t.Fatal("unbound destination sent")
				}
				return response(r, 200, `<span id="s_fin">fin prévue à 22h00</span>`, "text/html"), nil
			}), 1)
			body, err := c.Checkout(t.Context(), source, start)
			if calls != tc.hops || (err == nil) != (tc.hops == 2) || err == nil && len(body) == 0 {
				t.Fatal("redirect policy", calls, err)
			}
		})
	}
	// A second canonical hop must also stop before the third destination.
	calls := 0
	c := clientWithTransport(t, roundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		resp := response(r, 302, "", "text/html")
		target := path
		if calls == 2 {
			target = strings.Replace(path, "F456", "F457", 1)
		}
		resp.Header.Set("Location", target)
		return resp, nil
	}), 1)
	if _, err := c.Checkout(t.Context(), source, start); err == nil || calls != 2 {
		t.Fatal("second hop sent", calls, err)
	}
	for _, raw := range []string{source + "?", source + "#", strings.Replace(source, "/r/123", "/r/0123", 1), "https://relay.mvtx.us/ticketing/dbz"} {
		before := calls
		if _, err := c.Checkout(t.Context(), raw, start); err == nil || calls != before {
			t.Fatal("unsafe original was sent")
		}
	}
}

func TestTransportRetriesChallengeLimitsAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		kind   syncproxy.FailureKind
		want   int
	}{
		{503, "temporary", syncproxy.FailureServer, 4},
		{403, "forbidden", syncproxy.FailureChallenge, 1},
		{429, "slow down", syncproxy.FailureChallenge, 1},
		{200, "Just a moment... cf-chl-", syncproxy.FailureChallenge, 1},
		{200, strings.Repeat("x", MaxResponseBytes+1), syncproxy.FailureResponseLarge, 1},
	} {
		calls := 0
		c := clientWithTransport(t, roundTripper(func(r *http.Request) (*http.Response, error) {
			calls++
			return response(r, tc.status, tc.body, "application/json"), nil
		}), 4)
		_, err := c.Get(t.Context(), OperationCinemas, CinemasURL)
		var failure *RequestError
		if !errors.As(err, &failure) || failure.Kind != tc.kind || calls != tc.want {
			t.Fatal("classification or retry", failure, calls, tc.kind, tc.want)
		}
	}
	var calls atomic.Int32
	c := clientWithTransport(t, roundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, fmt.Errorf("proxy password secret raw source")
	}), 4)
	_, err := c.Get(t.Context(), OperationCinemas, CinemasURL)
	if err == nil || strings.Contains(err.Error(), "secret") || calls.Load() != 4 {
		t.Fatal("transport retry or sanitization")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := calls.Load()
	if _, err := c.Get(ctx, OperationCinemas, CinemasURL); !errors.Is(err, context.Canceled) || calls.Load() != before {
		t.Fatal("parent cancellation ignored", err)
	}
}

func TestConcordeOriginalCheckoutStartBinding(t *testing.T) {
	start, _ := time.Parse(time.RFC3339, "2026-10-17T20:00:00+02:00")
	source := "https://achat.concordecinema.com/r/123"
	for _, offset := range []int64{0, 60} {
		calls := 0
		c := clientWithTransport(t, roundTripper(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				if r.URL.String() != source {
					t.Fatal("original route changed")
				}
				resp := response(r, 302, "", "text/html")
				resp.Header.Set("Location", fmt.Sprintf("/reserver/F456/D%d/VF/789/", start.Unix()+offset))
				return resp, nil
			}
			return response(r, 200, `<span id="s_fin">fin prévue à 22h00</span>`, "text/html"), nil
		}), 1)
		_, err := c.Checkout(t.Context(), source, start)
		if offset == 0 && (err != nil || calls != 2) || offset != 0 && (err == nil || calls != 1) {
			t.Fatal("Concorde start binding", calls, err)
		}
	}
}
