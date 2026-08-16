package ugc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := f(r)
	if response != nil && response.Request == nil {
		response.Request = r
	}
	return response, err
}
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}

func TestClientTerminalResponseDoesNotRotate(t *testing.T) {
	first, second := 0, 0
	c := &Client{config: ClientConfig{}, clients: []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { first++; return response(403, "blocked"), nil })}, {Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { second++; return response(200, "ok"), nil })}}, unavailable: make([]bool, 2)}
	_, err := c.Get(context.Background(), "showings", "https://www.ugc.fr/test")
	var terminal *TerminalError
	if !errors.As(err, &terminal) {
		t.Fatalf("error=%v", err)
	}
	if first != 1 || second != 0 {
		t.Fatalf("requests=%d,%d", first, second)
	}
}
func TestClientChallengeRedactsSyntheticSecret(t *testing.T) {
	secret := "synthetic-password"
	c := &Client{config: ClientConfig{}, clients: []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(200, `<title>DataDome CAPTCHA</title>`+secret), nil
	})}}, unavailable: make([]bool, 1)}
	_, err := c.Get(context.Background(), "cinema", "https://www.ugc.fr/test")
	if err == nil {
		t.Fatal("challenge accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("response content leaked")
	}
}
func TestClientRetriesOneServerFailure(t *testing.T) {
	calls := 0
	c := &Client{config: ClientConfig{}, clients: []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(500, "temporary"), nil })}, {Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(200, "ok"), nil })}}, unavailable: make([]bool, 2)}
	result, err := c.Get(context.Background(), "sitemap", "https://www.ugc.fr/test")
	if err != nil || string(result.Body) != "ok" || result.FinalURL != "https://www.ugc.fr/test" || calls != 2 {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
	}
}

func TestClientExposesSanitizedFinalRedirectURL(t *testing.T) {
	calls := 0
	c := &Client{config: ClientConfig{}, clients: []*http.Client{{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.Path == "/cinema.html" {
			result := response(http.StatusFound, "redirect")
			result.Header.Set("Location", "https://www.ugc.fr/cinemas.html?id=1")
			return result, nil
		}
		return response(http.StatusOK, "directory"), nil
	})}}, unavailable: make([]bool, 1)}
	result, err := c.Get(context.Background(), "cinema 2", "https://www.ugc.fr/cinema.html?id=2")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || string(result.Body) != "directory" || result.FinalURL != "https://www.ugc.fr/cinemas.html?id=1" {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
}

func TestClientRejectsNonExactInitialAuthority(t *testing.T) {
	urls := []string{
		"https://synthetic-user:synthetic-password@www.ugc.fr/test",
		"https://www.ugc.fr:443/test",
		"https://www.ugc.fr:8443/test",
		"https://ugc.fr/test",
	}
	for _, raw := range urls {
		t.Run(raw, func(t *testing.T) {
			calls := 0
			client := &Client{config: ClientConfig{}, clients: []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return response(200, "ok"), nil
			})}}, unavailable: make([]bool, 1)}
			if _, err := client.Get(context.Background(), "test", raw); err == nil {
				t.Fatal("non-exact authority accepted")
			}
			if calls != 0 {
				t.Fatalf("transport calls=%d", calls)
			}
		})
	}
}

func TestRedirectPolicyRejectsCrossAuthority(t *testing.T) {
	invalid := []string{
		"https://synthetic-user@www.ugc.fr/next",
		"https://www.ugc.fr:443/next",
		"https://www.ugc.fr:8443/next",
		"https://assets.ugc.fr/next",
		"https://example.test/next",
	}
	for _, raw := range invalid {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkUGCRedirect(&http.Request{URL: parsed}, []*http.Request{{URL: parsed}}); !errors.Is(err, errRedirectHost) {
			t.Fatalf("redirect %q error=%v", raw, err)
		}
	}
	allowed, _ := url.Parse("https://www.ugc.fr/next")
	if err := checkUGCRedirect(&http.Request{URL: allowed}, []*http.Request{{URL: allowed}}); err != nil {
		t.Fatalf("valid redirect rejected: %v", err)
	}
}
