package mk2

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/syncproxy"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := f(r)
	if response != nil {
		response.Request = r
	}
	return response, err
}
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func noSleep(context.Context, time.Duration) error { return nil }

func TestClientRequiresProxiesAndStrictURLs(t *testing.T) {
	if _, err := NewClient(ClientConfig{Timeout: 10 * time.Second}); err == nil {
		t.Fatal("direct fallback")
	}
	for _, path := range []string{"/cinemas", "/films", "/cinema-complex/bibliotheque"} {
		u, _ := url.Parse(APIURL + path)
		if !allowedURL(u) {
			t.Fatal(path)
		}
	}
	for _, raw := range []string{APIURL + "/", APIURL + "/films?", APIURL + "/films?x=1", APIURL + "/films#x", APIURL + "/films/", APIURL + "/cinema-complex/../films", APIURL + "/cinema-complex/%62ibliotheque", APIURL + "/cinema-complex/a/b", APIURL + "/cinema-complex/" + strings.Repeat("x", 129), "http://prod-paris-cf.api.mk2.com/films", "https://user@prod-paris-cf.api.mk2.com/films", "https://prod-paris-cf.api.mk2.com:443/films", "https://prod-paris-cf.api.mk2.com.evil.test/films"} {
		u, _ := url.Parse(raw)
		if allowedURL(u) {
			t.Fatal("unsafe URL", raw)
		}
	}
	calls := 0
	c, err := newClient([]*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(200, `{"data":[]}`), nil })}}, 0, noSleep)
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"../films", "a?x=1", "", "https://evil.test"} {
		if _, err := c.FetchComplex(t.Context(), slug); err == nil {
			t.Fatal("unsafe slug")
		}
	}
	if calls != 0 {
		t.Fatal("unsafe acquisition")
	}
}
func TestClientRetriesDistinctProxiesAndCountsAttempts(t *testing.T) {
	for _, transportFailure := range []bool{false, true} {
		attempts := []int{}
		clients := make([]*http.Client, 5)
		for i := range clients {
			clients[i] = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				attempts = append(attempts, i)
				if transportFailure {
					return nil, errors.New("https://user:password@proxy.test secret")
				}
				return response(503, `{"error":"secret"}`), nil
			})}
		}
		c, err := newClient(clients, 0, noSleep)
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.FetchFilms(t.Context())
		if err == nil || len(attempts) != 4 || c.RequestCount() != 4 || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "password") {
			t.Fatalf("attempts=%v err=%v", attempts, err)
		}
		seen := map[int]bool{}
		for _, i := range attempts {
			if seen[i] {
				t.Fatal("proxy reused")
			}
			seen[i] = true
		}
	}
}
func TestClientResponsePolicyAndRedirectSafety(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		kind   syncproxy.FailureKind
	}{
		{"403", 403, `{"error":"secret"}`, syncproxy.FailureChallenge},
		{"429", 429, `{}`, syncproxy.FailureChallenge},
		{"challenge", 200, `<html><title>Just a moment...</title><div id="challenge-form">cloudflare challenge</div></html>`, syncproxy.FailureChallenge},
		{"bad JSON", 200, `{"data":`, syncproxy.FailureInvalidJSON},
		{"large", 200, strings.Repeat(" ", MaxBodySize+1), syncproxy.FailureResponseLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c, err := newClient([]*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(tc.status, tc.body), nil })}}, 0, noSleep)
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.FetchCinemas(t.Context())
			var re *RequestError
			if !errors.As(err, &re) || re.Kind != tc.kind || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
	calls := 0
	c, err := newClient([]*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		r := response(302, "")
		r.Header.Set("Location", "https://evil.test/secret")
		return r, nil
	})}}, 0, noSleep)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.FetchFilms(t.Context())
	var re *RequestError
	if !errors.As(err, &re) || re.Kind != syncproxy.FailureRedirect || calls != 1 || strings.Contains(err.Error(), "evil") {
		t.Fatalf("redirect err=%v calls=%d", err, calls)
	}
}
func TestClientSharedPacingAndCancellation(t *testing.T) {
	var mu sync.Mutex
	var starts []time.Time
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		return response(200, `{"data":[]}`), nil
	})
	c, err := newClient([]*http.Client{{Transport: transport}, {Transport: transport}}, 25*time.Millisecond, noSleep)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if _, err := c.FetchFilms(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if len(starts) != 2 || starts[1].Sub(starts[0]) < 20*time.Millisecond {
		t.Fatalf("pacing=%v", starts)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = c.FetchFilms(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
}

type boundedFetcher struct {
	mu          sync.Mutex
	active, max int
	entered     chan struct{}
	release     chan struct{}
}

func (*boundedFetcher) FetchCinemas(context.Context) ([]byte, error) { panic("not used") }
func (*boundedFetcher) FetchFilms(context.Context) ([]byte, error)   { panic("not used") }
func (f *boundedFetcher) FetchComplex(ctx context.Context, slug string) ([]byte, error) {
	f.mu.Lock()
	f.active++
	f.max = max(f.max, f.active)
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.active--; f.mu.Unlock() }()
	f.entered <- struct{}{}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.release:
	}
	return []byte(`{"slug":"` + slug + `","zipcode":"75013","cinemas":[],"sessionsByType":[]}`), nil
}
func TestComplexWorkersBoundedAndCancellation(t *testing.T) {
	f := &boundedFetcher{entered: make(chan struct{}, 10), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := fetchComplexes(t.Context(), f, []string{"a", "b", "c", "d"}); done <- err }()
	for range 2 {
		select {
		case <-f.entered:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	f.mu.Lock()
	active := f.active
	f.mu.Unlock()
	if active != 2 {
		t.Fatal("worker bound", active)
	}
	close(f.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if f.max != 2 {
		t.Fatal("worker bound", f.max)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fetchComplexes(ctx, f, []string{"a"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
