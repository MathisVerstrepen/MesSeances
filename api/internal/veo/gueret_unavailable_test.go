package veo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

func gueretTestUnavailable(start time.Time) string {
	return strings.Replace(gueretTestChain(start)[1], "https://relay.mvtx.us/", "/noweb", 1)
}

type gueretUnreadBody struct{ reads, closes int }

func (b *gueretUnreadBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (b *gueretUnreadBody) Close() error             { b.closes++; return nil }

func TestGueretUnavailableStopsAfterTwoGETsWithoutReading(t *testing.T) {
	start := gueretTestStart()
	chain := gueretTestChain(start)
	for _, location := range []string{gueretTestUnavailable(start), strings.Replace(gueretTestUnavailable(start), "15:00", "15%3A00", 1), "/noweb?perft=15%3a00&perfd=20261016&movie_id=34&house_id=12"} {
		t.Run(fmt.Sprint(len(location)), func(t *testing.T) {
			calls, body := 0, &gueretUnreadBody{}
			c := testClient(t, func(req *http.Request) (*http.Response, error) {
				if calls >= 2 || req.URL.String() != chain[calls] || req.Method != http.MethodGet || req.Header.Get("Cookie") != "" {
					t.Fatal("unavailable destination/action sent")
				}
				r := fixtureResponse(302, "", "text/plain")
				if calls == 0 {
					r.Header.Set("Location", strings.TrimPrefix(chain[1], "https://relay.mvtx.us"))
				} else {
					r.Body = body
					r.Header.Set("Location", location)
				}
				calls++
				return r, nil
			})
			// A caller's forged cell/source must not survive Checkout's binding.
			forged := &checkoutResponse{canonicalURL: chain[3], pool: clientPoolPosition{5, 5}}
			ctx := context.WithValue(t.Context(), checkoutBindingKey{}, checkoutBinding{source: chain[3], start: start, response: forged})
			result, err := c.Checkout(ctx, chain[0], start)
			if err != nil || !result.unavailableEnrichment || len(result.Body) != 0 || result.canonicalURL != "" || result.pool != (clientPoolPosition{}) || calls != 2 || c.RequestCount() != 1 || body.reads != 0 || body.closes != 1 || forged.unavailableEnrichment {
				t.Fatal("unavailable followed/read/fabricated metadata or spent retry", calls, body, err)
			}
		})
	}
	u, _ := url.Parse("https://relay.mvtx.us" + gueretTestUnavailable(start))
	if checkoutFetchURLAllowed(u) || allowedURL(u) || checkoutURLAllowed(u) {
		t.Fatal("unavailable route gained fetch/public authority")
	}
}

func TestGueretUnavailableNearMissesRemainTypedRedirects(t *testing.T) {
	start := gueretTestStart()
	chain, exact := gueretTestChain(start), gueretTestUnavailable(start)
	type mutation struct {
		name, location string
		hop, status    int
		duplicate      bool
	}
	cases := []mutation{{"missing", "", 1, 302, false}, {"two-locations", exact, 1, 302, true}}
	for _, status := range []int{200, 301, 303, 307, 308} {
		cases = append(cases, mutation{fmt.Sprint("status-", status), exact, 1, status, false})
	}
	for _, hop := range []int{0, 2, 3} {
		cases = append(cases, mutation{fmt.Sprint("hop-", hop), exact, hop, 302, false})
	}
	locations := []string{
		"https://relay.mvtx.us" + exact, "//relay.mvtx.us" + exact, "https://other.test" + exact,
		"https://relay.mvtx.us:443" + exact, "https://user@relay.mvtx.us" + exact,
		strings.Replace(exact, "/noweb?", "/noweb/?", 1), strings.Replace(exact, "/noweb?", "/noweb.?", 1),
		strings.Replace(exact, "/noweb?", "/NOWEB?", 1), strings.Replace(exact, "/noweb?", "/%6eoweb?", 1),
		strings.Replace(exact, "/noweb?", "/x/../noweb?", 1), strings.Replace(exact, "/noweb?", "/./noweb?", 1),
		strings.Replace(exact, "/noweb?", "noweb?", 1), "/noweb", "/noweb?", exact + "#", exact + "#x", exact + "?", exact + "&",
		exact + "&extra=1", exact + "&house_id=12", strings.Replace(exact, "house_id=12&", "", 1),
		strings.Replace(exact, "house_id=12", "house_id=13", 1), strings.Replace(exact, "movie_id=34", "movie_id=35", 1),
		strings.Replace(exact, "20261016", "20261017", 1), strings.Replace(exact, "15:00", "15:01", 1),
		strings.Replace(exact, "15:00", "25:00", 1), strings.Replace(exact, "15:00", "15%253A00", 1),
		strings.Replace(exact, "20261016", "20260230", 1), strings.Replace(exact, "house_id", "house%5Fid", 1),
		strings.Replace(exact, "movie_id=34", "movie_id=34%26x%3D1", 1), strings.Replace(exact, "house_id=12", "house_id=012", 1),
		strings.Replace(exact, "house_id=12", "house_id=%31%32", 1), strings.Replace(exact, "house_id=12", "house_id="+strings.Repeat("1", 4096), 1),
		exact + " ", exact + "\t", exact + "\n", exact + "\r", exact + "\\", exact + "%00", exact + "%ZZ",
	}
	for i, location := range locations {
		cases = append(cases, mutation{fmt.Sprint("raw-", i), location, 1, 302, false})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(req *http.Request) (*http.Response, error) {
				if calls > tc.hop || req.URL.String() != chain[calls] || req.Method != http.MethodGet {
					t.Fatal("rejected target sent")
				}
				status, target := 200, ""
				if calls < 3 {
					status, target = []int{302, 302, 307}[calls], chain[calls+1]
				}
				if calls == tc.hop {
					status, target = tc.status, tc.location
				}
				r := fixtureResponse(status, "synthetic", "text/html")
				if target != "" {
					r.Header.Set("Location", target)
				}
				if tc.duplicate && calls == tc.hop {
					r.Header.Add("Location", target)
				}
				calls++
				return r, nil
			})
			result, err := c.Checkout(t.Context(), chain[0], start)
			var failure *RequestError
			if !errors.As(err, &failure) || failure.Kind != syncproxy.FailureRedirect || result.unavailableEnrichment || calls != tc.hop+1 {
				t.Fatal("near-miss became unavailable or followed", calls, err)
			}
			facts, ok := failure.RedirectFacts()
			if !ok || facts.Branch != "gueret" || facts.Phase != "response" || !facts.HopKnown || facts.Hop != tc.hop {
				t.Fatal("typed rejection lost transport facts")
			}
		})
	}
	// The original source/start binding remains mandatory before ANY send.
	c := testClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("misbound source sent"); return nil, errShape })
	if result, err := c.Checkout(t.Context(), chain[0], start.Add(time.Minute)); err == nil || result.unavailableEnrichment {
		t.Fatal("misbound source became unavailable")
	}
}

