package veo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"messeances/api/internal/syncproxy"
)

func TestGueretFinal404StopsAfterFourGETsWithoutReading(t *testing.T) {
	start := gueretTestStart()
	chain := gueretTestChain(start)
	calls, body := 0, &gueretUnreadBody{}
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		if calls >= 4 || req.URL.String() != chain[calls] || req.Method != http.MethodGet || req.Header.Get("Cookie") != "" {
			t.Fatal("unexpected checkout request")
		}
		hop := calls
		calls++
		if hop == 3 {
			r := fixtureResponse(404, "", "text/html")
			r.Body = body
			return r, nil
		}
		r := fixtureResponse([]int{302, 302, 307}[hop], "", "text/html")
		r.Header.Set("Location", chain[hop+1])
		return r, nil
	}, func(*http.Request) (*http.Response, error) {
		t.Fatal("final404 retried on another proxy")
		return nil, errShape
	})
	result, err := c.Checkout(t.Context(), chain[0], start)
	if err != nil || !result.unavailableEnrichment || len(result.Body) != 0 || result.canonicalURL != "" || result.pool != (clientPoolPosition{}) || calls != 4 || c.RequestCount() != 1 || body.reads != 0 || body.closes != 1 {
		t.Fatal("missing session read, retried, or supplied page metadata", calls, body, err)
	}
}

func TestGueretFinal404RequiresBoundResponseIdentity(t *testing.T) {
	start := gueretTestStart()
	chain := gueretTestChain(start)
	for _, kind := range []string{"source-start", "missing-response", "missing-url", "wrong-response", "wrong-method", "regular-branch", "missing-cell", "empty-location", "location", "duplicate-location", "host", "path", "epoch", "representation"} {
		t.Run(kind, func(t *testing.T) {
			u, _ := url.Parse(chain[3])
			b := checkoutBinding{source: chain[0], start: start, response: &checkoutResponse{}}
			r := fixtureResponse(404, "", "text/html")
			defer func() { _ = r.Body.Close() }()
			r.Request, _ = http.NewRequestWithContext(t.Context(), http.MethodGet, u.String(), nil)
			switch kind {
			case "source-start":
				b.start = start.AddDate(0, 0, 1)
			case "missing-response":
				r.Request = nil
			case "missing-url":
				r.Request.URL = nil
			case "wrong-response":
				r.Request.URL, _ = url.Parse(strings.Replace(chain[3], "/78/", "/79/", 1))
			case "wrong-method":
				r.Request.Method = http.MethodPost
			case "regular-branch":
				b.source = "https://achat.veocinemas.fr/fixture/r/1"
			case "missing-cell":
				b.response = nil
			case "empty-location":
				r.Header.Set("Location", "")
			case "location":
				r.Header.Set("Location", chain[3])
			case "duplicate-location":
				r.Header["Location"] = []string{chain[3], chain[3]}
			case "host":
				u.Host = "achat.veocinemas.fr"
			case "path":
				u.Path += "extra/"
			case "epoch":
				u.Path = strings.Replace(u.Path, fmt.Sprint(start.Unix()), fmt.Sprint(start.Unix()+1), 1)
			case "representation":
				u.RawPath = strings.Replace(u.Path, "reserver", "%72eserver", 1)
			}
			if gueretUnavailable(u, r, b) {
				t.Fatal("unattested final404 became unavailable")
			}
		})
	}
}

func TestGueretFinal404DoesNotSuppressOtherFailures(t *testing.T) {
	start := gueretTestStart()
	chain := gueretTestChain(start)
	for _, tc := range []struct {
		hop, status, attempts int
		kind                  syncproxy.FailureKind
	}{
		{0, 404, 1, syncproxy.FailureStatus}, {1, 404, 1, syncproxy.FailureStatus}, {2, 404, 1, syncproxy.FailureStatus},
		{3, 410, 1, syncproxy.FailureStatus}, {3, 403, 1, syncproxy.FailureChallenge}, {3, 429, 1, syncproxy.FailureChallenge},
		{3, 503, 4, syncproxy.FailureServer},
	} {
		t.Run(fmt.Sprintf("hop%d-status%d", tc.hop, tc.status), func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				hop := calls % (tc.hop + 1)
				if req.URL.String() != chain[hop] {
					t.Fatal("wrong redirect destination")
				}
				calls++
				if hop == tc.hop {
					return fixtureResponse(tc.status, "synthetic", "text/html"), nil
				}
				r := fixtureResponse([]int{302, 302, 307}[hop], "", "text/html")
				r.Header.Set("Location", chain[hop+1])
				return r, nil
			})
			c := testClient(t, transport, transport, transport, transport)
			result, err := c.Checkout(t.Context(), chain[0], start)
			var failure *RequestError
			if !errors.As(err, &failure) || failure.Kind != tc.kind || failure.StatusCode != tc.status || result.unavailableEnrichment || c.RequestCount() != tc.attempts || calls != (tc.hop+1)*tc.attempts {
				t.Fatal("status classification or retry changed", calls, err)
			}
		})
	}
	for _, raw := range []string{chain[0], chain[3], "https://achat.veocinemas.fr/fixture/r/1"} {
		c := testClient(t, func(*http.Request) (*http.Response, error) { return fixtureResponse(404, "missing", "text/html"), nil })
		if _, err := c.Get(t.Context(), OperationCheckout, raw); err == nil {
			t.Fatal("unbound404 suppressed")
		}
		if result, err := c.Checkout(t.Context(), raw, start); err == nil || result.unavailableEnrichment {
			t.Fatal("non-final or non-Guéret404 suppressed")
		}
	}
}

func TestGueretFinal404CancellationWins(t *testing.T) {
	start := gueretTestStart()
	chain := gueretTestChain(start)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls, body := 0, &gueretUnreadBody{}
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		hop := calls
		calls++
		if hop == 3 {
			cancel()
			r := fixtureResponse(404, "", "text/html")
			r.Body = body
			return r, nil
		}
		r := fixtureResponse([]int{302, 302, 307}[hop], "", "text/html")
		r.Header.Set("Location", chain[hop+1])
		return r, nil
	})
	result, err := c.Checkout(ctx, chain[0], start)
	if !errors.Is(err, context.Canceled) || result.unavailableEnrichment || calls != 4 || body.reads != 0 || body.closes != 1 {
		t.Fatal("final404 overrode cancellation", calls, body, err)
	}
}
