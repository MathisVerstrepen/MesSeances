package cineode

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

const MaxResponseBytes = 16 << 20

// IDs preserve the approved sandbox order; routes are owned by schedule's manifest.
var cinemaIDs = [...]string{"4640", "597", "4077", "593", "4626", "592", "4388", "4582", "4622", "3524", "981", "3955", "4426", "991", "600", "595", "636", "4420", "603", "601", "594", "3859", "4444", "4624", "4638", "4277", "4386", "4384", "3064", "4075", "3074", "4592", "4181", "4281", "3857", "3945", "3104"}

// sourceName labels Cineode request errors.
const sourceName = "Cineode"

// operationPage covers every Cineode page request; all feed the programme.
var operationPage = syncproxy.Operation{Phase: syncproxy.PhaseProgram}

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}
type Client struct{ executor *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if err := syncproxy.ValidateSourceConfig("cineode", config.Proxies, config.Timeout); err != nil {
		return nil, err
	}
	clients, err := syncproxy.NewHTTPClients(config.Proxies, config.Timeout, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		return nil, fmt.Errorf("invalid Cineode proxy transport")
	}
	return newClient(clients, nil)
}

func allowedURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	for _, id := range cinemaIDs {
		for _, route := range []string{"seances", "infos-cine", "contact"} {
			if u.String() == "https://www.cineode.fr/"+schedule.CineodeSlug(id)+"/"+route+"/" {
				return true
			}
		}
	}
	return false
}

func newClient(clients []*http.Client, sleep func(context.Context, time.Duration) error) (*Client, error) {
	e, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: clients, ProxyBacked: true, ValidURL: allowedURL, MaxResponseBytes: MaxResponseBytes, Headers: http.Header{"User-Agent": {"Mozilla/5.0"}, "Accept-Language": {"fr-FR,fr;q=0.9"}}, CancelReadOnContext: true, Retry: syncproxy.RetryPolicy{Sleep: sleep, PreserveFailureAfterWait: true}})
	if err != nil {
		return nil, fmt.Errorf("invalid Cineode executor")
	}
	return &Client{executor: e}, nil
}
func (c *Client) RequestCount() int { return c.executor.RequestCount() }

// Page accepts only a verified cinema and one of the three acquisition routes.
func (c *Client) Page(ctx context.Context, id, route string) ([]byte, error) {
	if !schedule.ValidCineodeIdentity("theater", id) || route != "seances" && route != "infos-cine" && route != "contact" {
		return nil, syncproxy.InvalidRequest(sourceName, operationPage)
	}
	raw := "https://www.cineode.fr/" + schedule.CineodeSlug(id) + "/" + route + "/"
	var headers http.Header
	if route == "seances" {
		// The source can temporarily serve an obsolete or contradictory grid.
		// Ask caches to revalidate, including on the parser's bounded retry.
		headers = http.Header{"Cache-Control": {"no-cache"}}
	}
	body, failure := c.executor.Do(ctx, syncproxy.Request{Method: http.MethodGet, URL: raw, Headers: headers, MediaTypes: []string{"text/html", "application/xhtml+xml"}, NoRedirect: true}, syncproxy.StandardResponsePolicy())
	if failure == nil {
		return body, nil
	}
	if route != "seances" && failure.Kind == syncproxy.FailureStatus && failure.StatusCode == 404 {
		return nil, nil
	}
	return nil, syncproxy.NewRequestError(sourceName, operationPage, http.MethodGet, raw, failure)
}
