package veo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/syncproxy"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureResponse(status int, body, media string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {media}}, Body: io.NopCloser(strings.NewReader(body))}
}
func testClient(t *testing.T, transports ...roundTripFunc) *Client {
	t.Helper()
	clients := make([]*http.Client, len(transports))
	for i, transport := range transports {
		clients[i] = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			response, err := transport(r)
			if response != nil {
				response.Request = r
			}
			return response, err
		}), Timeout: time.Second}
	}
	c, err := newClient(clients, func(context.Context, time.Duration) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestClientProxyOnlyRetriesAndGenericErrors(t *testing.T) {
	if _, err := NewClient(ClientConfig{Timeout: 10 * time.Second}); err == nil {
		t.Fatal("direct access allowed")
	}
	calls := 0
	transports := make([]roundTripFunc, 5)
	for i := range transports {
		transports[i] = func(r *http.Request) (*http.Response, error) {
			calls++
			return fixtureResponse(503, "secret-provider-body", "application/json"), nil
		}
	}
	c := testClient(t, transports...)
	_, err := c.Get(t.Context(), OperationCinemas, MainOrigin+CatalogPath)
	var req *RequestError
	if !errors.As(err, &req) || req.Kind != syncproxy.FailureServer || calls != 4 || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), MainOrigin) {
		t.Fatal(calls, err)
	}
	for _, status := range []int{403, 429} {
		calls = 0
		c := testClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			return fixtureResponse(status, "secret", "text/html"), nil
		})
		if _, err := c.Get(t.Context(), OperationCinemas, MainOrigin+CatalogPath); err == nil || calls != 1 {
			t.Fatal(err, calls)
		}
	}
}
func TestClientRequestBodyContentTypesAndCharsets(t *testing.T) {
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.Header.Get("Referer") != CasteliaURL || r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("invalid RPC request")
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != `{"jsonrpc":"2.0","id":1,"method":"get_prog","params":{"site_id":"EMS1361"}}` {
			t.Fatal(string(b))
		}
		return fixtureResponse(200, `{}`, "application/json"), nil
	})
	if _, err := c.Get(t.Context(), OperationCastelia, ProgramURL); err != nil {
		t.Fatal(err)
	}
	c = testClient(t, func(*http.Request) (*http.Response, error) {
		return fixtureResponse(200, "<div id=s_fin>fin pr\xe9vue \xe0 23h15</div>{\"auditorium_showtime\":null}", "text/html; charset=iso-8859-1"), nil
	})
	b, err := c.Get(t.Context(), OperationCheckout, "https://achat.veocinemas.fr/fixture/r/1")
	if err != nil {
		t.Fatal(err)
	}
	f, err := parseCheckout(b, "")
	if err != nil || !f.hasEnd {
		t.Fatal(f, err)
	}
	for _, fixture := range []struct {
		status      int
		body, media string
	}{
		{200, "{}", "text/plain"},
		{302, "{}", "application/json"},
		{200, strings.Repeat("a", MaxResponseBytes+1), "application/json"},
		{200, "<html>Just a moment... cloudflare challenge-platform</html>", "text/html"},
	} {
		response := fixtureResponse(fixture.status, fixture.body, fixture.media)
		defer func() { _ = response.Body.Close() }()
		c := testClient(t, func(*http.Request) (*http.Response, error) { return response, nil })
		if _, err := c.Get(t.Context(), OperationCinemas, MainOrigin+CatalogPath); err == nil {
			t.Fatal("unsafe response accepted")
		}
	}
}
func TestClientInvalidURLAndCancellation(t *testing.T) {
	c := testClient(t, func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	if _, err := c.Get(t.Context(), OperationMovies, "https://evil.test/"); err == nil || c.RequestCount() != 0 {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Get(ctx, OperationCinemas, MainOrigin+CatalogPath); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCheckoutRejectsRedirectWithoutDecodingOrFollowing(t *testing.T) {
	for _, location := range []string{"https://achat.veocinemas.fr/fixture/r/2", "https://evil.test/", "https://achat.veocinemas.fr/index.php?id=1"} {
		calls := 0
		c := testClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			r := fixtureResponse(http.StatusTemporaryRedirect, "", "text/html; charset=iso-8859-1")
			r.Header.Set("Location", location)
			return r, nil
		})
		_, err := c.Get(t.Context(), OperationCheckout, "https://achat.veocinemas.fr/fixture/r/1")
		var failure *RequestError
		if !errors.As(err, &failure) || failure.Kind != syncproxy.FailureRedirect || calls != 1 {
			t.Fatalf("redirect incorrectly retried/decoded: calls=%d err=%v", calls, err)
		}
	}
}

func TestCheckoutBoundCanonicalRedirect(t *testing.T) {
	start := time.Unix(1792065000, 0)
	const source = "https://achat.veocinemas.fr/andernos/r/55880"
	for _, version := range []string{"VF", "VO"} {
		t.Run(version, func(t *testing.T) {
			calls := 0
			jar, _ := cookiejar.New(nil)
			u, _ := url.Parse(source)
			jar.SetCookies(u, []*http.Cookie{{Name: "must-not-send", Value: "private"}})
			client, err := newClient([]*http.Client{{Jar: jar, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.Header.Get("Cookie") != "" {
					t.Fatal("checkout sent cookies or non-GET")
				}
				response := fixtureResponse(200, `<div id=s_fin>fin prévue à 15h45</div>{"auditorium_showtime":"salle-2"}`, "text/html; charset=utf-8")
				if calls == 1 {
					if r.URL.String() != source {
						t.Fatal("wrong source")
					}
					response = fixtureResponse(307, "", "text/html; charset=iso-8859-1")
					response.Header.Set("Location", "https://achat.veocinemas.fr/andernos/reserver/F604912/D1792065000/"+version+"/271793/")
					response.Header.Add("Set-Cookie", "must-not-forward=private; Path=/")
				}
				response.Request = r
				return response, nil
			})}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			b, err := client.Checkout(t.Context(), source, start)
			if err != nil || calls != 2 || client.RequestCount() != 1 {
				t.Fatalf("canonical redirect: calls=%d err=%v", calls, err)
			}
			facts, err := parseCheckout(b.Body, b.canonicalURL)
			if err != nil || !facts.hasEnd || facts.room != "Salle 2" {
				t.Fatal("canonical body not parsed", err)
			}
		})
	}
}

func TestCheckoutRejectsUnboundRedirectTargets(t *testing.T) {
	const source = "https://achat.veocinemas.fr/andernos/r/55880"
	const destination = "https://achat.veocinemas.fr/andernos/reserver/F604912/D1792065000/VF/271793/"
	for _, target := range []string{
		destination + "?", destination + "?x=", destination + "#", destination + "#x",
		strings.Replace(destination, "https:", "http:", 1),
		strings.Replace(destination, "achat.veocinemas.fr", "evil.test", 1),
		strings.Replace(destination, "achat.veocinemas.fr", "achat.veocinemas.fr:443", 1),
		strings.Replace(destination, "https://", "https://user:secret@", 1),
		strings.Replace(destination, "/andernos/", "/other-cinema/", 1),
		strings.Replace(destination, "D1792065000", "D1792065001", 1),
		strings.Replace(destination, "D1792065000", "D17920650000", 1),
		strings.Replace(destination, "F604912", "F0", 1),
		strings.Replace(destination, "271793", "0", 1),
		strings.Replace(destination, "/VF/", "/VOSTF/", 1),
		strings.Replace(destination, "/andernos/", "/andernos--test/", 1),
		strings.Replace(destination, "/andernos/", "/Andernos/", 1),
		strings.Replace(destination, "/andernos/", "/%61ndernos/", 1),
		strings.Replace(destination, "/andernos/", "/andernos/../andernos/", 1),
		strings.Replace(destination, "/andernos/", "/andernos%20/", 1),
		strings.Replace(destination, "/andernos/", "/andernos%5c/", 1),
		strings.TrimSuffix(destination, "/"), source, "https://achat.veocinemas.fr/andernos/r/55881",
	} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				r := fixtureResponse(307, "", "text/html")
				r.Header.Set("Location", target)
				return r, nil
			})
			_, err := c.Checkout(t.Context(), source, time.Unix(1792065000, 0))
			var failure *RequestError
			if !errors.As(err, &failure) || failure.Kind != syncproxy.FailureRedirect || calls != 1 {
				t.Fatalf("unsafe redirect followed/retried: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestCheckoutRedirectHopLimitAndDirectDestinationRejection(t *testing.T) {
	const source = "https://achat.veocinemas.fr/andernos/r/55880"
	const destination = "https://achat.veocinemas.fr/andernos/reserver/F604912/D1792065000/VF/271793/"
	calls := 0
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		r := fixtureResponse(307, "", "text/html")
		r.Header.Set("Location", destination)
		return r, nil
	})
	_, err := c.Checkout(t.Context(), source, time.Unix(1792065000, 0))
	var failure *RequestError
	if !errors.As(err, &failure) || failure.Kind != syncproxy.FailureRedirect || calls != 2 {
		t.Fatal("second hop allowed or retried", calls, err)
	}
	for _, start := range []time.Time{time.Time{}, time.Unix(1792065000, 1)} {
		if _, err := c.Checkout(t.Context(), source, start); err == nil || calls != 2 {
			t.Fatal("invalid session start accepted", err)
		}
	}
	if _, err := c.Checkout(t.Context(), destination, time.Unix(1792065000, 0)); err == nil || calls != 2 {
		t.Fatal("internal destination accepted as source", err)
	}
}

