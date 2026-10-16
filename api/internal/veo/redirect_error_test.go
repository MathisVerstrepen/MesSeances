package veo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

func TestCheckoutRedirectFactsFromExactRejectedResponse(t *testing.T) {
	start := gueretTestStart()
	chain := gueretTestChain(start)
	regular := "https://achat.veocinemas.fr/fixture/r/123"
	canonical := fmt.Sprintf("https://achat.veocinemas.fr/fixture/reserver/F34/D%d/VF/78/", start.Unix())
	for _, tc := range []struct {
		name, branch, reason, host, route string
		hop, status, count                int
		location                          string
	}{
		{"regular-parse", "regular", "raw_location_parse", "unknown", "unknown", 0, 302, 1, "https://%zz/secret"},
		{"regular-host", "regular", "unsafe_authority", "other", "other", 0, 302, 1, "https://user:secret@foreign.test/private?opaque=secret"},
		{"regular-route", "regular", "route", "regular", "other", 0, 302, 1, "https://achat.veocinemas.fr/private-secret"},
		{"regular-query", "regular", "query", "regular", "canonical", 0, 302, 1, canonical + "?opaque=secret"},
		{"regular-cinema", "regular", "cinema_binding", "regular", "canonical", 0, 302, 1, strings.Replace(canonical, "/fixture/", "/different/", 1)},
		{"regular-epoch", "regular", "epoch_binding", "regular", "canonical", 0, 302, 1, strings.Replace(canonical, fmt.Sprint(start.Unix()), fmt.Sprint(start.Unix()+1), 1)},
		{"regular-fragment", "regular", "representation", "regular", "canonical", 0, 302, 1, canonical + "#secret"},
		{"gueret-status", "gueret", "status", "relay", "relay_root", 0, 307, 1, chain[1]},
		{"gueret-missing", "gueret", "location_count", "relay", "relay_source", 0, 302, 0, ""},
		{"gueret-many", "gueret", "location_count", "relay", "relay_root", 0, 302, 9, chain[1]},
		{"gueret-parse", "gueret", "raw_location_parse", "unknown", "unknown", 0, 302, 1, "https://%zz/secret"},
		{"gueret-day", "gueret", "date_binding", "relay", "relay_root", 0, 302, 1, strings.Replace(chain[1], "20261016", "20261017", 1)},
		{"gueret-clock", "gueret", "time_binding", "relay", "relay_root", 0, 302, 1, strings.Replace(chain[1], "15:00", "15:01", 1)},
		{"gueret-query", "gueret", "query", "relay", "relay_root", 0, 302, 1, chain[1] + "&opaque=secret"},
		{"gueret-skip", "gueret", "hop", "dedicated", "alias", 0, 302, 1, chain[2]},
		{"gueret-host", "gueret", "unsafe_authority", "other", "other", 1, 302, 1, "https://foreign.test/private-secret"},
		{"gueret-route", "gueret", "route", "dedicated", "other", 1, 302, 1, "https://achat.cinema-senechal.com/private-secret"},
		{"gueret-epoch", "gueret", "epoch_binding", "dedicated", "canonical", 2, 307, 1, strings.Replace(chain[3], fmt.Sprint(start.Unix()), fmt.Sprint(start.Unix()+1), 1)},
		{"gueret-fourth", "gueret", "hop", "dedicated", "canonical", 3, 302, 1, chain[3]},
		{"gueret-early-ok", "gueret", "status", "relay", "relay_source", 0, 200, 0, ""},
		{"gueret-final-location", "gueret", "location_count", "dedicated", "canonical", 3, 200, 1, chain[3]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(req *http.Request) (*http.Response, error) {
				hop := calls
				calls++
				if hop > tc.hop {
					t.Fatal("rejected target sent")
				}
				if hop < tc.hop {
					r := fixtureResponse([]int{302, 302, 307}[hop], "", "text/html")
					r.Header.Set("Location", chain[hop+1])
					return r, nil
				}
				r := fixtureResponse(tc.status, "private-secret", "text/html")
				for i := 0; i < tc.count; i++ {
					r.Header.Add("Location", tc.location)
				}
				return r, nil
			})
			source, cinema := regular, "P1028"
			if tc.branch == "gueret" {
				source, cinema = chain[0], "G06DB"
			}
			_, err := c.Checkout(t.Context(), source, start)
			var rejected *RequestError
			if !errors.As(err, &rejected) || rejected.Kind != syncproxy.FailureRedirect || calls != tc.hop+1 || c.RequestCount() != 1 {
				t.Fatal("classification/request budget changed", calls, err)
			}
			row := redirectTestRow(source, start, cinema)
			err = identifyCheckoutRequestError(err, row)
			if !errors.As(err, &rejected) {
				t.Fatal("RequestError lost")
			}
			d, ok := rejected.RedirectFacts()
			if !ok || d.Branch != tc.branch || d.Phase != "response" || d.Reason != tc.reason || !d.HopKnown || d.Hop != tc.hop || d.HTTPStatus != tc.status || d.LocationCount != min(tc.count, 4) || d.LocationCountTruncated != (tc.count > 4) || d.ObservedHost != tc.host || d.ObservedRoute != tc.route || d.CinemaID != cinema || d.ShowingID != row.ProviderShowingID || d.PoolPosition != 1 || d.PoolSize != 1 {
				t.Fatalf("wrong exact-response facts: %+v", d)
			}
			wire, _ := json.Marshal(d)
			for _, forbidden := range []string{"secret", source, canonical, chain[1], "Location=", "opaque="} {
				if strings.Contains(string(wire)+err.Error(), forbidden) {
					t.Fatal("provider values retained")
				}
			}
			d.Reason = "forged-secret"
			copy, _ := rejected.RedirectFacts()
			if copy.Reason != tc.reason {
				t.Fatal("diagnostic copy aliases error")
			}
		})
	}
}

