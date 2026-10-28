package veo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

func regularTestCanonical(source string, start time.Time) string {
	u, _ := url.Parse(source)
	return fmt.Sprintf("https://achat.veocinemas.fr/%s/reserver/F624618/D%d/VF/2078/", strings.Split(u.Path, "/")[1], start.Unix())
}

func TestRegularFinal404StopsAfterTwoGETsWithoutReading(t *testing.T) {
	start := gueretTestStart()
	for _, alias := range []string{"ussel/r/215082", "ussel/reserver/r/215082", "decazeville/r/240987", "decazeville/reserver/r/240987", "another-cinema/r/9"} {
		t.Run(alias, func(t *testing.T) {
			source := "https://achat.veocinemas.fr/" + alias
			canonical := regularTestCanonical(source, start)
			if strings.HasPrefix(alias, "decazeville/") {
				canonical = strings.ReplaceAll(strings.ReplaceAll(canonical, "F624618", "F624266"), "/2078/", "/2081/")
			}
			calls, body := 0, &gueretUnreadBody{}
			c := testClient(t, func(req *http.Request) (*http.Response, error) {
				if calls >= 2 || req.Method != http.MethodGet || req.URL.String() != []string{source, canonical}[calls] {
					t.Fatal("unexpected request")
				}
				calls++
				if calls == 1 {
					r := fixtureResponse(307, "", "text/html")
					r.Header.Set("Location", canonical)
					return r, nil
				}
				r := fixtureResponse(404, "", "text/html")
				r.Body = body
				return r, nil
			}, func(*http.Request) (*http.Response, error) {
				t.Fatal("unavailable retried on spare proxy")
				return nil, errShape
			})
			result, err := c.Checkout(t.Context(), source, start)
			if err != nil || !result.unavailableEnrichment || !result.regularFinal404Unavailable || len(result.Body) != 0 || result.canonicalURL != "" || result.pool != (clientPoolPosition{}) || calls != 2 || c.RequestCount() != 1 || body.reads != 0 || body.closes != 1 {
				t.Fatal("final404 read, retried or trusted page facts", calls, body, err)
			}
		})
	}
}