func TestGueretUnavailableRequiresActualBoundResponseIdentity(t *testing.T) {
	start := gueretTestStart()
	chain := gueretTestChain(start)
	for _, kind := range []string{"source-start", "root-clock", "missing-response", "wrong-response", "wrong-method", "regular-branch", "missing-cell"} {
		t.Run(kind, func(t *testing.T) {
			u, _ := url.Parse(chain[1])
			b := checkoutBinding{source: chain[0], start: start, response: &checkoutResponse{}}
			r := fixtureResponse(302, "", "text/html")
			defer func() { _ = r.Body.Close() }()
			r.Header.Set("Location", gueretTestUnavailable(start))
			r.Request, _ = http.NewRequestWithContext(t.Context(), http.MethodGet, u.String(), nil)
			switch kind {
			case "source-start":
				b.start = start.Add(time.Minute)
			case "root-clock":
				u.RawQuery = strings.Replace(u.RawQuery, "15:00", "15:01", 1)
				r.Request.URL = u
			case "missing-response":
				r.Request = nil
			case "wrong-response":
				r.Request.URL, _ = url.Parse(chain[2])
			case "wrong-method":
				r.Request.Method = http.MethodPost
			case "regular-branch":
				b.source = "https://achat.veocinemas.fr/fixture/r/1"
			case "missing-cell":
				b.response = nil
			}
			if gueretUnavailable(u, r, b) {
				t.Fatal("unattested unavailable accepted")
			}
		})
	}
}

