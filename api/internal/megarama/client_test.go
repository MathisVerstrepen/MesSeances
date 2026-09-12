package megarama

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
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
func response(status int, contentType, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}
func testClient(t *testing.T, f roundTripFunc) *Client {
	t.Helper()
	clients := make([]*http.Client, 4)
	for i := range clients {
		clients[i] = &http.Client{Transport: f}
	}
	c, err := newClient(clients, func(context.Context, time.Duration) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClientConcurrentRefererAndRetryIsolation(t *testing.T) {
	var mu sync.Mutex
	counts := map[string]int{}
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		id, host := "EMS0565", "bordeaux.megarama.fr"
		if strings.Contains(string(body), "EMS1315") {
			id, host = "EMS1315", "boulogne.megarama.fr"
		}
		if r.Method != "POST" || r.URL.String() != ProgramURL || r.Header.Get("Referer") != "https://"+host+"/" || r.Header.Get("Content-Type") != "application/json" || string(body) != `{"jsonrpc":"2.0","method":"get_prog","params":{"site_id":"`+id+`"},"id":1}` {
			t.Error("isolated request tuple mismatch")
		}
		mu.Lock()
		counts[id]++
		count := counts[id]
		mu.Unlock()
		if count == 1 {
			return response(503, "text/html", "temporarily unavailable"), nil
		}
		return response(200, "application/json", `{}`), nil
	})
	var wg sync.WaitGroup
	for _, item := range []struct{ id, website string }{{"EMS0565", "https://bordeaux.megarama.fr/"}, {"EMS1315", "https://boulogne.megarama.fr/"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Program(t.Context(), item.id, item.website); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if c.RequestCount() != 4 || counts["EMS0565"] != 2 || counts["EMS1315"] != 2 {
		t.Fatal("retry count")
	}
}

func TestClientResponseSafetyAndMedia(t *testing.T) {
	for _, test := range []struct {
		status      int
		media, body string
		kind        syncproxy.FailureKind
		count       int
	}{
		{403, "text/html", "secret", syncproxy.FailureChallenge, 1}, {429, "text/html", "secret", syncproxy.FailureChallenge, 1}, {503, "text/html", "secret", syncproxy.FailureServer, 4}, {200, "text/html", "<html>normal</html>", syncproxy.FailureContentType, 1}, {200, "application/json", "{", syncproxy.FailureInvalidJSON, 1}, {302, "text/html", "redirect", syncproxy.FailureStatus, 1},
	} {
		c := testClient(t, func(*http.Request) (*http.Response, error) { return response(test.status, test.media, test.body), nil })
		_, err := c.Program(t.Context(), "EMS0565", "https://bordeaux.megarama.fr/")
		var requestErr *RequestError
		if !errors.As(err, &requestErr) || requestErr.Kind != test.kind || c.RequestCount() != test.count || strings.Contains(err.Error(), "secret") {
			t.Fatalf("safe response classification: %v", err)
		}
	}
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == ConfigURL {
			return response(200, "application/javascript", configFixture), nil
		}
		return response(200, "text/html", `<html></html>`), nil
	})
	if _, err := c.Config(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Poster(t.Context(), "ABCDE"); err != nil {
		t.Fatal(err)
	}
}

func TestClientRejectsUnsafeRequestsAndRedactsTransport(t *testing.T) {
	if _, err := NewClient(ClientConfig{Timeout: 10 * time.Second}); err == nil {
		t.Fatal("direct network fallback")
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("synthetic-proxy-secret") })
	if _, err := c.Program(t.Context(), "EMS0565", "https://evil.test/"); err == nil || c.RequestCount() != 0 {
		t.Fatal("unsafe referer dispatched")
	}
	if _, err := c.Poster(t.Context(), "../bad"); err == nil || c.RequestCount() != 0 {
		t.Fatal("unsafe poster dispatched")
	}
	_, err := c.Config(t.Context())
	if err == nil || strings.Contains(err.Error(), "synthetic-proxy-secret") || c.RequestCount() != 4 {
		t.Fatal("transport leak or retries")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = c.Config(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost")
	}
}
