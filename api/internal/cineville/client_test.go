package cineville

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/syncproxy"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := f(r)
	if response != nil && response.Request == nil {
		response.Request = r
	}
	return response, err
}
func response(status int, media, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {media}}, Body: io.NopCloser(strings.NewReader(body))}
}
func testClient(t *testing.T, roundTrip roundTripFunc) *Client {
	t.Helper()
	clients := make([]*http.Client, 4)
	for i := range clients {
		clients[i] = &http.Client{Transport: roundTrip}
	}
	c, err := newClient(clients, 0, func(context.Context, time.Duration) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestClientResponseClassificationAndRedaction(t *testing.T) {
	for _, v := range []struct {
		status      int
		media, body string
		kind        syncproxy.FailureKind
		attempts    int
	}{
		{403, "text/html", "synthetic-private", syncproxy.FailureChallenge, 1},
		{429, "text/html", "synthetic-private", syncproxy.FailureChallenge, 1},
		{503, "text/html", "synthetic-private", syncproxy.FailureServer, 4},
		{404, "text/html", "synthetic-private", syncproxy.FailureStatus, 1},
		{200, "text/html", "<html>normal page</html>", syncproxy.FailureContentType, 1},
		{200, "application/json", "{", syncproxy.FailureInvalidJSON, 1},
		{200, "application/json", "", syncproxy.FailureEmptyResponse, 1},
		{200, "text/html", "<html>cf-chl challenge-platform</html>", syncproxy.FailureChallenge, 1},
	} {
		c := testClient(t, func(*http.Request) (*http.Response, error) { return response(v.status, v.media, v.body), nil })
		_, err := c.FetchCinema(t.Context(), "build", "katorza")
		var re *RequestError
		if !errors.As(err, &re) || re.Kind != v.kind || c.RequestCount() != v.attempts || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatalf("status=%d kind=%d attempts=%d err=%v", v.status, v.kind, c.RequestCount(), err)
		}
	}
}
func TestClientRoutesProxyRotationAndThrottle(t *testing.T) {
	seen := map[int]bool{}
	var starts []time.Time
	clients := make([]*http.Client, 4)
	for i := range clients {
		clients[i] = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if seen[i] {
				t.Error("proxy reused during retry")
			}
			seen[i] = true
			starts = append(starts, time.Now())
			if r.URL.String() != "https://www.cineville.fr/_next/data/build-1/programmes/katorza.json" || r.Method != "GET" {
				t.Error("wrong acquisition route")
			}
			if len(starts) < 4 {
				return nil, errors.New("synthetic-proxy-password")
			}
			return response(200, "application/json; charset=utf-8", `{}`), nil
		})}
	}
	c, err := newClient(clients, 5*time.Millisecond, func(context.Context, time.Duration) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.FetchCinema(t.Context(), "build-1", "katorza"); err != nil || c.RequestCount() != 4 {
		t.Fatalf("count=%d err=%v", c.RequestCount(), err)
	}
	for i := 1; i < len(starts); i++ {
		if starts[i].Sub(starts[i-1]) < 5*time.Millisecond {
			t.Fatal("retry bypassed shared throttle")
		}
	}
	c = testClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != BootstrapURL {
			t.Error("wrong bootstrap")
		}
		return response(200, "text/html", `<html></html>`), nil
	})
	if _, err := c.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
}
func TestClientURLPolicyAndConfiguration(t *testing.T) {
	if _, err := NewClient(ClientConfig{Timeout: 20 * time.Second}); err == nil {
		t.Fatal("direct network fallback")
	}
	for _, raw := range []string{"http://www.cineville.fr/programmes/www", "https://www.cineville.fr:443/programmes/www", "https://user@www.cineville.fr/programmes/www", "https://cineville.fr/programmes/www", BootstrapURL + "?", BootstrapURL + "#x", BootstrapURL + "/../www", "https://www.cineville.fr/_next/data/../programmes/a.json", "https://www.cineville.fr/_next/data/b/programmes/a%2fb.json", "https://www.cineville.fr/_next/data/b/programmes/a.json?x"} {
		u, err := url.Parse(raw)
		if err == nil && allowedURL(u) {
			t.Fatalf("unsafe URL %q", raw)
		}
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("unsafe segment reached transport")
		return nil, nil
	})
	for _, segment := range []string{"", "../x", "a/b", "x%2fy", "x?z", "https://evil.test"} {
		if _, err := c.FetchCinema(t.Context(), segment, "katorza"); err == nil {
			t.Fatal("build accepted")
		}
		if _, err := c.FetchCinema(t.Context(), "build", segment); err == nil {
			t.Fatal("route accepted")
		}
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("synthetic-read-secret") }
func (failingReader) Close() error             { return nil }
func TestClientReadTransportAndBodyBounds(t *testing.T) {
	for _, transport := range []roundTripFunc{
		func(*http.Request) (*http.Response, error) { return nil, errors.New("synthetic-proxy-secret") },
		func(*http.Request) (*http.Response, error) {
			r := response(200, "application/json", "")
			r.Body = failingReader{}
			return r, nil
		},
	} {
		c := testClient(t, transport)
		_, err := c.FetchCinema(t.Context(), "build", "laval")
		if err == nil || c.RequestCount() != 4 || strings.Contains(err.Error(), "secret") {
			t.Fatalf("count=%d err=%v", c.RequestCount(), err)
		}
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		return response(200, "application/json", strings.Repeat("x", MaxBodySize+1)), nil
	})
	_, err := c.FetchCinema(t.Context(), "build", "laval")
	var re *RequestError
	if !errors.As(err, &re) || re.Kind != syncproxy.FailureResponseLarge || c.RequestCount() != 1 {
		t.Fatal("body bound")
	}
}
func TestClientRejectsRedirectAndCanceledPacer(t *testing.T) {
	for _, target := range []string{"https://evil.test/", "http://www.cineville.fr/programmes/www", BootstrapURL} {
		c := testClient(t, func(*http.Request) (*http.Response, error) {
			r := response(302, "text/html", "")
			r.Header.Set("Location", target)
			return r, nil
		})
		if _, err := c.FetchCinema(t.Context(), "build", "laval"); err == nil {
			t.Fatal("redirect accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p := pacer{interval: time.Hour, last: time.Now()}
	if err := p.wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("pacer cancellation")
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("canceled request reached network")
		return nil, nil
	})
	if _, err := c.Fetch(ctx); !errors.Is(err, context.Canceled) || c.RequestCount() != 0 {
		t.Fatal("request cancellation")
	}
}
