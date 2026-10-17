package cineode

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

func TestProxyManifestURLAndHTMLContract(t *testing.T) {
	if _, err := NewClient(ClientConfig{Timeout: 10 * time.Second}); err == nil {
		t.Fatal("unproxied client accepted")
	}
	n := 0
	c, err := newClient([]*http.Client{{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		n++
		if r.Method != "GET" || !allowedURL(r.URL) || (r.Body != nil && r.Body != http.NoBody) {
			t.Error("request contract")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html; charset=iso-8859-1"}}, Body: io.NopCloser(strings.NewReader("source"))}, nil
	})}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range cinemaIDs {
		for _, route := range []string{"seances", "infos-cine", "contact"} {
			if _, err := c.Page(t.Context(), id, route); err != nil {
				t.Fatal(err)
			}
		}
	}
	if n != 111 || c.RequestCount() != 111 {
		t.Fatal("manifest count")
	}
	for _, route := range []string{"seances?x=1", "seances/#", "../seances", "seances%2f", "unknown"} {
		if _, err := c.Page(t.Context(), "4640", route); err == nil {
			t.Fatal("invalid route requested")
		}
	}
	for _, raw := range []string{"http://www.cineode.fr/arletty/seances/", "https://www.cineode.fr:443/arletty/seances/", "https://user@www.cineode.fr/arletty/seances/", "https://www.cineode.fr/arletty/seances/?x=1", "https://www.cineode.fr/arletty/seances/#x", "https://www.cineode.fr/%61rletty/seances/", "https://www.cineode.fr/chantonnay-lumiere/seances/", "https://evil.example/arletty/seances/"} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if allowedURL(u) {
			t.Fatal("unsafe URL accepted")
		}
	}
}

func TestBoundedRetriesChallengesRedirectsAndSanitizedErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		transport bool
		want      int
		kind      syncproxy.FailureKind
	}{
		{"forbidden", 403, "PRIVATE_BODY", false, 1, syncproxy.FailureChallenge},
		{"rate limit", 429, "PRIVATE_BODY", false, 1, syncproxy.FailureChallenge},
		{"server", 500, "PRIVATE_BODY", false, 4, syncproxy.FailureServer},
		{"transport", 200, "", true, 4, syncproxy.FailureTransport},
		{"redirect", 302, "", false, 1, syncproxy.FailureRedirect},
		{"challenge", 200, "<html><title>Just a moment...</title><script src='/cdn-cgi/challenge-platform/a'></script></html>", false, 1, syncproxy.FailureChallenge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			used := map[int]bool{}
			clients := make([]*http.Client, 4)
			for i := range clients {
				clients[i] = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++
					if used[i] {
						t.Error("proxy reused")
					}
					used[i] = true
					if tc.transport {
						return nil, errors.New("PRIVATE_PROXY")
					}
					return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {"text/html"}, "Location": {"https://www.cineode.fr/arletty/seances/"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
				})}
			}
			c, err := newClient(clients, func(context.Context, time.Duration) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Page(t.Context(), "4640", "seances")
			var requestErr *RequestError
			if !errors.As(err, &requestErr) || calls != tc.want || requestErr.Kind != tc.kind || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("retry/error policy", calls, err)
			}
		})
	}
}

func TestMediaBodyBoundsOptional404AndCancellation(t *testing.T) {
	for _, tc := range []struct {
		media, body string
		status      int
		route       string
		ok          bool
	}{
		{"application/json", "{}", 200, "seances", false},
		{"", "<html/>", 200, "seances", false},
		{"text/html", strings.Repeat("x", MaxResponseBytes+1), 200, "seances", false},
		{"text/html", "", 404, "seances", false},
		{"text/html", "", 404, "infos-cine", true},
		{"text/html", "", 404, "contact", true},
	} {
		c, err := newClient([]*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {tc.media}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Page(t.Context(), "4640", tc.route)
		if (err == nil) != tc.ok {
			t.Fatal("media/body/status contract", tc.media, tc.status, err)
		}
	}
	c, err := newClient([]*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("canceled request sent")
		return nil, context.Canceled
	})}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Page(ctx, "4640", "seances"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}
