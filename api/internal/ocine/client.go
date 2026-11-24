package ocine

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

var cinemaIDs = [...]string{"309", "310", "461"}

// sourceName labels Ocine request errors.
const sourceName = "Ocine"

// operationPage covers every Ocine page request; all feed the programme.
var operationPage = syncproxy.Operation{Phase: syncproxy.PhaseProgram}

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}
type Client struct{ executor *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if err := syncproxy.ValidateSourceConfig("ocine", config.Proxies, config.Timeout); err != nil {
		return nil, err
	}
	clients, err := syncproxy.NewHTTPClients(config.Proxies, config.Timeout, checkoutRedirect)
	if err != nil {
		return nil, fmt.Errorf("invalid Ocine proxy transport")
	}
	return newClient(clients, nil)
}

func allowedURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	for _, id := range cinemaIDs {
		base := "https://www.ocine.fr/" + schedule.OcineSlug(id) + "/"
		if u.String() == base+"seances/" || u.String() == base+"plan-d-acces/" {
			return true
		}
		if _, _, _, _, ok := schedule.OcineBookingParts(u.String(), id); ok {
			return true
		}
		if _, ok := schedule.OcineEntryID(u.String(), id); ok {
			return true
		}
		if _, _, _, _, ok := canonicalCheckoutParts(u.String(), id); ok {
			return true
		}
		if boxofficeURL(u, id) {
			return true
		}
	}
	return false
}

func newClient(clients []*http.Client, sleep func(context.Context, time.Duration) error) (*Client, error) {
	e, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: clients, ProxyBacked: true, ValidURL: allowedURL, MaxResponseBytes: MaxResponseBytes, Headers: http.Header{"User-Agent": {"Mozilla/5.0"}, "Accept-Language": {"fr-FR,fr;q=0.9"}}, CancelReadOnContext: true, Retry: syncproxy.RetryPolicy{Sleep: sleep, PreserveFailureAfterWait: true, RequestScopedFailures: true}})
	if err != nil {
		return nil, fmt.Errorf("invalid Ocine executor")
	}
	return &Client{executor: e}, nil
}

func (c *Client) RequestCount() int { return c.executor.RequestCount() }

func (c *Client) Page(ctx context.Context, id, route string) ([]byte, error) {
	if !schedule.ValidOcineIdentity("theater", id) || route != "seances" && route != "plan-d-acces" {
		return nil, syncproxy.InvalidRequest(sourceName, operationPage)
	}
	return c.get(ctx, "https://www.ocine.fr/"+schedule.OcineSlug(id)+"/"+route+"/")
}

func (c *Client) Checkout(ctx context.Context, raw string) ([]byte, error) {
	for _, id := range cinemaIDs {
		if _, _, _, _, ok := schedule.OcineBookingParts(raw, id); ok {
			return c.get(ctx, raw)
		}
	}
	return nil, syncproxy.InvalidRequest(sourceName, operationPage)
}

func (c *Client) get(ctx context.Context, raw string) ([]byte, error) {
	return c.fetch(ctx, raw, []string{"text/html", "application/xhtml+xml"}, false)
}

func (c *Client) fetch(ctx context.Context, raw string, media []string, checkout bool) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || u.String() != raw || !allowedURL(u) {
		return nil, syncproxy.InvalidRequest(sourceName, operationPage)
	}
	body, failure := c.executor.Do(ctx, syncproxy.Request{Method: http.MethodGet, URL: raw, MediaTypes: media, NoRedirect: !checkout, AllowRedirect: checkout}, syncproxy.StandardResponsePolicy())
	if failure == nil {
		return body, nil
	}
	return nil, syncproxy.NewRequestError(sourceName, operationPage, http.MethodGet, raw, failure)
}
