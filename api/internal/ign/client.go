package ign

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"messeances/api/internal/geocoding"
)

const (
	defaultEndpoint = "https://data.geopf.fr/geocodage/search"
	maxResponseSize = 1 << 20
	requestInterval = 200 * time.Millisecond
)

type SleepFunc func(context.Context, time.Duration) error

type Config struct {
	Timeout   time.Duration
	Transport http.RoundTripper
	Endpoint  string
	Now       func() time.Time
	Sleep     SleepFunc
}

type Client struct {
	http      *http.Client
	endpoint  *url.URL
	now       func() time.Time
	sleep     SleepFunc
	lastStart time.Time
}

func NewClient(config Config) (*Client, error) {
	if config.Timeout < 5*time.Second || config.Timeout > 60*time.Second {
		return nil, fmt.Errorf("invalid IGN client configuration")
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" && parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid IGN client configuration")
	}
	if config.Transport == nil {
		config.Transport = http.DefaultTransport
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Sleep == nil {
		config.Sleep = sleepContext
	}
	client := &Client{endpoint: parsed, now: config.Now, sleep: config.Sleep}
	client.http = &http.Client{Timeout: config.Timeout, Transport: config.Transport, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 || request.URL.Scheme != parsed.Scheme || request.URL.Host != parsed.Host {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	return client, nil
}

func (c *Client) Search(ctx context.Context, query geocoding.Query) ([]geocoding.Candidate, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("IGN request canceled")
	}
	requestURL := *c.endpoint
	values := requestURL.Query()
	values.Set("q", strings.Join([]string{query.Address, query.PostalCode, query.City}, " "))
	values.Set("postcode", query.PostalCode)
	values.Set("index", "address")
	values.Set("autocomplete", "0")
	values.Set("limit", "3")
	requestURL.RawQuery = values.Encode()
	backoffs := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second}
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			if err := c.sleep(ctx, backoffs[attempt-1]); err != nil {
				return nil, fmt.Errorf("IGN request canceled")
			}
		}
		if err := c.pace(ctx); err != nil {
			return nil, fmt.Errorf("IGN request canceled")
		}
		candidates, retry, err := c.request(ctx, requestURL.String())
		if err == nil {
			return candidates, nil
		}
		if !retry || attempt == 3 {
			return nil, err
		}
	}
	return nil, fmt.Errorf("IGN request failed")
}

func (c *Client) pace(ctx context.Context) error {
	now := c.now()
	if !c.lastStart.IsZero() {
		wait := c.lastStart.Add(requestInterval).Sub(now)
		if wait > 0 {
			if err := c.sleep(ctx, wait); err != nil {
				return err
			}
		}
	}
	c.lastStart = c.now()
	return nil
}

func (c *Client) request(ctx context.Context, target string) ([]geocoding.Candidate, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, false, fmt.Errorf("IGN request failed")
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, true, fmt.Errorf("IGN request failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseSize+1))
		return nil, true, fmt.Errorf("IGN request failed")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, false, fmt.Errorf("IGN request failed")
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" && mediaType != "application/geo+json" {
		return nil, false, fmt.Errorf("IGN response is invalid")
	}
	limited := io.LimitReader(response.Body, maxResponseSize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, true, fmt.Errorf("IGN response read failed")
	}
	if len(body) > maxResponseSize {
		return nil, false, fmt.Errorf("IGN response is invalid")
	}
	var payload featureCollection
	if err := json.Unmarshal(body, &payload); err != nil || payload.Type != "FeatureCollection" || payload.Features == nil {
		return nil, false, fmt.Errorf("IGN response is invalid")
	}
	result := make([]geocoding.Candidate, 0, len(payload.Features))
	for _, feature := range payload.Features {
		candidate := geocoding.Candidate{Label: feature.Properties.Label, PostalCode: feature.Properties.Postcode, City: feature.Properties.City, Type: feature.Properties.Type}
		if feature.Properties.Score != nil {
			candidate.Score, candidate.HasScore = *feature.Properties.Score, true
		}
		if feature.Geometry.Type == "Point" && len(feature.Geometry.Coordinates) == 2 {
			candidate.Longitude, candidate.Latitude, candidate.HasCoordinates = feature.Geometry.Coordinates[0], feature.Geometry.Coordinates[1], true
		}
		result = append(result, candidate)
	}
	return result, false, nil
}

type featureCollection struct {
	Type     string    `json:"type"`
	Features []feature `json:"features"`
}

type feature struct {
	Geometry struct {
		Type        string    `json:"type"`
		Coordinates []float64 `json:"coordinates"`
	} `json:"geometry"`
	Properties struct {
		Label    string   `json:"label"`
		Score    *float64 `json:"score"`
		Postcode string   `json:"postcode"`
		City     string   `json:"city"`
		Type     string   `json:"type"`
	} `json:"properties"`
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
