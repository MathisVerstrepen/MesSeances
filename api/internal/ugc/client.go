package ugc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxResponseBytes = 8 << 20

var (
	errRedirectHost  = errors.New("redirect host rejected")
	errRedirectLimit = errors.New("redirect limit exceeded")
)

type ClientConfig struct {
	Proxies         []Proxy
	RequestInterval time.Duration
	Timeout         time.Duration
}
type TerminalError struct{ category string }

func (e *TerminalError) Error() string { return "UGC request stopped: " + e.category }

type FetchResult struct {
	Body     []byte
	FinalURL string
}

type Client struct {
	mu           sync.Mutex
	config       ClientConfig
	clients      []*http.Client
	unavailable  []bool
	next         int
	lastStart    time.Time
	requestCount int
}

func NewClient(config ClientConfig) (*Client, error) {
	if len(config.Proxies) == 0 {
		return nil, fmt.Errorf("at least one proxy is required")
	}
	if config.RequestInterval < time.Second {
		return nil, fmt.Errorf("request interval must be at least 1s")
	}
	if config.Timeout < 5*time.Second || config.Timeout > 60*time.Second {
		return nil, fmt.Errorf("timeout must be between 5s and 60s")
	}
	client := &Client{config: config, unavailable: make([]bool, len(config.Proxies))}
	for _, proxy := range config.Proxies {
		transport := &http.Transport{Proxy: http.ProxyURL(proxy.endpoint), ForceAttemptHTTP2: true, MaxIdleConns: 1, MaxIdleConnsPerHost: 1, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: config.Timeout}
		httpClient := &http.Client{Transport: transport, Timeout: config.Timeout, CheckRedirect: checkUGCRedirect}
		client.clients = append(client.clients, httpClient)
	}
	return client, nil
}

func (c *Client) RequestCount() int { c.mu.Lock(); defer c.mu.Unlock(); return c.requestCount }

func (c *Client) Get(ctx context.Context, kind, rawURL string) (FetchResult, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || !isAllowedUGCURL(parsed) {
		return FetchResult{}, fmt.Errorf("%s request rejected: invalid public URL", kind)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	first := c.availableFrom(c.next)
	if first < 0 {
		return FetchResult{}, fmt.Errorf("%s request failed: no proxy available", kind)
	}
	for attempt := 0; attempt < 2; attempt++ {
		ordinal := first
		if attempt == 1 {
			ordinal = c.availableFrom((first + 1) % len(c.clients))
			if ordinal < 0 || ordinal == first {
				return FetchResult{}, fmt.Errorf("%s request failed after proxy %d", kind, first+1)
			}
		}
		if err := c.pace(ctx); err != nil {
			return FetchResult{}, fmt.Errorf("%s request canceled", kind)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return FetchResult{}, fmt.Errorf("%s request rejected", kind)
		}
		req.Header.Set("User-Agent", "MovieFlow-schedule-sync/1.0")
		if kind == "sitemap" {
			req.Header.Set("Accept", "application/xml,text/xml;q=0.9")
		} else {
			req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9")
		}
		c.lastStart = time.Now()
		c.requestCount++
		response, requestErr := c.clients[ordinal].Do(req)
		if requestErr != nil {
			if errors.Is(requestErr, errRedirectHost) || errors.Is(requestErr, errRedirectLimit) {
				return FetchResult{}, fmt.Errorf("%s request failed on proxy %d: redirect rejected", kind, ordinal+1)
			}
			c.unavailable[ordinal] = true
			if attempt == 0 {
				continue
			}
			return FetchResult{}, fmt.Errorf("%s request failed on proxy %d: transport", kind, ordinal+1)
		}
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
			response.Body.Close()
			return FetchResult{}, &TerminalError{category: fmt.Sprintf("HTTP %d for %s via proxy %d", response.StatusCode, kind, ordinal+1)}
		}
		body, readErr := readBounded(response.Body)
		response.Body.Close()
		if readErr != nil {
			return FetchResult{}, fmt.Errorf("%s request failed on proxy %d: response too large or unreadable", kind, ordinal+1)
		}
		if isChallenge(body) {
			return FetchResult{}, &TerminalError{category: fmt.Sprintf("challenge response for %s via proxy %d", kind, ordinal+1)}
		}
		if response.StatusCode >= 500 {
			c.unavailable[ordinal] = true
			if attempt == 0 {
				continue
			}
			return FetchResult{}, fmt.Errorf("%s request failed on proxy %d: server error", kind, ordinal+1)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return FetchResult{}, fmt.Errorf("%s request failed on proxy %d: HTTP %d", kind, ordinal+1, response.StatusCode)
		}
		finalURL, finalURLErr := sanitizedFinalURL(response.Request)
		if finalURLErr != nil {
			return FetchResult{}, fmt.Errorf("%s request failed on proxy %d: invalid final response URL", kind, ordinal+1)
		}
		c.next = (ordinal + 1) % len(c.clients)
		return FetchResult{Body: body, FinalURL: finalURL}, nil
	}
	return FetchResult{}, fmt.Errorf("%s request failed", kind)
}

func sanitizedFinalURL(request *http.Request) (string, error) {
	if request == nil || !isAllowedUGCURL(request.URL) || request.URL.Fragment != "" || request.URL.Opaque != "" {
		return "", fmt.Errorf("invalid final URL")
	}
	final := *request.URL
	final.Scheme = "https"
	final.Host = "www.ugc.fr"
	final.User = nil
	return final.String(), nil
}

func isAllowedUGCURL(parsed *url.URL) bool {
	return parsed != nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Host, "www.ugc.fr") && parsed.User == nil
}

func checkUGCRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > 3 {
		return errRedirectLimit
	}
	if req == nil || !isAllowedUGCURL(req.URL) {
		return errRedirectHost
	}
	return nil
}

func (c *Client) availableFrom(start int) int {
	for offset := 0; offset < len(c.clients); offset++ {
		index := (start + offset) % len(c.clients)
		if !c.unavailable[index] {
			return index
		}
	}
	return -1
}
func (c *Client) pace(ctx context.Context) error {
	if c.lastStart.IsZero() {
		return nil
	}
	wait := c.config.RequestInterval - time.Since(c.lastStart)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func readBounded(r io.Reader) ([]byte, error) {
	reader := io.LimitReader(r, maxResponseBytes+1)
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("too large")
	}
	return body, nil
}
func isChallenge(body []byte) bool {
	value := strings.ToLower(string(body))
	markers := []string{"<title>datadome", "<title>captcha", "<title>attention required! | cloudflare", "captcha-delivery.com", "geo.captcha-delivery.com", "class=\"g-recaptcha", "id=\"captcha", "action=\"/captcha", "cf-chl-", "cloudflare ray id", "challenge-platform", "/cdn-cgi/challenge", "id=\"challenge-form\""}
	for _, marker := range markers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