func TestRegularFinal404RequiresBoundRedirectAndActualResponse(t *testing.T) {
	start := gueretTestStart()
	source := "https://achat.veocinemas.fr/ussel/r/215082"
	canonical := regularTestCanonical(source, start)
	for _, kind := range []string{"valid", "missing-cell", "zero-start", "fractional-start", "wrong-start", "invalid-source", "source-slug", "source-host", "missing-provenance", "provenance-status", "provenance-request", "provenance-url", "provenance-method", "provenance-source", "extra-hop", "provenance-location", "relative-location", "empty-provenance-location", "duplicate-provenance-location", "request-method", "request-url", "missing-response", "response-url", "response-method", "response-mismatch", "final-empty-location", "final-location", "final-duplicate-location", "canonical-host", "canonical-slug", "canonical-path", "canonical-epoch", "canonical-query", "canonical-rawpath", "canonical-fragment"} {
		t.Run(kind, func(t *testing.T) {
			b := checkoutBinding{source: source, start: start, response: &checkoutResponse{}}
			prior, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, source, nil)
			req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, canonical, nil)
			req.Response = &http.Response{StatusCode: 307, Request: prior, Header: http.Header{"Location": {canonical}}}
			actual, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, canonical, nil)
			r := &http.Response{StatusCode: 404, Request: actual, Header: http.Header{}}
			switch kind {
			case "missing-cell":
				b.response = nil
			case "zero-start":
				b.start = time.Time{}
			case "fractional-start":
				b.start = start.Add(time.Nanosecond)
			case "wrong-start":
				b.start = start.Add(time.Second)
			case "invalid-source":
				b.source += "?"
			case "source-slug":
				b.source = strings.Replace(source, "ussel", "other", 1)
			case "source-host":
				b.source = gueretTestChain(start)[0]
			case "missing-provenance":
				req.Response = nil
			case "provenance-status":
				req.Response.StatusCode = 302
			case "provenance-request":
				req.Response.Request = nil
			case "provenance-url":
				prior.URL = nil
			case "provenance-method":
				prior.Method = http.MethodPost
			case "provenance-source":
				prior.URL.Path += "1"
			case "extra-hop":
				prior.Response = &http.Response{StatusCode: 307}
			case "provenance-location":
				req.Response.Header.Set("Location", canonical+"?")
			case "relative-location":
				req.Response.Header.Set("Location", req.URL.Path)
			case "empty-provenance-location":
				req.Response.Header.Set("Location", "")
			case "duplicate-provenance-location":
				req.Response.Header.Add("Location", canonical)
			case "request-method":
				req.Method = http.MethodPost
			case "request-url":
				req.URL = nil
			case "missing-response":
				r.Request = nil
			case "response-url":
				actual.URL = nil
			case "response-method":
				actual.Method = http.MethodPost
			case "response-mismatch":
				actual.URL.Path = strings.Replace(actual.URL.Path, "/2078/", "/2079/", 1)
			case "final-empty-location":
				r.Header.Set("Location", "")
			case "final-location":
				r.Header.Set("Location", canonical)
			case "final-duplicate-location":
				r.Header["Location"] = []string{"", ""}
			case "canonical-host":
				req.URL.Host = "other.example"
			case "canonical-slug":
				req.URL.Path = strings.Replace(req.URL.Path, "ussel", "other", 1)
			case "canonical-path":
				req.URL.Path += "extra/"
			case "canonical-epoch":
				req.URL.Path = strings.Replace(req.URL.Path, fmt.Sprint(start.Unix()), fmt.Sprint(start.Unix()+1), 1)
			case "canonical-query":
				req.URL.ForceQuery = true
			case "canonical-rawpath":
				req.URL.RawPath = strings.Replace(req.URL.Path, "reserver", "%72eserver", 1)
			case "canonical-fragment":
				req.URL.Fragment = "fragment"
			}
			if strings.HasPrefix(kind, "canonical-") {
				// Keep provenance/response identities consistent so rejection must
				// come from the canonical URL and binding rules, not a second fault.
				actual.URL = req.URL
				req.Response.Header.Set("Location", req.URL.String())
			}
			if got := regularFinal404Unavailable(req, r, b); got != (kind == "valid") {
				t.Fatal("unattested404 accepted or valid control rejected", got)
			}
		})
	}
}

