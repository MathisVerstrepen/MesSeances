package scare

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"messeances/api/internal/syncproxy"
)

const sourceName = "SCARE"

// LinesURL is the data-fair endpoint of the SCARE open programme dataset
// "Programmation des cinémas indépendants" (Licence Ouverte).
const LinesURL = "https://datacinesindes.fr/data-fair/api/v1/datasets/programmation-cinemas/lines"

// PageSize is the number of rows requested per page (the data-fair maximum).
const PageSize = 10000

// MaxResponseBytes bounds one page.
const MaxResponseBytes = 64 << 20

// OperationLines is the only SCARE request.
var OperationLines = syncproxy.Operation{Name: "lines", Phase: syncproxy.PhaseProgram}

// selectedFields are the only columns read from the dataset.
var selectedFields = []string{
	"cineid", "cinenom", "cineadresse", "cinecp", "cineville", "_coords.lat", "_coords.lon",
	"filmid", "filmtitle", "filmduration", "filmstoryline", "filmgenre",
	"showid", "showstart", "showend", "filmversion", "filmaudio", "showurl",
}

var (
	cinemaID = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	cursor   = regexp.MustCompile(`^[0-9A-Za-z_.,-]{1,256}$`)
	dateOnly = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
)

// Query selects one page of rows of the given cinemas starting at From.
type Query struct {
	Cinemas []string
	From    string
	// After is the opaque cursor of the previous page; empty for the first.
	After string
}

// URL builds the request URL, or "" when the query is invalid.
func (q Query) URL() string {
	if len(q.Cinemas) == 0 || !dateOnly.MatchString(q.From) || q.After != "" && !cursor.MatchString(q.After) {
		return ""
	}
	for _, id := range q.Cinemas {
		if !cinemaID.MatchString(id) {
			return ""
		}
	}
	values := url.Values{}
	values.Set("size", fmt.Sprint(PageSize))
	values.Set("cineid_in", strings.Join(q.Cinemas, ","))
	values.Set("showstart_gte", q.From)
	values.Set("select", strings.Join(selectedFields, ","))
	values.Set("sort", "cineid,showstart,_i")
	if q.After != "" {
		values.Set("after", q.After)
	}
	return LinesURL + "?" + values.Encode()
}

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}

// Client fetches dataset pages through the configured proxies.
type Client struct{ executor *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if err := syncproxy.ValidateSourceConfig("scare", config.Proxies, config.Timeout); err != nil {
		return nil, err
	}
	clients, err := syncproxy.NewHTTPClients(config.Proxies, config.Timeout, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		return nil, fmt.Errorf("invalid SCARE proxy transport")
	}
	return newClient(clients, nil)
}

func allowedURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.Host == "datacinesindes.fr" && u.User == nil && u.Path == "/data-fair/api/v1/datasets/programmation-cinemas/lines" && u.Fragment == ""
}

func newClient(clients []*http.Client, sleep func(context.Context, time.Duration) error) (*Client, error) {
	e, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: clients, ProxyBacked: true, ValidURL: allowedURL, MaxResponseBytes: MaxResponseBytes, Headers: http.Header{"User-Agent": {"Mozilla/5.0"}, "Accept": {"application/json"}}, CancelReadOnContext: true, Retry: syncproxy.RetryPolicy{Sleep: sleep, PreserveFailureAfterWait: true}})
	if err != nil {
		return nil, fmt.Errorf("invalid SCARE executor")
	}
	return &Client{executor: e}, nil
}

func (c *Client) RequestCount() int { return c.executor.RequestCount() }

// Lines fetches one page of rows.
func (c *Client) Lines(ctx context.Context, q Query) ([]byte, error) {
	raw := q.URL()
	if raw == "" {
		return nil, syncproxy.InvalidRequest(sourceName, OperationLines)
	}
	body, failure := c.executor.Do(ctx, syncproxy.Request{Method: http.MethodGet, URL: raw, NoRedirect: true}, syncproxy.StandardResponsePolicy())
	if failure == nil {
		return body, nil
	}
	return nil, syncproxy.NewRequestError(sourceName, OperationLines, http.MethodGet, LinesURL, failure)
}
