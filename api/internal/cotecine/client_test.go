package cotecine

import (
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

func TestClientFetchesOnlyConfiguredProgrammePages(t *testing.T) {
	if _, err := NewClient(ClientConfig{Timeout: 10 * time.Second}); err == nil {
		t.Fatal("unproxied client accepted")
	}
	var requested []string
	c, err := newClient([]*http.Client{{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.String())
		if r.Method != http.MethodGet || !allowedURL(r.URL) {
			t.Error("request contract")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html; charset=iso-8859-1"}}, Body: io.NopCloser(strings.NewReader("source"))}, nil
	})}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, website := range []string{"https://www.cinema-arvor.fr/", "https://www.cinema-confluences.com/sable-sur-sarthe/"} {
		if _, err := c.Programme(t.Context(), website); err != nil {
			t.Fatal(err)
		}
	}
	if len(requested) != 2 || requested[1] != "https://www.cinema-confluences.com/sable-sur-sarthe/seances/" || c.RequestCount() != 2 {
		t.Fatalf("requested=%v", requested)
	}
	for _, website := range []string{"https://www.cinema-arvor.fr", "http://www.cinema-arvor.fr/", "https://www.cinema-arvor.fr:443/", "https://user@www.cinema-arvor.fr/", "https://www.cinema-arvor.fr/?x=1", "https://www.cinema-arvor.fr/Sable/", "https://www.cinema-arvor.fr/a/../", "https://127.0.0.1/"} {
		_, err := c.Programme(t.Context(), website)
		var requestErr *syncproxy.RequestError
		if !errors.As(err, &requestErr) || requestErr.Kind != syncproxy.FailureInvalidURL {
			t.Fatalf("website %q err=%v", website, err)
		}
	}
	if len(requested) != 2 {
		t.Fatal("invalid website reached the network")
	}
	for _, raw := range []string{"https://www.cinema-arvor.fr/seances/?x=1", "https://www.cinema-arvor.fr/seances/#x", "https://www.cinema-arvor.fr/film/1/", "https://www.cinema-arvor.fr/seances"} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if allowedURL(u) {
			t.Fatalf("unsafe URL accepted: %s", raw)
		}
	}
}