func TestRegularFinal404PreservesFatalStatusesAndRedirectChecks(t *testing.T) {
	start := gueretTestStart()
	source := "https://achat.veocinemas.fr/ussel/r/215082"
	canonical := regularTestCanonical(source, start)
	for _, tc := range []struct {
		name                  string
		hop, status, attempts int
		kind                  syncproxy.FailureKind
		aliasStatus           int
		locations             []string
	}{
		{name: "alias404", hop: 0, status: 404, attempts: 1, kind: syncproxy.FailureStatus},
		{name: "gone", hop: 1, status: 410, attempts: 1, kind: syncproxy.FailureStatus},
		{name: "forbidden", hop: 1, status: 403, attempts: 1, kind: syncproxy.FailureChallenge},
		{name: "limited", hop: 1, status: 429, attempts: 1, kind: syncproxy.FailureChallenge},
		{name: "server", hop: 1, status: 503, attempts: 4, kind: syncproxy.FailureServer},
		{name: "non307", hop: 1, status: 404, attempts: 1, kind: syncproxy.FailureStatus, aliasStatus: 302},
		{name: "empty-location", hop: 1, status: 404, attempts: 1, kind: syncproxy.FailureStatus, locations: []string{""}},
		{name: "location", hop: 1, status: 404, attempts: 1, kind: syncproxy.FailureStatus, locations: []string{canonical}},
		{name: "duplicate-location", hop: 1, status: 404, attempts: 1, kind: syncproxy.FailureStatus, locations: []string{"", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				hop := calls % (tc.hop + 1)
				calls++
				if req.URL.String() != []string{source, canonical}[hop] {
					t.Fatal("wrong destination sent")
				}
				if hop == tc.hop {
					r := fixtureResponse(tc.status, "synthetic", "text/html")
					if tc.locations != nil {
						r.Header["Location"] = tc.locations
					}
					return r, nil
				}
				status := tc.aliasStatus
				if status == 0 {
					status = 307
				}
				r := fixtureResponse(status, "", "text/html")
				r.Header.Set("Location", canonical)
				return r, nil
			})
			c := testClient(t, transport, transport, transport, transport)
			result, err := c.Checkout(t.Context(), source, start)
			var failure *RequestError
			if !errors.As(err, &failure) || failure.Kind != tc.kind || failure.StatusCode != tc.status || result.unavailableEnrichment || result.regularFinal404Unavailable || c.RequestCount() != tc.attempts || calls != (tc.hop+1)*tc.attempts {
				t.Fatal("failure suppressed or retry changed", calls, err)
			}
		})
	}
	for _, target := range []string{strings.Replace(canonical, "ussel", "other", 1), strings.Replace(canonical, fmt.Sprint(start.Unix()), fmt.Sprint(start.Unix()+1), 1), canonical + "?", canonical + "#", strings.Replace(canonical, "reserver", "%72eserver", 1), "/ussel/reserver/F1/D1792155600/VF/2/"} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.String() != source {
					t.Fatal("invalid target sent")
				}
				r := fixtureResponse(307, "", "text/html")
				r.Header.Set("Location", target)
				return r, nil
			})
			result, err := c.Checkout(t.Context(), source, start)
			var failure *RequestError
			if !errors.As(err, &failure) || failure.Kind != syncproxy.FailureRedirect || result.unavailableEnrichment || calls != 1 {
				t.Fatal("invalid redirect not rejected before fetch", err)
			}
		})
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		return fixtureResponse(404, "synthetic", "text/html"), nil
	})
	if _, err := c.Get(t.Context(), OperationCheckout, source); err == nil {
		t.Fatal("unbound404 suppressed")
	}
	if result, err := c.Checkout(t.Context(), canonical, start); err == nil || result.unavailableEnrichment {
		t.Fatal("direct canonical404 suppressed")
	}
}

func TestRegularFinal404ConcurrentRetryIsolation(t *testing.T) {
	const count = 24
	start := gueretTestStart()
	var mu sync.Mutex
	attempts := map[string]int{}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		b := req.Context().Value(checkoutBindingKey{}).(checkoutBinding)
		u, _ := url.Parse(b.source)
		i, _ := strconv.Atoi(strings.TrimPrefix(u.Path, "/ussel/r/"))
		if req.URL.String() == b.source {
			mu.Lock()
			attempts[b.source]++
			n := attempts[b.source]
			mu.Unlock()
			if n == 1 {
				return fixtureResponse(503, "synthetic", "text/html"), nil
			}
			r := fixtureResponse(307, "", "text/html")
			target := regularTestCanonical(b.source, b.start)
			if i%3 == 2 {
				target = "https://achat.veocinemas.fr/ussel/reserver/"
			}
			r.Header.Set("Location", target)
			return r, nil
		}
		if i%3 == 0 {
			return fixtureResponse(404, "synthetic", "text/html"), nil
		}
		return fixtureResponse(200, "healthy", "text/html"), nil
	})
	// Each initial 503 cools its proxy. Keep enough distinct synthetic proxies
	// for every concurrent request's one retry, as in the Guéret control.
	transports := make([]roundTripFunc, count*2)
	for i := range transports {
		transports[i] = transport
	}
	c := testClient(t, transports...)
	var wg sync.WaitGroup
	for i := 1; i <= count; i++ {
		wg.Go(func() {
			forged := &checkoutResponse{unavailableEnrichment: true, regularFinal404Unavailable: true}
			ctx := context.WithValue(t.Context(), checkoutBindingKey{}, checkoutBinding{response: forged})
			result, err := c.Checkout(ctx, fmt.Sprintf("https://achat.veocinemas.fr/ussel/r/%d", i), start)
			if err != nil || result.unavailableEnrichment != (i%3 != 1) || result.regularFinal404Unavailable != (i%3 == 0) {
				t.Error("request/retry outcome leaked", i, err)
			}
			if i%3 == 1 && (string(result.Body) != "healthy" || result.canonicalURL == "" || result.pool.index == 0) {
				t.Error("healthy metadata lost")
			}
			if i%3 != 1 && (len(result.Body) != 0 || result.canonicalURL != "" || result.pool != (clientPoolPosition{})) {
				t.Error("unavailable has page facts")
			}
		})
	}
	wg.Wait()
	if len(attempts) != count || c.RequestCount() != count*2 {
		t.Fatal("wrong attempt count", c.RequestCount())
	}
	for _, n := range attempts {
		if n != 2 {
			t.Fatal("request did not retry exactly once", n)
		}
	}
}