func TestCheckoutCanonicalResponseStillValidated(t *testing.T) {
	const source = "https://achat.veocinemas.fr/andernos/r/55880"
	for _, fixture := range []struct {
		status      int
		body, media string
	}{
		{200, "not HTML", "text/plain"},
		{200, strings.Repeat("a", MaxHTMLBytes+1), "text/html"},
		{403, "denied", "text/html"},
		{200, "Just a moment... cloudflare challenge-platform", "text/html"},
	} {
		response := fixtureResponse(fixture.status, fixture.body, fixture.media)
		defer func() { _ = response.Body.Close() }()
		calls := 0
		c := testClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				r := fixtureResponse(307, "", "text/html")
				r.Header.Set("Location", "https://achat.veocinemas.fr/andernos/reserver/F604912/D1792065000/VF/271793/")
				return r, nil
			}
			return response, nil
		})
		if _, err := c.Checkout(t.Context(), source, time.Unix(1792065000, 0)); err == nil || calls != 2 {
			t.Fatal("unsafe final response accepted", calls, err)
		}
	}
}

func TestCheckoutAlternateSourceBinding(t *testing.T) {
	start := time.Unix(1792065000, 0)
	for _, slug := range []string{"chateaurenard", "labarthe-sur-leze"} {
		for _, segment := range []string{"", "reserver/"} {
			source := "https://achat.veocinemas.fr/" + slug + "/" + segment + "r/22309"
			destination := "https://achat.veocinemas.fr/" + slug + "/reserver/F1/D1792065000/VO/1/"
			for _, target := range []string{destination, strings.Replace(destination, slug, "other-cinema", 1), strings.Replace(destination, "1792065000", "1792065001", 1), destination + "?", destination + "#"} {
				calls := 0
				c := testClient(t, func(r *http.Request) (*http.Response, error) {
					calls++
					if calls == 1 {
						if r.URL.String() != source {
							t.Fatal("source bytes changed")
						}
						response := fixtureResponse(307, "", "text/html; charset=iso-8859-1")
						response.Header.Set("Location", target)
						return response, nil
					}
					if r.URL.String() != destination {
						t.Fatal("unsafe destination followed")
					}
					return fixtureResponse(200, `<div id=s_fin>fin prévue à 15h45</div>{"auditorium_showtime":"salle-1"}`, "text/html; charset=utf-8"), nil
				})
				body, err := c.Checkout(t.Context(), source, start)
				if target != destination {
					var failure *RequestError
					if !errors.As(err, &failure) || failure.Kind != syncproxy.FailureRedirect || calls != 1 {
						t.Fatal("alternate source weakened redirect binding", calls, err)
					}
					continue
				}
				facts, parseErr := parseCheckout(body.Body, body.canonicalURL)
				if err != nil || parseErr != nil || calls != 2 || !facts.hasEnd || facts.room != "Salle 1" {
					t.Fatal("alternate source did not parse exact end/room", calls, err, parseErr)
				}
			}
		}
	}
}

