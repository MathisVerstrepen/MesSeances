// Package sourcepages retains published film routes without deriving title slugs.
package sourcepages

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type indexSource interface {
	MoviePageIndex(context.Context) ([]byte, error)
}

// Enrich is optional for custom acquisition adapters. The production Boxoffice
// clients implement indexSource and use their existing proxy-only executors.
// Fetch/parse failures abort publication; missing or ambiguous pages stay null.
func Enrich(ctx context.Context, source any, data *schedule.Dataset) error {
	getter, ok := source.(indexSource)
	if !ok || len(data.Showtimes) == 0 {
		return nil
	}
	body, err := getter.MoviePageIndex(ctx)
	if err != nil {
		return err
	}
	ids := make(map[string]bool)
	for _, record := range data.Showtimes {
		ids[record.Movie.ProviderID] = true
	}
	pages, err := Parse(data.Provider, ids, body)
	if err != nil {
		return err
	}
	for i := range data.Showtimes {
		data.Showtimes[i].Movie.SourcePageURL = pages[data.Showtimes[i].Movie.ProviderID]
	}
	return nil
}

// Parse accepts a bounded sitemap URL set. Only requested, exactly bound movie
// identities are retained. A second distinct route for one ID is ambiguous.
func Parse(provider schedule.Provider, ids map[string]bool, body []byte) (map[string]string, error) {
	if len(body) == 0 || len(body) > 16<<20 {
		return nil, fmt.Errorf("invalid source movie sitemap size")
	}
	var document struct {
		XMLName xml.Name `xml:"urlset"`
		URLs    []struct {
			Location string `xml:"loc"`
		} `xml:"url"`
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("invalid source movie sitemap")
	}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid source movie sitemap trailer")
		}
		if whitespace, ok := token.(xml.CharData); !ok || strings.TrimSpace(string(whitespace)) != "" {
			return nil, fmt.Errorf("invalid source movie sitemap trailer")
		}
	}
	pages, ambiguous := make(map[string]string), make(map[string]bool)
	for _, entry := range document.URLs {
		u, err := url.Parse(entry.Location)
		if err != nil {
			continue
		}
		parts := strings.Split(u.Path, "/")
		if len(parts) != 4 {
			continue
		}
		// Numeric IDs and opaque event IDs can contain hyphens. Check each
		// possible boundary against the exact requested identity set.
		var matched string
		for i, char := range parts[2] {
			if char != '-' {
				continue
			}
			id := parts[2][:i]
			if !ids[id] || !schedule.ValidSourceMoviePageURL(provider, id, entry.Location) {
				continue
			}
			if matched != "" {
				matched = ""
				break
			}
			matched = id
		}
		if matched == "" {
			continue
		}
		if prior := pages[matched]; prior != "" && prior != entry.Location {
			ambiguous[matched] = true
		}
		pages[matched] = entry.Location
	}
	for id := range ambiguous {
		delete(pages, id)
	}
	return pages, nil
}

type RequestError struct {
	syncproxy.RequestDiagnostic
	Kind       syncproxy.FailureKind
	StatusCode int
	cause      error
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("source movie sitemap request failed (category %d, status %d)", e.Kind, e.StatusCode)
}
func (e *RequestError) Unwrap() error { return e.cause }

// Fetch shares the caller's URL policy, pacing, proxy rotation, counters and
// bounded reads. It never follows redirects or fetches individual sitemap URLs.
func Fetch(ctx context.Context, executor *syncproxy.Executor, target string) ([]byte, error) {
	body, failure := executor.Do(ctx, syncproxy.Request{Method: http.MethodGet, URL: target, NoRedirect: true,
		MediaTypes: []string{"application/xml", "text/xml"}, Headers: http.Header{"Accept": {"application/xml, text/xml"}}}, syncproxy.ResponsePolicy{
		BeforeRead: func(status int) (*syncproxy.Failure, bool) {
			if status == 403 || status == 429 {
				return &syncproxy.Failure{Kind: syncproxy.FailureChallenge, StatusCode: status}, false
			}
			return nil, false
		},
		AfterRead: func(status int, body []byte) (*syncproxy.Failure, bool) {
			if syncproxy.IsChallenge(body) {
				return &syncproxy.Failure{Kind: syncproxy.FailureChallenge}, false
			}
			if status >= 500 {
				return &syncproxy.Failure{Kind: syncproxy.FailureServer, StatusCode: status}, true
			}
			if status != http.StatusOK {
				return &syncproxy.Failure{Kind: syncproxy.FailureStatus, StatusCode: status}, false
			}
			return nil, false
		},
	})
	if failure == nil {
		return body, nil
	}
	err := &RequestError{Kind: failure.Kind, StatusCode: failure.StatusCode, RequestDiagnostic: syncproxy.NewRequestDiagnostic(http.MethodGet, target, failure)}
	if failure.Kind == syncproxy.FailureCanceled {
		err.cause = ctx.Err()
	}
	return nil, err
}
