package cinewest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	v, e := f(r)
	if v != nil {
		v.Request = r
	}
	return v, e
}
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func noSleep(context.Context, time.Duration) error { return nil }

func TestClientProxyOnlyAndRequestContracts(t *testing.T) {
	if _, err := NewClient(ClientConfig{Timeout: 10 * time.Second}); err == nil {
		t.Fatal("direct fallback")
	}
	calls := 0
	c, err := newClient([]*http.Client{{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("User-Agent") == "" {
			t.Fatal("browser user agent missing")
		}
		switch r.URL.Host {
		case "ws.ticketingcine.com":
			var body struct {
				Method string
				Params map[string]string
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Method != "get_prog" || r.Method != "POST" || r.Referer() != schedule.CinewestWebsite("ticketingcine-"+body.Params["site_id"]) {
				t.Fatal("wrong RPC or referer")
			}
		case "cinewest.cineoffice.fr":
			if r.URL.Query().Get("api_token") != "synthetic-cinema" || r.Referer() != "" {
				t.Fatal("wrong token scope")
			}
		case "www.capitolestudios.com":
			if r.URL.Path == apiPath+"schedule" && r.URL.Query().Get("theaters") != `{"id":"W8400","timeZone":"Europe/Paris"}` {
				t.Fatal("noncompact theaters")
			}
		}
		return response(200, `[]`), nil
	})}}, noSleep)
	if err != nil {
		t.Fatal(err)
	}
	for _, site := range []string{"EMS1185", "EMS1317", "EMS0042"} {
		if _, err := c.Program(t.Context(), site); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Catalog(t.Context(), "shows", "synthetic-cinema"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Schedule(t.Context(), "2026-09-14", "2027-09-14"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Movies(t.Context(), []string{"1", "1000000001"}); err != nil {
		t.Fatal(err)
	}
	if calls != 6 || c.RequestCount() != 6 {
		t.Fatal("request count")
	}
	for _, raw := range []string{officeURL + "shows?api_token=x&next=evil", officeURL + "shows?api_token=x&api_token=y", officeURL + "shows?api_token=", officeURL + "../shows?api_token=x", "http://cinewest.cineoffice.fr/vad/shows?api_token=x", "https://user@cinewest.cineoffice.fr/vad/shows?api_token=x", "https://cinewest.cineoffice.fr:443/vad/shows?api_token=x", capitoleURL + "/api/other", capitoleURL + apiPath + "movies?ids=1&basic=false&castingLimit=3&next=x"} {
		u, _ := url.Parse(raw)
		if allowedURL(u) {
			t.Fatal("unsafe acquisition")
		}
	}
}
func TestClientSafeFailuresAndDistinctRetries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		want     syncproxy.FailureKind
		attempts int
	}{
		{"forbidden", 403, "synthetic-secret", syncproxy.FailureChallenge, 1},
		{"throttled", 429, "synthetic-secret", syncproxy.FailureChallenge, 1},
		{"challenge", 200, `<html><title>Just a moment...</title><div id="challenge-form">cloudflare challenge</div></html>`, syncproxy.FailureChallenge, 1},
		{"server", 503, "synthetic-secret", syncproxy.FailureServer, 4},
		{"transport", 0, "", syncproxy.FailureTransport, 4},
		{"redirect", 302, "", syncproxy.FailureRedirect, 1},
		{"oversized", 200, strings.Repeat(" ", MaxResponseBytes+1), syncproxy.FailureResponseLarge, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := map[int]bool{}
			clients := make([]*http.Client, 5)
			for i := range clients {
				clients[i] = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					if seen[i] {
						t.Fatal("proxy repeated")
					}
					seen[i] = true
					if tc.status == 0 {
						return nil, errors.New("https://user:synthetic-secret@proxy.invalid")
					}
					r := response(tc.status, tc.body)
					if tc.status == 302 {
						r.Header.Set("Location", "https://evil.invalid/?token=synthetic-secret")
					}
					return r, nil
				})}
			}
			c, err := newClient(clients, noSleep)
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Catalog(t.Context(), "shows", "synthetic-secret")
			var re *RequestError
			if !errors.As(err, &re) || re.Kind != tc.want || len(seen) != tc.attempts || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "http") {
				t.Fatalf("wrong bounded failure: %v", err)
			}
		})
	}
}
