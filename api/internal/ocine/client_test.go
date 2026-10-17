package ocine

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

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func TestClientSafeRoutesRetriesAndRedaction(t *testing.T) {
	for _, raw := range []string{"https://www.ocine.fr/maubeuge/seances/", "https://www.ocine.fr/st-omer/plan-d-acces/", "https://www.ocine.fr/dunkerque/reserver/F123/D1792252800/VO/4/"} {
		u, _ := url.Parse(raw)
		if !allowedURL(u) {
			t.Fatal("safe route rejected")
		}
	}
	for _, raw := range []string{"http://www.ocine.fr/maubeuge/seances/", "https://www.ocine.fr:443/maubeuge/seances/", "https://www.ocine.fr/maubeuge/seances/?x=1", "https://user@www.ocine.fr/maubeuge/seances/", "https://www.ocine.fr/unknown/seances/", "https://evil.example/maubeuge/seances/", "https://www.ocine.fr/maubeuge/reserver/F0123/D1792252800/VF/4/"} {
		u, _ := url.Parse(raw)
		if allowedURL(u) {
			t.Fatal("unsafe route accepted", raw)
		}
	}
	if _, err := NewClient(ClientConfig{Timeout: 10 * time.Second}); err == nil {
		t.Fatal("direct source access allowed")
	}
	if _, err := (&Client{}).Checkout(t.Context(), "https://www.ocine.fr/maubeuge/reserver/F123/D1792252800/VO/4/#"); err == nil {
		t.Fatal("bare fragment accepted")
	}
	for _, kind := range []string{"transport", "server", "challenge", "challenge-body", "wrong-type", "empty", "too-large", "redirect", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			clients := make([]*http.Client, 4)
			for i := range clients {
				index := i
				clients[i] = &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					if calls != index+1 {
						t.Error("proxy reused")
					}
					if r.Method != "GET" || r.Header.Get("Cookie") != "" {
						t.Error("unsafe request")
					}
					switch kind {
					case "transport":
						return nil, errors.New("private-proxy-secret")
					case "server":
						return testResponse(503, "private response"), nil
					case "challenge":
						return testResponse(403, "private response"), nil
					case "challenge-body":
						return testResponse(200, "<html><title>Captcha</title></html>"), nil
					case "wrong-type":
						r := testResponse(200, "private response")
						r.Header.Set("Content-Type", "application/json")
						return r, nil
					case "empty":
						return testResponse(200, ""), nil
					case "too-large":
						return testResponse(200, strings.Repeat("x", MaxResponseBytes+1)), nil
					case "redirect":
						r := testResponse(302, "")
						r.Header.Set("Location", "https://evil.example/")
						return r, nil
					default:
						return nil, r.Context().Err()
					}
				})}
			}
			c, err := newClient(clients, func(context.Context, time.Duration) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			if kind == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			_, err = c.Page(ctx, "309", "seances")
			if err == nil || strings.Contains(err.Error(), "private") {
				t.Fatal("unsafe error", err)
			}
			want := 1
			if kind == "transport" || kind == "server" {
				want = 4
			}
			if kind == "cancel" {
				want = 0
			}
			if calls != want {
				t.Fatal("request count", calls, want)
			}
			var requestErr *RequestError
			if !errors.As(err, &requestErr) {
				t.Fatal("lost request category")
			}
			if kind == "cancel" && (requestErr.Kind != syncproxy.FailureCanceled || !errors.Is(err, context.Canceled)) {
				t.Fatal("lost cancellation")
			}
		})
	}
}
