// Package wikidata acquires bounded Metacritic identifiers from direct item claims.
package wikidata

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 2 << 20

var errLookup = errors.New("wikidata lookup unavailable")

type Config struct {
	HTTPClient *http.Client
	// BaseURL is restricted to literal loopback origins for synthetic fixtures.
	BaseURL string
}

type Client struct {
	http *http.Client
	base *url.URL
}

func NewClient() *Client {
	c, _ := NewClientWithConfig(Config{})
	return c
}

func NewClientWithConfig(cfg Config) (*Client, error) {
	origin := cfg.BaseURL
	if origin == "" {
		origin = "https://www.wikidata.org"
	}
	base, err := url.Parse(origin)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.Path != "" || base.RawPath != "" || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" {
		return nil, errLookup
	}
	if cfg.BaseURL != "" && base.Hostname() != "127.0.0.1" && base.Hostname() != "::1" {
		return nil, errLookup
	}
	hc := http.Client{}
	if cfg.HTTPClient != nil {
		hc = *cfg.HTTPClient
	}
	hc.Jar = nil
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{http: &hc, base: base}, nil
}

func ValidQID(value string) bool {
	if len(value) < 2 || len(value) > 20 || value[0] != 'Q' || value[1] < '1' || value[1] > '9' {
		return false
	}
	for i := 2; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

// ValidMetacriticID accepts only complete movie paths, never URLs.
func ValidMetacriticID(value string) bool {
	return strings.HasPrefix(value, "movie/") && validPath(value)
}

func validPath(value string) bool {
	if len(value) > 255 {
		return false
	}
	category, slug, found := strings.Cut(value, "/")
	if !found || category == "" || slug == "" {
		return false
	}
	for i := range len(category) {
		if category[i] < 'a' || category[i] > 'z' {
			return false
		}
	}
	for i := range len(slug) {
		c := slug[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && !strings.ContainsRune("!+_()-", rune(c)) {
			return false
		}
	}
	return true
}

func (c *Client) MetacriticID(ctx context.Context, qid string) (string, error) {
	if !ValidQID(qid) {
		return "", errLookup
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	endpoint := *c.base
	endpoint.Path = "/w/api.php"
	endpoint.RawQuery = url.Values{"action": {"wbgetentities"}, "ids": {qid}, "props": {"claims"}, "redirects": {"no"}, "format": {"json"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", errLookup
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "MesSeancesMetacriticBot/1.0 (https://messeances.fr)")
	response, err := c.http.Do(req)
	if err != nil {
		return "", errLookup
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", errLookup
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return "", errLookup
	}
	return extract(body, qid)
}

func extract(body []byte, qid string) (string, error) {
	var envelope struct {
		Success  int                        `json:"success"`
		Error    json.RawMessage            `json:"error"`
		Entities map[string]json.RawMessage `json:"entities"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Success != 1 || len(envelope.Error) != 0 {
		return "", errLookup
	}
	var entity struct {
		ID       string          `json:"id"`
		Type     string          `json:"type"`
		Missing  json.RawMessage `json:"missing"`
		Deleted  json.RawMessage `json:"deleted"`
		Redirect json.RawMessage `json:"redirect"`
		Claims   json.RawMessage `json:"claims"`
	}
	if json.Unmarshal(envelope.Entities[qid], &entity) != nil || entity.ID != qid || entity.Type != "item" || len(entity.Missing) != 0 || len(entity.Deleted) != 0 || len(entity.Redirect) != 0 {
		return "", errLookup
	}
	claims := make(map[string]json.RawMessage)
	if len(entity.Claims) != 0 {
		if json.Unmarshal(entity.Claims, &claims) != nil || claims == nil {
			var empty []json.RawMessage
			if json.Unmarshal(entity.Claims, &empty) != nil || empty == nil || len(empty) != 0 {
				return "", errLookup
			}
		}
	}
	raw, present := claims["P1712"]
	if !present {
		return "", nil
	}
	var statements []json.RawMessage
	if json.Unmarshal(raw, &statements) != nil || statements == nil {
		return "", errLookup
	}
	candidate := ""
	for _, raw := range statements {
		var rank struct {
			Rank string `json:"rank"`
		}
		if json.Unmarshal(raw, &rank) != nil {
			return "", errLookup
		}
		if rank.Rank == "deprecated" {
			continue
		}
		var statement struct {
			Type     string `json:"type"`
			Rank     string `json:"rank"`
			Mainsnak struct {
				Property  string `json:"property"`
				Datatype  string `json:"datatype"`
				Snaktype  string `json:"snaktype"`
				Datavalue struct {
					Type  string          `json:"type"`
					Value json.RawMessage `json:"value"`
				} `json:"datavalue"`
			} `json:"mainsnak"`
		}
		if json.Unmarshal(raw, &statement) != nil {
			return "", errLookup
		}
		snak := statement.Mainsnak
		if statement.Type != "statement" || (statement.Rank != "normal" && statement.Rank != "preferred") || snak.Property != "P1712" || snak.Datatype != "external-id" {
			return "", errLookup
		}
		if snak.Snaktype == "novalue" {
			continue
		}
		var value string
		if snak.Snaktype != "value" || snak.Datavalue.Type != "string" || json.Unmarshal(snak.Datavalue.Value, &value) != nil || !validPath(value) {
			return "", errLookup
		}
		if !ValidMetacriticID(value) {
			continue
		}
		if candidate != "" && candidate != value {
			return "", errLookup
		}
		candidate = value
	}
	return candidate, nil
}