func TestCheckoutCanonicalMetadataIsPerRequestAndRetry(t *testing.T) {
	const origin = "https://achat.veocinemas.fr"
	const count = 24
	var mu sync.Mutex
	attempts := map[int]int{}
	actualFinalPool := map[int]int{}
	destination := func(id int) string {
		return origin + "/toulouse/reserver/F" + strconv.Itoa(id) + "/D1792065000/VF/81/"
	}
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		parts := strings.Split(r.URL.Path, "/")
		if parts[2] == "r" {
			id, _ := strconv.Atoi(parts[3])
			mu.Lock()
			attempts[id]++
			attempt := attempts[id]
			mu.Unlock()
			if attempt > 1 {
				id += 1000
			}
			response := fixtureResponse(307, "", "text/html")
			response.Header.Set("Location", destination(id))
			return response, nil
		}
		id, _ := strconv.Atoi(strings.TrimPrefix(parts[3], "F"))
		if id <= 1000 {
			return fixtureResponse(503, "temporarily unavailable", "text/html"), nil
		}
		bodyTarget := id
		if id%2 != 0 {
			// Same cinema/start/tail, but the first attempt's film is stale.
			bodyTarget -= 1000
		}
		return fixtureResponse(200, bookingFormFixture(destination(bodyTarget)), "text/html"), nil
	})
	transports := make([]roundTripFunc, count*2)
	for i := range transports {
		transports[i] = func(r *http.Request) (*http.Response, error) {
			response, err := transport(r)
			if response != nil && response.StatusCode == http.StatusOK {
				id, _ := strconv.Atoi(strings.TrimPrefix(strings.Split(r.URL.Path, "/")[3], "F"))
				mu.Lock()
				actualFinalPool[id] = i + 1
				mu.Unlock()
			}
			return response, err
		}
	}
	c := testClient(t, transports...)
	var wg sync.WaitGroup
	for id := 1; id <= count; id++ {
		wg.Go(func() {
			result, err := c.Checkout(t.Context(), origin+"/toulouse/r/"+strconv.Itoa(id), time.Unix(1792065000, 0))
			if err != nil || result.canonicalURL != destination(id+1000) {
				t.Errorf("request %d lost trusted final identity: %v", id, err)
				return
			}
			mu.Lock()
			actual := actualFinalPool[id+1000]
			mu.Unlock()
			if result.pool.count != count*2 || result.pool.index != actual || actual < 1 {
				t.Error("actual pool position missing")
			}
			_, err = parseCheckout(result.Body, result.canonicalURL)
			if (err == nil) != (id%2 == 0) {
				t.Errorf("request %d used another response's canonical identity", id)
			}
		})
	}
	wg.Wait()
	if c.RequestCount() != count*2 {
		t.Fatal("retry count changed", c.RequestCount())
	}
	for id, attempts := range attempts {
		if attempts != 2 {
			t.Fatal("wrong source retry count", id, attempts)
		}
	}
}