func redirectTestRow(source string, start time.Time, cinema string) schedule.ShowtimeRecord {
	return schedule.ShowtimeRecord{TheaterID: "veo-" + cinema, ProviderShowingID: schedule.VeoShowingID(cinema, "synthetic"), BookingURL: source, StartTime: start}
}

func TestCheckoutRedirectRequestAndCallbackFacts(t *testing.T) {
	start := gueretTestStart()
	source := gueretTestChain(start)[0]
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("misbound source sent")
		return nil, errShape
	})
	_, err := c.Checkout(t.Context(), source, start.Add(time.Minute))
	var rejected *RequestError
	if !errors.As(err, &rejected) {
		t.Fatal(err)
	}
	d, ok := rejected.RedirectFacts()
	if !ok || d.Phase != "request" || d.Reason != "source_binding" || d.HopKnown || d.ExpectedHost != "unknown" || d.HTTPStatus != 0 || d.LocationCount != 0 {
		t.Fatal(d)
	}
	canonical := fmt.Sprintf("https://achat.veocinemas.fr/fixture/reserver/F34/D%d/VF/78/", start.Unix())
	calls := 0
	c = testClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		r := fixtureResponse(302, "", "text/html")
		r.Header.Set("Location", canonical)
		return r, nil
	})
	_, err = c.Checkout(t.Context(), "https://achat.veocinemas.fr/fixture/r/123", start)
	if !errors.As(err, &rejected) || calls != 2 {
		t.Fatal("main hop limit changed", calls, err)
	}
	d, ok = rejected.RedirectFacts()
	if !ok || d.Branch != "regular" || d.Phase != "callback" || d.Reason != "hop" || d.Hop != 1 || !d.HopKnown || d.HTTPStatus != 302 || d.PoolPosition != 1 {
		t.Fatal(d)
	}
}

func TestCheckoutRedirectIdentityAttestationAndCancellation(t *testing.T) {
	start := gueretTestStart()
	source := gueretTestChain(start)[0]
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		r := fixtureResponse(302, "", "text/html")
		r.Header.Set("Location", "https://foreign.test/private-secret")
		return r, nil
	})
	_, actual := c.Checkout(t.Context(), source, start)
	row := redirectTestRow(source, start, "G06DB")
	for _, mutate := range []func(*schedule.ShowtimeRecord){
		func(r *schedule.ShowtimeRecord) { r.TheaterID = "secret" },
		func(r *schedule.ShowtimeRecord) { r.ProviderShowingID = schedule.VeoShowingID("P1028", "synthetic") },
		func(r *schedule.ShowtimeRecord) { r.BookingURL += "&secret=1" },
		func(r *schedule.ShowtimeRecord) { r.StartTime = r.StartTime.Add(time.Minute) },
		func(r *schedule.ShowtimeRecord) { *r = redirectTestRow(source, start, "P1028") },
	} {
		forged := row
		mutate(&forged)
		var rejected *RequestError
		if !errors.As(identifyCheckoutRequestError(actual, forged), &rejected) {
			t.Fatal("type lost")
		}
		d, ok := rejected.RedirectFacts()
		if !ok || d.CinemaID != "" || d.ShowingID != "" {
			t.Fatal("forged row identified")
		}
	}
	forged := &RequestError{Operation: OperationCheckout, Kind: syncproxy.FailureRedirect}
	if _, ok := forged.RedirectFacts(); ok || !errors.Is(identifyCheckoutRequestError(forged, row), forged) {
		t.Fatal("getter forged attestation")
	}
	wrapped := fmt.Errorf("outer cancellation: %w", errors.Join(actual, context.Canceled))
	if !errors.Is(identifyCheckoutRequestError(wrapped, row), context.Canceled) {
		t.Fatal("caller wrapper cancellation lost")
	}
	for _, cancellation := range []error{context.Canceled, context.DeadlineExceeded} {
		ctx, cancel := context.WithCancel(t.Context())
		c = testClient(t, func(*http.Request) (*http.Response, error) {
			cancel()
			return nil, cancellation
		})
		_, err := c.Checkout(ctx, source, start)
		var rejected *RequestError
		if !errors.Is(err, context.Canceled) || !errors.As(err, &rejected) || rejected.Kind != syncproxy.FailureCanceled {
			t.Fatal("cancellation precedence lost", err)
		}
		if _, ok := rejected.RedirectFacts(); ok {
			t.Fatal("cancellation retained rejection")
		}
	}
}

