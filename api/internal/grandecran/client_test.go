package grandecran

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"messeances/api/internal/syncproxy"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(r *http.Request, status int, body, media string) *http.Response {
	return &http.Response{Request: r, StatusCode: status, Header: http.Header{"Content-Type": {media}}, Body: io.NopCloser(strings.NewReader(body))}
}
func TestClientPoliciesRetriesAndRedaction(t *testing.T) {
	if _, err := NewClient(ClientConfig{Timeout: 20 * time.Second}); err == nil {
		t.Fatal("direct client")
	}
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		want     syncproxy.FailureKind
		attempts int
	}{
		{"ok", 200, `{}`, 0, 1}, {"server", 503, `secret-provider-body`, syncproxy.FailureServer, 4}, {"block", 403, `secret-provider-body`, syncproxy.FailureChallenge, 1}, {"rate", 429, `secret-provider-body`, syncproxy.FailureChallenge, 1}, {"challenge", 200, `<html>cf-chl-platform</html>`, syncproxy.FailureChallenge, 1}, {"transport", 0, "", syncproxy.FailureTransport, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clients := make([]*http.Client, 4)
			for i := range clients {
				clients[i] = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					if tc.status == 0 {
						return nil, errors.New("secret-proxy-url")
					}
					return response(r, tc.status, tc.body, "application/json"), nil
				})}
			}
			c, err := newClient(clients, func(context.Context, time.Duration) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Get(t.Context(), OperationCinemas, CinemasURL)
			if tc.want == 0 {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var re *RequestError
				if !errors.As(err, &re) || re.Kind != tc.want || strings.Contains(err.Error(), "secret") {
					t.Fatalf("err=%v", err)
				}
			}
			if c.RequestCount() != tc.attempts {
				t.Fatalf("attempts=%d", c.RequestCount())
			}
		})
	}
}
func TestRoomRedirectPolicyAndNoJSONOptIn(t *testing.T) {
	booking := "https://achat.grandecran.fr/test/r/123"
	for _, tc := range []struct {
		location string
		valid    bool
	}{{"/checkout?session=123", true}, {"https://evil.test/checkout", false}, {"https://www.grandecran.fr/checkout", false}, {"https://achat.grandecran.fr:443/x", false}, {"/a/%2e%2e/x", false}, {"/a/%2500", false}, {"/a/%00", false}, {"/checkout?q=%0a", false}, {"/checkout#x", false}, {"/checkout?q=%252e", false}} {
		t.Run(tc.location, func(t *testing.T) {
			var calls atomic.Int64
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if r.URL.String() == booking {
					res := response(r, 302, "", "text/html")
					res.Header.Set("Location", tc.location)
					return res, nil
				}
				return response(r, 200, `{"auditorium_showtime":"salle-1"}`, "text/html"), nil
			})}
			c, err := newClient([]*http.Client{client}, nil)
			if err != nil {
				t.Fatal(err)
			}
			b, err := c.Get(t.Context(), OperationRoom, booking)
			if tc.valid {
				if err != nil || parseRoom(b) != "Salle 1" || calls.Load() != 2 {
					t.Fatalf("redirect err=%v calls=%d", err, calls.Load())
				}
			} else if err == nil || calls.Load() != 1 {
				t.Fatal("unsafe redirect followed")
			}
		})
	}
	var calls atomic.Int64
	c, err := newClient([]*http.Client{{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		res := response(r, 302, "", "text/html")
		res.Header.Set("Location", booking)
		return res, nil
	})}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(t.Context(), OperationRoom, booking); err == nil || calls.Load() > 4 {
		t.Fatal("redirect loop unbounded")
	}
	for _, raw := range []string{CinemasURL + "?x=1", strings.Replace(CinemasURL, "www.grandecran.fr", "www.grandecran.fr.evil.test", 1), "https://cms-assets.webediamovies.pro/prod/cgr/build/public/page-data/sq/d/2506275789.json"} {
		u, _ := url.Parse(raw)
		if operationMatchesURL(OperationCinemas, u) {
			t.Fatal("unsafe discovery URL")
		}
	}
	for op, raw := range map[Operation]string{OperationProgram: programURL("G028P"), OperationMovies: moviesURL([]string{"1", "cEvent_1"}), OperationSchedule: scheduleURL("P9488", "2026-09-14", "2029-07-01")} {
		u, _ := url.Parse(raw)
		if !operationMatchesURL(op, u) {
			t.Fatalf("generated URL rejected op=%s", op)
		}
	}
}
