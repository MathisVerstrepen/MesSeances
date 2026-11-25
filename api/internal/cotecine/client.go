package cotecine

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

const sourceName = "Cotecine"

// MaxResponseBytes bounds one programme page; the largest observed pages
// list two weeks of sessions in under 1 MiB.
const MaxResponseBytes = 8 << 20

// programmeRoute is the Cinegroup page listing the whole programme.
const programmeRoute = "seances/"

// OperationProgramme covers the single programme request of a site.
var OperationProgramme = syncproxy.Operation{Name: "programme", Phase: syncproxy.PhaseProgram}

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}

// Client fetches Cinegroup programme pages through the proxies.
type Client struct{ executor *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if err := syncproxy.ValidateSourceConfig("cotecine", config.Proxies, config.Timeout); err != nil {
		return nil, err
	}
	clients, err := syncproxy.NewHTTPClients(config.Proxies, config.Timeout, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		return nil, fmt.Errorf("invalid Cotecine proxy transport")
	}
	return newClient(clients, nil)
}

// ProgrammeURL is the programme page of a configured site.
func ProgrammeURL(website string) string { return website + programmeRoute }

// allowedURL accepts the programme page of any valid Cinegroup site; Sync
// only builds URLs from configured sources.
func allowedURL(u *url.URL) bool {
	if u == nil || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return false
	}
	raw := u.String()
	website, ok := strings.CutSuffix(raw, programmeRoute)
	return ok && schedule.ValidCotecineWebsite(website)
}

func newClient(clients []*http.Client, sleep func(context.Context, time.Duration) error) (*Client, error) {
	e, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: clients, ProxyBacked: true, ValidURL: allowedURL, MaxResponseBytes: MaxResponseBytes, Headers: http.Header{"User-Agent": {"Mozilla/5.0"}, "Accept-Language": {"fr-FR,fr;q=0.9"}}, CancelReadOnContext: true, Retry: syncproxy.RetryPolicy{Sleep: sleep, PreserveFailureAfterWait: true}})
	if err != nil {
		return nil, fmt.Errorf("invalid Cotecine executor")
	}
	return &Client{executor: e}, nil
}

func (c *Client) RequestCount() int { return c.executor.RequestCount() }

// Programme fetches the programme page of a configured site.
func (c *Client) Programme(ctx context.Context, website string) ([]byte, error) {
	raw := ProgrammeURL(website)
	u, err := url.Parse(raw)
	if !schedule.ValidCotecineWebsite(website) || err != nil || !allowedURL(u) {
		return nil, syncproxy.InvalidRequest(sourceName, OperationProgramme)
	}
	body, failure := c.executor.Do(ctx, syncproxy.Request{Method: http.MethodGet, URL: raw, MediaTypes: []string{"text/html", "application/xhtml+xml"}, NoRedirect: true}, syncproxy.StandardResponsePolicy())
	if failure == nil {
		return body, nil
	}
	return nil, syncproxy.NewRequestError(sourceName, OperationProgramme, http.MethodGet, raw, failure)
}