func TestCheckoutRedirectEnrichmentIdentityAndForgedContext(t *testing.T) {
	start := gueretTestStart()
	source := gueretTestChain(start)[0]
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		r := fixtureResponse(302, "", "text/html")
		r.Header.Set("Location", "https://foreign.test/private-secret")
		return r, nil
	})
	g := newGueretFixture(t, nil)
	g.checkout = c.Checkout
	row := redirectTestRow(source, start, "G06DB")
	err := enrichCheckouts(t.Context(), g, []schedule.ShowtimeRecord{row}, &SyncSummary{}, start.Add(-time.Hour))
	var rejected *RequestError
	if !errors.As(err, &rejected) {
		t.Fatal(err)
	}
	d, ok := rejected.RedirectFacts()
	if !ok || d.CinemaID != "G06DB" || d.ShowingID != row.ProviderShowingID || c.RequestCount() != 1 {
		t.Fatal("final worker error lost identity", d)
	}
	forged := &checkoutResponse{redirect: &checkoutRedirectDiagnostic{facts: CheckoutRedirectFacts{Reason: "private-secret", PoolPosition: 999}}}
	ctx := context.WithValue(t.Context(), checkoutBindingKey{}, checkoutBinding{source: "private-secret", response: forged})
	_, err = c.Checkout(ctx, source, start)
	if !errors.As(err, &rejected) {
		t.Fatal(err)
	}
	d, ok = rejected.RedirectFacts()
	if !ok || d.Reason != "unsafe_authority" || d.PoolPosition != 1 || d.CinemaID != "" {
		t.Fatal("caller context forged transport facts", d)
	}
}

func TestCheckoutRedirectConcurrentAndRetryIsolation(t *testing.T) {
	start := gueretTestStart()
	var wg sync.WaitGroup
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		b := req.Context().Value(checkoutBindingKey{}).(checkoutBinding)
		r := fixtureResponse(302, "", "text/html")
		chain := gueretTestChain(b.start)
		if b.start.Minute()%2 == 0 {
			r.Header.Set("Location", strings.Replace(chain[1], b.start.Format("20060102"), "20261017", 1))
		} else {
			r.Header.Set("Location", chain[2])
		}
		return r, nil
	})
	for i := 0; i < 32; i++ {
		wg.Go(func() {
			s := start.Add(time.Duration(i) * time.Minute)
			_, err := c.Checkout(t.Context(), gueretTestChain(s)[0], s)
			var rejected *RequestError
			if !errors.As(err, &rejected) {
				t.Error(err)
				return
			}
			d, ok := rejected.RedirectFacts()
			want := "date_binding"
			if i%2 != 0 {
				want = "hop"
			}
			if !ok || d.Reason != want || d.Hop != 0 || d.LocationCount != 1 {
				t.Error("request facts crossed", d)
			}
		})
	}
	wg.Wait()
	for _, finalStatus := range []int{302, 403} {
		calls := 0
		c = testClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			return fixtureResponse(503, "", "text/html"), nil
		}, func(*http.Request) (*http.Response, error) {
			calls++
			r := fixtureResponse(finalStatus, "synthetic", "text/html")
			r.Header.Set("Location", "https://foreign.test/private-secret")
			return r, nil
		})
		_, err := c.Checkout(t.Context(), gueretTestChain(start)[0], start)
		var rejected *RequestError
		if !errors.As(err, &rejected) || calls != 2 {
			t.Fatal("retry count changed", calls, err)
		}
		d, ok := rejected.RedirectFacts()
		if finalStatus == 302 && (!ok || d.PoolPosition != 2 || d.PoolSize != 2 || d.HTTPStatus != 302) || finalStatus == 403 && ok {
			t.Fatal("retry metadata stale", d)
		}
	}
}

func TestCheckoutRedirectResponseIdentityFacts(t *testing.T) {
	start := gueretTestStart()
	for _, missing := range []bool{false, true} {
		c, err := newClient([]*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			r := fixtureResponse(200, "synthetic", "text/html")
			if !missing {
				u, _ := url.Parse("https://foreign.test/private-secret")
				r.Request = &http.Request{URL: u}
			}
			return r, nil
		})}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Checkout(t.Context(), "https://achat.veocinemas.fr/fixture/r/123", start)
		var rejected *RequestError
		if !errors.As(err, &rejected) || rejected.Kind != syncproxy.FailureRedirect {
			t.Fatal("final response classification changed", err)
		}
		d, ok := rejected.RedirectFacts()
		want := "response_binding"
		if missing {
			want = "response_identity_unknown"
		}
		if !ok || d.Reason != want || d.HTTPStatus != 200 || !missing && d.ObservedHost != "other" {
			t.Fatal(d)
		}
	}
}