func TestGueretUnavailableConcurrentRetriesAndCancellation(t *testing.T) {
	const count = 24
	var mu sync.Mutex
	attempts := map[string]int{}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		b := req.Context().Value(checkoutBindingKey{}).(checkoutBinding)
		hop, chain := gueretHop(req.URL, b), gueretTestChain(b.start)
		if hop == 0 {
			mu.Lock()
			attempts[b.source]++
			n := attempts[b.source]
			mu.Unlock()
			if n == 1 {
				return fixtureResponse(503, "retry", "text/html"), nil
			}
		}
		if hop == 3 {
			return fixtureResponse(200, gueretTestPage(chain[3]), "text/html"), nil
		}
		r := fixtureResponse([]int{302, 302, 307}[hop], "", "text/html")
		r.Header.Set("Location", chain[hop+1])
		if hop == 1 && b.start.Minute()%2 != 0 {
			r.Header.Set("Location", gueretTestUnavailable(b.start))
		}
		return r, nil
	})
	// Each deliberate 503 cools its proxy in the shared executor. Supply enough
	// synthetic clients for every concurrent request's distinct-proxy retry.
	transports := make([]roundTripFunc, count*2)
	for i := range transports {
		transports[i] = transport
	}
	c := testClient(t, transports...)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Go(func() {
			start := gueretTestStart().Add(time.Duration(i) * time.Minute)
			result, err := c.Checkout(t.Context(), gueretTestChain(start)[0], start)
			if err != nil || result.unavailableEnrichment != (i%2 != 0) {
				t.Error("worker/retry outcome crossed", err)
				return
			}
			if i%2 != 0 {
				if len(result.Body) != 0 || result.canonicalURL != "" || result.pool != (clientPoolPosition{}) {
					t.Error("unavailable retained other response metadata")
				}
			} else if result.canonicalURL != gueretTestChain(start)[3] || result.pool.index < 1 || result.pool.index > count*2 || result.pool.count != count*2 {
				t.Error("canonical response lost actual pool/identity")
			}
		})
	}
	wg.Wait()
	if c.RequestCount() != count*2 {
		t.Fatal("retry budget changed", c.RequestCount())
	}
	for _, n := range attempts {
		if n != 2 {
			t.Fatal("extra attempt")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	start := gueretTestStart()
	calls := 0
	c = testClient(t, func(req *http.Request) (*http.Response, error) {
		r := fixtureResponse(302, "", "text/html")
		if calls == 0 {
			r.Header.Set("Location", gueretTestChain(start)[1])
		} else {
			cancel()
			r.Header.Set("Location", gueretTestUnavailable(start))
		}
		calls++
		return r, nil
	})
	if result, err := c.Checkout(ctx, gueretTestChain(start)[0], start); !errors.Is(err, context.Canceled) || result.unavailableEnrichment || calls != 2 {
		t.Fatal("unavailable overrode cancellation", err)
	}
}

func TestGueretUnavailableCannotRepairActionRetry(t *testing.T) {
	start := gueretTestStart()
	chain := gueretTestChain(start)
	row := retryTestRow(990)
	row.TheaterID, row.ProviderShowingID = "veo-G06DB", schedule.VeoShowingID("G06DB", "retry-fixture")
	row.ID, row.BookingURL, row.StartTime, row.EndTime = "veo-showing-"+row.ProviderShowingID, chain[0], start, start
	row.Room = "API room"
	calls, attempt := 0, 0
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		b := req.Context().Value(checkoutBindingKey{}).(checkoutBinding)
		hop := gueretHop(req.URL, b)
		calls++
		if hop == 0 {
			attempt++
		}
		if hop == 3 {
			return fixtureResponse(200, strings.Replace(gueretTestPage(chain[3]), "/F34/", "/F35/", 1), "text/html"), nil
		}
		r := fixtureResponse([]int{302, 302, 307}[hop], "", "text/html")
		r.Header.Set("Location", chain[hop+1])
		if hop == 1 && attempt == 2 {
			r.Header.Set("Location", gueretTestUnavailable(start))
		}
		return r, nil
	})
	rows, summary := []schedule.ShowtimeRecord{row}, SyncSummary{}
	err := enrichCheckouts(t.Context(), c, rows, &summary, start.Add(-time.Hour))
	var failure *CheckoutParseError
	if !errors.As(err, &failure) || failure.Reason != RejectBookingForm || calls != 6 || c.RequestCount() != 2 || summary.CheckoutRetryAttempts != 1 || summary.CheckoutRecoveries != 0 || summary.GueretUnavailableCheckouts != 0 || !reflect.DeepEqual(rows[0], row) {
		t.Fatal("unavailable repaired mismatch or applied partial facts", calls, summary, err)
	}
}

