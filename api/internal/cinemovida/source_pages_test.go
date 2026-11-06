package cinemovida

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type pageIndexTransport func(*http.Request) (*http.Response, error)

func (f pageIndexTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMoviePageIndexUsesBoundedSourceRoute(t *testing.T) {
	for _, status := range []int{200, 403, 302} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			clients := []*http.Client{{Transport: pageIndexTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls > 1 || r.URL.String() != APIBaseURL+"/sitemap-0.xml" {
					t.Fatal("unexpected source request")
				}
				return &http.Response{StatusCode: status, Request: r, Header: http.Header{"Content-Type": {"application/xml"}, "Location": {"https://evil.example/redirect"}}, Body: io.NopCloser(strings.NewReader("<urlset/>"))}, nil
			})}}
			client, err := newClient(clients, nil)
			if err != nil {
				t.Fatal(err)
			}
			body, err := client.MoviePageIndex(t.Context())
			if status == 200 {
				if err != nil || string(body) != "<urlset/>" {
					t.Fatal("XML route rejected", err)
				}
			} else if err == nil {
				t.Fatal("unsafe status accepted")
			}
			if calls != 1 || client.RequestCount() != 1 {
				t.Fatal("wrong request accounting")
			}
			if _, err := client.Get(t.Context(), OperationMovies, APIBaseURL+"/sitemap-0.xml"); err == nil {
				t.Fatal("sitemap admitted as movie JSON")
			}
		})
	}
}