func TestCheckoutBodyCannotSupplyTrustedCanonicalURL(t *testing.T) {
	const canonical = "https://achat.veocinemas.fr/toulouse/reserver/F324508/D1792065000/VF/81/"
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		return fixtureResponse(200, bookingFormFixture(canonical), "text/html"), nil
	})
	result, err := c.Checkout(t.Context(), "https://achat.veocinemas.fr/toulouse/r/8106", time.Unix(1792065000, 0))
	if err != nil || result.canonicalURL != "" {
		t.Fatal("body invented redirect metadata", err)
	}
	if result.pool != (clientPoolPosition{1, 1}) {
		t.Fatal("noncanonical successful response lost pool position")
	}
	if _, err := parseCheckout(result.Body, result.canonicalURL); err == nil {
		t.Fatal("direct source response falsely vouched for a body-chosen destination")
	}
}

func TestCheckoutUnavailableEntryIsExactAndNeverFollowed(t *testing.T) {
	const entry = "https://achat.veocinemas.fr/toulouse/reserver/"
	for _, source := range []string{"https://achat.veocinemas.fr/toulouse/r/8045", "https://achat.veocinemas.fr/toulouse/reserver/r/8045"} {
		calls := 0
		c := testClient(t, func(r *http.Request) (*http.Response, error) {
			calls++
			if r.URL.String() != source {
				t.Fatal("entry destination requested")
			}
			response := fixtureResponse(307, `not a session page; {"auditorium_showtime":BROKEN}`, "text/plain")
			response.Header.Set("Location", entry)
			return response, nil
		})
		result, err := c.Checkout(t.Context(), source, time.Unix(1792065000, 0))
		if err != nil || !result.unavailableEnrichment || len(result.Body) != 0 || result.canonicalURL != "" || calls != 1 || c.RequestCount() != 1 {
			t.Fatal("unavailable outcome fabricated a page, followed or retried", calls, err)
		}
	}
	u, _ := url.Parse(entry)
	if checkoutFetchURLAllowed(u) || checkoutURLAllowed(u) || allowedURL(u) {
		t.Fatal("cinema entry added to URL authority")
	}
}