func TestGueretUnavailablePreservesFullDatasetAndAtomicFailure(t *testing.T) {
	for _, room := range []string{"SENECHAL5", ""} {
		g := newGueretFixture(t, nil)
		if room == "" {
			g = newGueretFixture(t, func(_ Operation, _ string, b []byte) []byte {
				return []byte(strings.ReplaceAll(string(b), "SENECHAL5", ""))
			})
		}
		c := testClient(t, func(req *http.Request) (*http.Response, error) {
			b := req.Context().Value(checkoutBindingKey{}).(checkoutBinding)
			r := fixtureResponse(302, "", "text/html")
			if gueretHop(req.URL, b) == 0 {
				r.Header.Set("Location", gueretTestChain(b.start)[1])
			} else {
				r.Header.Set("Location", gueretTestUnavailable(b.start))
			}
			return r, nil
		})
		g.checkout = func(ctx context.Context, raw string, start time.Time) (CheckoutResult, error) {
			if schedule.ValidVeoGueretRelayURL(raw) {
				return c.Checkout(ctx, raw, start)
			}
			return g.fixtureGetter.Checkout(ctx, raw, start)
		}
		d, s, err := Sync(t.Context(), g, fixtureOptions())
		if err != nil || len(d.Theaters) != 20 || s.GueretUnavailableCheckouts != 2 || s.ToulouseUnavailableCheckouts != 0 || s.CheckoutRetryAttempts != 0 || d.Window.Through != "2026-10-28" {
			t.Fatal("full dataset lost", s, err)
		}
		for _, r := range d.Showtimes {
			if r.TheaterID == "veo-G06DB" && (r.Room != room || !r.EndTime.Equal(r.StartTime) || !schedule.ValidVeoGueretRelayURL(r.BookingURL) || r.ProviderShowingID != schedule.VeoShowingID("G06DB", "gueret-"+r.ServiceDate)) {
				t.Fatal("gap changed source row or invented facts")
			}
		}
		for _, cv := range d.Coverage {
			if cv.TheaterID == "veo-G06DB" && cv.Status != schedule.CoverageComplete {
				t.Fatal("advertised coverage lost")
			}
		}
		// A different row's failure still discards the ENTIRE acquired snapshot.
		g.checkout = func(ctx context.Context, raw string, start time.Time) (CheckoutResult, error) {
			if schedule.ValidVeoGueretRelayURL(raw) {
				return c.Checkout(ctx, raw, start)
			}
			return CheckoutResult{}, &RequestError{Operation: OperationCheckout, Kind: syncproxy.FailureChallenge, StatusCode: 403}
		}
		d, _, err = Sync(t.Context(), g, fixtureOptions())
		if err == nil || len(d.Theaters) != 0 || len(d.Showtimes) != 0 {
			t.Fatal("other failure published partial dataset")
		}
	}
}
