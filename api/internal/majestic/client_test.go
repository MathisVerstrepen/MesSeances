package majestic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
func TestMajesticRequestManifestAndProxyRequired(t *testing.T) {
	if _, err := NewClient(ClientConfig{Timeout: 10 * time.Second}); err == nil {
		t.Fatal("direct transport accepted")
	}
	count := 0
	c, err := newClient([]*http.Client{{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		count++
		var input struct {
			JSONRPC, Method string
			Params          map[string]string
			ID              int
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || r.URL.String() != ProgramURL || r.Method != "POST" || r.Header.Get("Referer") != "https://www.ticketingcine.com/" || r.Header.Get("Content-Type") != "application/json" || input.JSONRPC != "2.0" || input.Method != "get_prog" || input.ID != 1 || input.Params["site_id"] != sites[count-1] {
			t.Error("request contract")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{"Content-Type": {"application/json"}}}, nil
	})}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, site := range sites {
		if _, err := c.Program(t.Context(), site); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Program(t.Context(), "EMS9999"); err == nil || count != 7 || c.RequestCount() != 7 {
		t.Fatal("manifest allowlist/count")
	}
}
func TestMajesticTransportFailuresAreBoundedAndSanitized(t *testing.T) {
	for _, status := range []int{403, 429, 500, 302} {
		calls := 0
		clients := make([]*http.Client, 4)
		for i := range clients {
			clients[i] = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("PRIVATE_RESPONSE")), Header: http.Header{"Location": {"https://evil.example/"}}}, nil
			})}
		}
		c, err := newClient(clients, func(context.Context, time.Duration) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Program(t.Context(), sites[0])
		var requestErr *RequestError
		want := 1
		if status == 500 {
			want = 4
		}
		if !errors.As(err, &requestErr) || calls != want || strings.Contains(err.Error(), "PRIVATE_RESPONSE") {
			t.Fatal("unsafe/unbounded error", status, calls, err)
		}
		if (status == 403 || status == 429) && requestErr.Kind != syncproxy.FailureChallenge {
			t.Fatal("challenge retried")
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
	if _, err := c.Program(ctx, sites[0]); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}