func TestCheckoutUnavailableEntryNearMissesReject(t *testing.T) {
	const source = "https://achat.veocinemas.fr/toulouse/r/8045"
	const entry = "https://achat.veocinemas.fr/toulouse/reserver/"
	locations := []string{
		"", entry + "?", entry + "?id=1", entry + "#", entry + "#session", "/toulouse/reserver/",
		"//achat.veocinemas.fr/toulouse/reserver/", "http://achat.veocinemas.fr/toulouse/reserver/",
		"https://other.test/toulouse/reserver/", "https://achat.veocinemas.fr/other/reserver/",
		"https://achat.veocinemas.fr/toulouse/reserver", entry + "/", entry + "session/",
		"https://achat.veocinemas.fr/toulouse//reserver/", "https://achat.veocinemas.fr/toulouse/./reserver/",
		"https://achat.veocinemas.fr/other/../toulouse/reserver/", "https://achat.veocinemas.fr/toulouse/reserver%2f",
		"https://achat.veocinemas.fr/%74oulouse/reserver/", "https://achat.veocinemas.fr/toulouse/Reserver/",
		"https://achat.veocinemas.fr:443/toulouse/reserver/", "https://user@achat.veocinemas.fr/toulouse/reserver/",
		"https://achat.veocinemas.fr./toulouse/reserver/", "https://ACHAT.veocinemas.fr/toulouse/reserver/",
		entry + " ", entry + "\n", "https://[broken",
	}
	for _, status := range []int{301, 302, 303, 308} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			assertUnavailableEntryRejected(t, source, entry, status, false)
		})
	}
	for i, location := range locations {
		t.Run("location-"+strconv.Itoa(i), func(t *testing.T) {
			assertUnavailableEntryRejected(t, source, location, 307, false)
		})
	}
	t.Run("multiple-locations", func(t *testing.T) {
		assertUnavailableEntryRejected(t, source, entry, 307, true)
	})
	t.Run("second-hop", func(t *testing.T) {
		calls := 0
		c := testClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			r := fixtureResponse(307, "", "text/html")
			r.Header.Set("Location", entry)
			if calls == 1 {
				r.Header.Set("Location", entry+"F123/D1792065000/VF/81/")
			}
			return r, nil
		})
		result, err := c.Checkout(t.Context(), source, time.Unix(1792065000, 0))
		if err == nil || result.unavailableEnrichment || calls != 2 || c.RequestCount() != 1 {
			t.Fatal("second-hop entry converted or followed", calls, err)
		}
	})
}

func assertUnavailableEntryRejected(t *testing.T, source, location string, status int, multiple bool) {
	t.Helper()
	calls := 0
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		r := fixtureResponse(status, "not a session", "text/html")
		r.Header.Set("Location", location)
		if multiple {
			r.Header.Add("Location", location)
		}
		return r, nil
	})
	result, err := c.Checkout(t.Context(), source, time.Unix(1792065000, 0))
	if err == nil || result.unavailableEnrichment || calls != 1 || c.RequestCount() != 1 {
		t.Fatal("near-miss accepted, followed or retried", calls, err)
	}
}