func TestRegularFinal404CannotHealMismatchOrCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprint("canceled-", canceled), func(t *testing.T) {
			row := retryTestRow(9)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls, body := 0, &gueretUnreadBody{}
			c := testClient(t, func(req *http.Request) (*http.Response, error) {
				calls++
				canonical := regularTestCanonical(row.BookingURL, row.StartTime)
				if req.URL.String() == row.BookingURL {
					r := fixtureResponse(307, "", "text/html")
					r.Header.Set("Location", canonical)
					return r, nil
				}
				if !canceled && calls == 2 {
					return fixtureResponse(200, bookingFormFixture(strings.Replace(canonical, "F624618", "F624619", 1))+`<div id=s_fin>fin prévue à 20h15</div>`, "text/html"), nil
				}
				if canceled {
					cancel()
				}
				r := fixtureResponse(404, "", "text/html")
				r.Body = body
				return r, nil
			})
			g := checkoutClientFixture{client: c}
			rows, s := []schedule.ShowtimeRecord{row}, SyncSummary{}
			err := enrichCheckouts(ctx, g, rows, &s, row.StartTime.Add(-time.Hour))
			var rejected *CheckoutParseError
			if canceled && !errors.Is(err, context.Canceled) || !canceled && (!errors.As(err, &rejected) || rejected.Reason != RejectBookingForm) || !reflect.DeepEqual(rows[0], row) || s.RegularFinal404UnavailableCheckouts != 0 || s.GueretUnavailableCheckouts != 0 || s.CheckoutRecoveries != 0 || s.CheckoutRetryAttempts != btoi(!canceled) || calls != 2+2*btoi(!canceled) || body.reads != 0 || body.closes != 1 {
				t.Fatal("unavailable healed mismatch/cancel or counted failed enrichment", s, err)
			}
			// Started rows never request or count unavailable enrichment.
			if err := enrichCheckouts(t.Context(), g, rows, &SyncSummary{}, row.StartTime); err != nil || calls != 2+2*btoi(!canceled) {
				t.Fatal("started row fetched", err)
			}
		})
	}
}

