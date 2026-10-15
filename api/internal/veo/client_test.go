package veo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
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
	f, err := parseCheckout(b)
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
			facts, err := parseCheckout(b)
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
