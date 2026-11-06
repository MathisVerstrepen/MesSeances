package sourcepages

import (
	"context"
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

func TestPublishedRoutesMatchExactSourceIdentities(t *testing.T) {
	for _, tc := range []struct {
		provider schedule.Provider
		prefix   string
	}{
		{schedule.ProviderCGR, "https://www.cgrcinemas.fr/films-a-l-affiche/"},
		{schedule.ProviderGrandEcran, "https://www.grandecran.fr/movies/"},
		{schedule.ProviderNoeCinemas, "https://www.noecinemas.com/films/"},
		{schedule.ProviderCinemovida, "https://www.cine-movida.com/movies/"},
		{schedule.ProviderVeo, "https://veocinemas.fr/movies/"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			want := tc.prefix + "123-original-source-title/"
			body := `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>` + want + `</loc></url><url><loc>` + tc.prefix + `1234-other-film/</loc></url><url><loc>https://evil.example/movies/12-trap/</loc></url></urlset>`
			pages, err := Parse(tc.provider, map[string]bool{"123": true, "12": true}, []byte(body))
			if err != nil || len(pages) != 1 || pages["123"] != want {
				t.Fatalf("pages=%v err=%v", pages, err)
			}
		})
	}
	pages, err := Parse(schedule.ProviderGrandEcran, map[string]bool{"cEvent_1-a": true}, []byte(`<urlset><url><loc>https://www.grandecran.fr/movies/cEvent_1-a-source-event/</loc></url></urlset>`))
	if err != nil || pages["cEvent_1-a"] == "" {
		t.Fatalf("opaque event lost: %v %v", pages, err)
	}
}

func TestAmbiguousAndMalformedSitemaps(t *testing.T) {
	prefix := "https://www.noecinemas.com/films/123-"
	pages, err := Parse(schedule.ProviderNoeCinemas, map[string]bool{"123": true}, []byte(`<urlset><url><loc>`+prefix+`a/</loc></url><url><loc>`+prefix+`b/</loc></url></urlset>`))
	if err != nil || len(pages) != 0 {
		t.Fatalf("ambiguous page retained: %v %v", pages, err)
	}
	pages, err = Parse(schedule.ProviderGrandEcran, map[string]bool{"cEvent": true, "cEvent-a": true}, []byte(`<urlset><url><loc>https://www.grandecran.fr/movies/cEvent-a-title/</loc></url></urlset>`))
	if err != nil || len(pages) != 0 {
		t.Fatalf("ambiguous source ID boundary retained: %v %v", pages, err)
	}
	for _, body := range []string{"", `<html>challenge</html>`, `<urlset>`, `<urlset></urlset><urlset/>`, `<urlset/>garbage`} {
		if _, err := Parse(schedule.ProviderNoeCinemas, nil, []byte(body)); err == nil {
			t.Fatalf("accepted malformed sitemap %q", body)
		}
	}
}

type indexStub struct {
	body  []byte
	err   error
	calls int
}

func (s *indexStub) MoviePageIndex(context.Context) ([]byte, error) { s.calls++; return s.body, s.err }

func TestEnrichmentIsAtomicAndDoesNotGuessMissingPages(t *testing.T) {
	data := schedule.Dataset{Provider: schedule.ProviderCGR, Showtimes: []schedule.ShowtimeRecord{
		{Movie: schedule.MovieRecord{ProviderID: "123", Title: "Edited title"}},
		{Movie: schedule.MovieRecord{ProviderID: "123", Title: "Edited title"}},
		{Movie: schedule.MovieRecord{ProviderID: "124"}},
	}}
	source := &indexStub{err: context.Canceled}
	if err := Enrich(t.Context(), source, &data); !errors.Is(err, context.Canceled) || data.Showtimes[0].Movie.SourcePageURL != "" {
		t.Fatal("failed enrichment changed data", err)
	}
	source.err = nil
	source.body = []byte(`<urlset><url><loc>https://www.cgrcinemas.fr/films-a-l-affiche/123-source-title/</loc></url></urlset>`)
	if err := Enrich(t.Context(), source, &data); err != nil {
		t.Fatal(err)
	}
	if source.calls != 2 || data.Showtimes[0].Movie.SourcePageURL == "" || data.Showtimes[1].Movie.SourcePageURL != data.Showtimes[0].Movie.SourcePageURL || data.Showtimes[2].Movie.SourcePageURL != "" {
		t.Fatalf("incorrect assignment: %+v", data.Showtimes)
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFetchRetriesServerErrorsButStopsChallengesAndRejectsRedirects(t *testing.T) {
	const target = "https://www.noecinemas.com/sitemap-0.xml"
	for _, status := range []int{500, 403, 429, 302} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			transport := transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != target {
					t.Fatal("unexpected request")
				}
				code, body := 200, `<urlset/>`
				if calls == 1 {
					code, body = status, "private provider error"
				}
				return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/xml"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			executor, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: []*http.Client{{Transport: transport}, {Transport: transport}}, ProxyBacked: true, MaxResponseBytes: 1024, ValidURL: func(u *url.URL) bool { return u.String() == target }, Retry: syncproxy.RetryPolicy{Sleep: func(context.Context, time.Duration) error { return nil }}})
			if err != nil {
				t.Fatal(err)
			}
			body, err := Fetch(t.Context(), executor, target)
			if status == 500 {
				if err != nil || calls != 2 || string(body) != `<urlset/>` {
					t.Fatalf("retry failed: %d %v", calls, err)
				}
			} else if err == nil || calls != 1 || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe error/retry: %d %v", calls, err)
			}
		})
	}
}