func TestRegularFinal404SyncRetainsRowsAndIndependentCounters(t *testing.T) {
	g := newGueretFixture(t, func(op Operation, raw string, b []byte) []byte {
		b = []byte(strings.ReplaceAll(string(b), "P0001", "G0699"))
		if op == OperationSchedule && strings.HasPrefix(raw, MainOrigin) {
			u, _ := url.Parse(raw)
			slug := "fixture"
			if strings.Contains(u.Query().Get("theaters"), "G0699") {
				slug = "toulouse"
			}
			if strings.Contains(u.Query().Get("theaters"), "P0002") {
				slug = "ussel"
			}
			if strings.Contains(u.Query().Get("theaters"), "P0003") {
				slug = "entry"
			}
			b = []byte(strings.ReplaceAll(string(b), "/fixture/r/", "/"+slug+"/r/"))
		}
		return b
	})
	want, err := acquire(t.Context(), g, fixtureOptions(), &SyncSummary{})
	if err != nil {
		t.Fatal(err)
	}
	fatal := 0
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		b := req.Context().Value(checkoutBindingKey{}).(checkoutBinding)
		if schedule.ValidVeoGueretRelayURL(b.source) {
			hop := gueretHop(req.URL, b)
			if hop == 3 {
				return fixtureResponse(404, "synthetic", "text/html"), nil
			}
			r := fixtureResponse([]int{302, 302, 307}[hop], "", "text/html")
			r.Header.Set("Location", gueretTestChain(b.start)[hop+1])
			return r, nil
		}
		canonical := regularTestCanonical(b.source, b.start)
		ussel := strings.Contains(b.source, "/ussel/")
		if req.URL.String() == b.source {
			r := fixtureResponse(307, "", "text/html")
			if strings.Contains(b.source, "/entry/") {
				canonical = "https://achat.veocinemas.fr/entry/reserver/"
			}
			if fatal == 2 && ussel && b.start.Day() == 28 {
				canonical = strings.Replace(canonical, "/ussel/", "/other-cinema/", 1)
			}
			r.Header.Set("Location", canonical)
			return r, nil
		}
		if ussel {
			status := 404
			if fatal == 1 && b.start.Day() == 28 {
				status = 410
			}
			return fixtureResponse(status, "synthetic", "text/html"), nil
		}
		action := canonical
		if strings.Contains(b.source, "/toulouse/") {
			action = "https://achat.veocinemas.fr/toulouse/reserver/"
		}
		return fixtureResponse(200, bookingFormFixture(action)+`<div id=s_fin>fin prévue à 01h15</div><script>{"auditorium_showtime":"salle-2"}</script>`, "text/html"), nil
	})
	g.checkout = c.Checkout
	d, s, err := Sync(t.Context(), g, fixtureOptions())
	if err != nil || len(d.Theaters) != 20 || len(d.Showtimes) != len(want.Showtimes) || !reflect.DeepEqual(d.Coverage, want.Coverage) || d.Window != want.Window || s.RegularFinal404UnavailableCheckouts != 2 || s.GueretUnavailableCheckouts != 2 || s.ToulouseUnavailableCheckouts != 2 || s.CheckoutRetryAttempts != 0 || s.CheckoutRecoveries != 0 || s.Checkouts != len(d.Showtimes)-1 || s.ExactEnds != s.Checkouts-8 || c.RequestCount() != s.Checkouts {
		t.Fatal("dataset/counter isolation lost", s, err)
	}
	for i, row := range d.Showtimes {
		if row.TheaterID == "veo-G06DB" || row.TheaterID == "veo-P0002" || row.TheaterID == "veo-G0699" || row.TheaterID == "veo-P0003" {
			if !reflect.DeepEqual(row, want.Showtimes[i]) || !row.EndTime.Equal(row.StartTime) {
				t.Fatal("unavailable changed original row/room/booking or invented facts")
			}
		}
	}
	for _, kind := range []int{1, 2} {
		fatal = kind // Written only between completed Sync calls.
		d, _, err = Sync(t.Context(), g, fixtureOptions())
		var failure *RequestError
		wantKind := syncproxy.FailureStatus
		if kind == 2 {
			wantKind = syncproxy.FailureRedirect
		}
		if !errors.As(err, &failure) || failure.Kind != wantKind || len(d.Theaters) != 0 || len(d.Showtimes) != 0 {
			t.Fatal("fatal failure published partial dataset", err)
		}
	}
}
