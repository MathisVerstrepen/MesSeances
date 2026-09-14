package noecinemas

import (
	"context"
	"errors"
	"io"
	"messeances/api/internal/syncproxy"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(r *http.Request, status int, body, media string) *http.Response {
	return &http.Response{Request: r, StatusCode: status, Header: http.Header{"Content-Type": {media}}, Body: io.NopCloser(strings.NewReader(body))}
}
func TestClientBoundsRetriesCancellationAndRedaction(t *testing.T) {
	if _, err := NewClient(ClientConfig{Timeout: 20 * time.Second}); err == nil {
		t.Fatal("direct transport")
	}
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		want     syncproxy.FailureKind
		attempts int
	}{
		{"ok", 200, `{}`, 0, 1}, {"server", 503, "secret-body", syncproxy.FailureServer, 4}, {"403", 403, "secret", syncproxy.FailureChallenge, 1}, {"429", 429, "secret", syncproxy.FailureChallenge, 1}, {"challenge", 200, "<html>cf-chl-platform</html>", syncproxy.FailureChallenge, 1}, {"transport", 0, "", syncproxy.FailureTransport, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clients := make([]*http.Client, 4)
			used := map[int]bool{}
			for i := range clients {
				clients[i] = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					if used[i] {
						t.Fatal("proxy repeated")
					}
					used[i] = true
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
				t.Fatal("attempt bound")
			}
		})
	}
	calls := 0
	c, err := newClient([]*http.Client{{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return response(r, 200, strings.Repeat(" ", MaxResponseBytes+1), "application/json"), nil
	})}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Get(t.Context(), OperationCinemas, CinemasURL); err == nil {
		t.Fatal("oversized body")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = c.Get(ctx, OperationCinemas, CinemasURL); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("cancel ignored")
	}
}
func TestRoomRedirectGrammarAndNoCookies(t *testing.T) {
	booking := "https://achat.cinema-laigle.com/reserver/r/123"
	for _, tc := range []struct {
		location string
		valid    bool
	}{
		{"/reserver/F123/D2026091420/VO/12345/", true}, {"/reserver/F123/D2026091420/VF/12345/", true},
		{"https://evil.test/reserver/F123/D2026091420/VF/12345/", false}, {"https://achat.cinepal.fr/reserver/r/123", false}, {"https://achat.cinema-laigle.com:443/reserver/r/123", false}, {"http://achat.cinema-laigle.com/reserver/r/123", false}, {"/reserver/F123/D20260914/VF/12345/", false}, {"/reserver/F123/D2026091420/VOSTFR/12345/", false}, {"/checkout?session=123", false}, {"/reserver/r/123?token=secret", false}, {"/reserver/r/%31", false}, {"/reserver/r/1#x", false}, {"/reserver/r/%252e", false},
	} {
		t.Run(tc.location, func(t *testing.T) {
			calls := 0
			jar, _ := cookiejar.New(nil)
			u, _ := url.Parse(booking)
			jar.SetCookies(u, []*http.Cookie{{Name: "secret", Value: "secret"}})
			c, err := newClient([]*http.Client{{Jar: jar, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" {
					t.Fatal("cookie retained")
				}
				if calls == 1 {
					res := response(r, 307, "", "text/html")
					res.Header.Set("Location", tc.location)
					res.Header.Set("Set-Cookie", "secret=secret")
					return res, nil
				}
				return response(r, 200, `{"auditorium_showtime":"salle-1"}`, "text/html"), nil
			})}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			b, err := c.Get(t.Context(), OperationRoom, booking)
			if tc.valid {
				if err != nil || calls != 2 || parseRoom(b) != "Salle 1" {
					t.Fatal("valid redirect")
				}
			} else if err == nil || calls != 1 {
				t.Fatal("unsafe hop followed")
			}
		})
	}
	calls := 0
	c, err := newClient([]*http.Client{{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		res := response(r, 302, "", "text/html")
		res.Header.Set("Location", booking)
		return res, nil
	})}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Get(t.Context(), OperationRoom, booking); err == nil || calls > 4 {
		t.Fatal("unbounded redirects")
	}
}
func TestOperationSpecificURLGrammar(t *testing.T) {
	for op, raw := range map[Operation]string{OperationCinemas: CinemasURL, OperationProgram: programURL("P8088"), OperationMovies: moviesURL([]string{"1", "cEvent_1"}), OperationSchedule: scheduleURL("P8088", "2026-09-14", "2028-07-15")} {
		u, _ := url.Parse(raw)
		if !operationMatchesURL(op, u) {
			t.Fatalf("generated URL op=%s", op)
		}
		for _, bad := range []string{raw + "&extra=1", strings.Replace(raw, "www.noecinemas.com", "www.noecinemas.com.evil.test", 1), strings.Replace(raw, "https:", "http:", 1), raw + "#x", strings.Replace(raw, "www.noecinemas.com", "www.noecinemas.com:443", 1)} {
			u, _ := url.Parse(bad)
			if operationMatchesURL(op, u) {
				t.Fatal("unsafe operation")
			}
		}
	}
	for op, raw := range map[Operation]string{OperationProgram: moviesURL([]string{"1"}), OperationMovies: moviesURL([]string{"1", "1"}), OperationRoom: "https://achat.cinema-laigle.com/reserver/F1/D2026091420/VF/123/", OperationSchedule: strings.Replace(scheduleURL("P8088", "2026-09-14", "2026-09-14"), "03%3A00%3A00", "02%3A00%3A00", 1)} {
		u, _ := url.Parse(raw)
		if operationMatchesURL(op, u) {
			t.Fatal("wrong operation grammar")
		}
	}
}