func TestCheckoutUnavailableConcurrentRetriesAreIsolated(t *testing.T) {
	const count = 24
	var mu sync.Mutex
	attempts := make(map[string]int)
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		parts := strings.Split(r.URL.Path, "/")
		id, _ := strconv.Atoi(parts[len(parts)-1])
		if checkoutDestination.MatchString(r.URL.Path) {
			return fixtureResponse(200, bookingFormFixture(r.URL.String()), "text/html"), nil
		}
		mu.Lock()
		attempts[r.URL.Path]++
		attempt := attempts[r.URL.Path]
		mu.Unlock()
		if attempt == 1 {
			return fixtureResponse(500, "retry", "text/html"), nil
		}
		response := fixtureResponse(307, "", "text/html")
		location := "https://achat.veocinemas.fr/fixture/reserver/"
		if id%2 == 0 {
			location += "F123/D1792065000/VF/81/"
		}
		response.Header.Set("Location", location)
		return response, nil
	})
	transports := make([]roundTripFunc, count*2)
	for i := range transports {
		transports[i] = transport
	}
	c := testClient(t, transports...)
	var wg sync.WaitGroup
	for id := 1; id <= count; id++ {
		wg.Go(func() {
			result, err := c.Checkout(t.Context(), "https://achat.veocinemas.fr/fixture/r/"+strconv.Itoa(id), time.Unix(1792065000, 0))
			if err != nil || result.unavailableEnrichment != (id%2 != 0) {
				t.Errorf("request %d mixed unavailable outcomes: %v", id, err)
				return
			}
			if id%2 == 0 {
				if _, err := parseCheckout(result.Body, result.canonicalURL); err != nil {
					t.Errorf("request %d lost canonical identity: %v", id, err)
				}
			} else if result.canonicalURL != "" || len(result.Body) != 0 {
				t.Error("unavailable outcome retained canonical metadata")
			}
		})
	}
	wg.Wait()
	if c.RequestCount() != count*2 {
		t.Fatal("unexpected retries", c.RequestCount())
	}
	for _, n := range attempts {
		if n != 2 {
			t.Fatal("wrong retry count", n)
		}
	}
}

func TestCheckoutUnavailableDoesNotConvertCancellationOrFailures(t *testing.T) {
	for _, kind := range []string{"canceled-entry", "canceled-success", "transport", "challenge", "status"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				switch kind {
				case "canceled-entry":
					cancel()
					r := fixtureResponse(307, "", "text/html")
					r.Header.Set("Location", "https://achat.veocinemas.fr/toulouse/reserver/")
					return r, nil
				case "canceled-success":
					cancel()
					return fixtureResponse(200, "<form></form>", "text/html"), nil
				case "transport":
					return nil, errors.New("fixture transport error")
				case "challenge":
					return fixtureResponse(403, "challenge", "text/html"), nil
				default:
					return fixtureResponse(404, "not found", "text/html"), nil
				}
			})
			c := testClient(t, transport, transport, transport, transport)
			result, err := c.Checkout(ctx, "https://achat.veocinemas.fr/toulouse/r/8045", time.Unix(1792065000, 0))
			if err == nil || result.unavailableEnrichment || result.pool != (clientPoolPosition{}) || strings.HasPrefix(kind, "canceled-") && !errors.Is(err, context.Canceled) {
				t.Fatal("error became unavailable success", err)
			}
		})
	}
}

func TestCheckoutTransportClearsEarlierResponseDiagnostics(t *testing.T) {
	const source = "https://achat.veocinemas.fr/toulouse/r/8170"
	cell := &checkoutResponse{canonicalURL: "earlier", unavailableEnrichment: true, pool: clientPoolPosition{1, 5}}
	ctx := context.WithValue(t.Context(), checkoutBindingKey{}, checkoutBinding{source: source, start: time.Unix(1792065000, 0), response: cell})
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := htmlTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return fixtureResponse(503, "unavailable", "text/html"), nil
	}), pool: clientPoolPosition{2, 5}}
	response, err := transport.RoundTrip(r)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil || *cell != (checkoutResponse{}) {
		t.Fatal("earlier successful hop/retry metadata retained")
	}
}
