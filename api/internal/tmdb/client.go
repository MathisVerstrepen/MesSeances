package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	productionBaseURL = "https://api.themoviedb.org"
	maxResponseBytes  = 2 << 20
)

var ErrStop = errors.New("tmdb pass must stop")

type Candidate struct {
	ID            int64
	Title         string
	OriginalTitle string
	PosterURL     string
}

type Details struct {
	ID            int64
	Title         string
	OriginalTitle string
	Overview      string
	ReleaseDate   string
	PosterURL     string
	BackdropURL   string
	Runtime       int
	Genres        []string
}

type Config struct {
	HTTPClient      *http.Client
	BaseURL         string
	RequestInterval time.Duration
	Now             func() time.Time
	Sleep           func(context.Context, time.Duration) error
}

type Client struct {
	token        string
	http         *http.Client
	base         *url.URL
	interval     time.Duration
	now          func() time.Time
	sleep        func(context.Context, time.Duration) error
	mu           sync.Mutex
	nextStart    time.Time
	imageBase    string
	posterSize   string
	backdropSize string
}

func NewClient(token string) (*Client, error) {
	return NewClientWithConfig(token, Config{})
}

func NewClientWithConfig(token string, cfg Config) (*Client, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("tmdb credential is missing")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = productionBaseURL
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme != "https" && cfg.BaseURL == "" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("tmdb endpoint is invalid")
	}
	if cfg.BaseURL == "" && (base.Scheme != "https" || base.Host != "api.themoviedb.org") {
		return nil, fmt.Errorf("tmdb endpoint is invalid")
	}
	if cfg.BaseURL != "" && base.Hostname() != "127.0.0.1" && base.Hostname() != "localhost" && base.Hostname() != "::1" {
		return nil, fmt.Errorf("tmdb test endpoint is invalid")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	httpClient := *cfg.HTTPClient
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if cfg.RequestInterval == 0 {
		cfg.RequestInterval = 250 * time.Millisecond
	}
	if cfg.RequestInterval < 0 {
		return nil, fmt.Errorf("tmdb request interval is invalid")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Sleep == nil {
		cfg.Sleep = func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return &Client{token: token, http: &httpClient, base: base, interval: cfg.RequestInterval, now: cfg.Now, sleep: cfg.Sleep}, nil
}

func (c *Client) Search(ctx context.Context, title string) ([]Candidate, error) {
	query := url.Values{"query": {title}, "language": {"fr-FR"}, "region": {"FR"}, "include_adult": {"false"}, "page": {"1"}}
	var response struct {
		Results []struct {
			ID            int64  `json:"id"`
			Title         string `json:"title"`
			OriginalTitle string `json:"original_title"`
			PosterPath    string `json:"poster_path"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/3/search/movie", query, &response); err != nil {
		return nil, err
	}
	result := make([]Candidate, 0, min(20, len(response.Results)))
	for _, item := range response.Results {
		if len(result) == 20 {
			break
		}
		if item.ID <= 0 || !validText(item.Title, 1024) || !validText(item.OriginalTitle, 1024) {
			return nil, fmt.Errorf("tmdb search response is invalid")
		}
		candidate := Candidate{ID: item.ID, Title: item.Title, OriginalTitle: item.OriginalTitle}
		if item.PosterPath != "" {
			posterURL, err := c.posterURL(ctx, item.PosterPath)
			if err != nil {
				return nil, err
			}
			candidate.PosterURL = posterURL
		}
		result = append(result, candidate)
	}
	return result, nil
}

func (c *Client) Details(ctx context.Context, id int64) (Details, error) {
	if id <= 0 {
		return Details{}, fmt.Errorf("tmdb movie ID is invalid")
	}
	var response struct {
		ID            int64  `json:"id"`
		Title         string `json:"title"`
		OriginalTitle string `json:"original_title"`
		Overview      string `json:"overview"`
		ReleaseDate   string `json:"release_date"`
		PosterPath    string `json:"poster_path"`
		BackdropPath  string `json:"backdrop_path"`
		Runtime       int    `json:"runtime"`
		Genres        []struct {
			Name string `json:"name"`
		} `json:"genres"`
	}
	if err := c.get(ctx, "/3/movie/"+strconv.FormatInt(id, 10), url.Values{"language": {"fr-FR"}}, &response); err != nil {
		return Details{}, err
	}
	if response.ID != id || !validText(response.Title, 1024) || !validText(response.OriginalTitle, 1024) || response.Runtime < 0 || response.Runtime > 600 || len(response.Overview) > 10000 {
		return Details{}, fmt.Errorf("tmdb movie response is invalid")
	}
	if response.ReleaseDate != "" {
		if parsed, err := time.Parse("2006-01-02", response.ReleaseDate); err != nil || parsed.Format("2006-01-02") != response.ReleaseDate {
			return Details{}, fmt.Errorf("tmdb movie response is invalid")
		}
	}
	details := Details{ID: response.ID, Title: response.Title, OriginalTitle: response.OriginalTitle, Overview: response.Overview, ReleaseDate: response.ReleaseDate, Runtime: response.Runtime, Genres: []string{}}
	for _, genre := range response.Genres {
		if !validText(genre.Name, 256) || len(details.Genres) == 32 {
			return Details{}, fmt.Errorf("tmdb movie response is invalid")
		}
		details.Genres = append(details.Genres, genre.Name)
	}
	if response.PosterPath != "" {
		posterURL, err := c.posterURL(ctx, response.PosterPath)
		if err != nil {
			return Details{}, err
		}
		details.PosterURL = posterURL
	}
	if response.BackdropPath != "" {
		backdropURL, err := c.backdropURL(ctx, response.BackdropPath)
		if err != nil {
			return Details{}, err
		}
		details.BackdropURL = backdropURL
	}
	return details, nil
}

func (c *Client) posterURL(ctx context.Context, path string) (string, error) {
	if len(path) < 2 || len(path) > 1024 || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "..") || strings.ContainsAny(path, "?#\\") {
		return "", fmt.Errorf("tmdb poster path is invalid")
	}
	base, posterSize, _, err := c.configuration(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(base, "/") + "/" + posterSize + path, nil
}

func (c *Client) backdropURL(ctx context.Context, path string) (string, error) {
	if len(path) < 2 || len(path) > 1024 || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "..") || strings.ContainsAny(path, "%?#\\") {
		return "", fmt.Errorf("tmdb backdrop path is invalid")
	}
	base, _, backdropSize, err := c.configuration(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(base, "/") + "/" + backdropSize + path, nil
}

func (c *Client) configuration(ctx context.Context) (string, string, string, error) {
	c.mu.Lock()
	if c.imageBase != "" {
		base, posterSize, backdropSize := c.imageBase, c.posterSize, c.backdropSize
		c.mu.Unlock()
		return base, posterSize, backdropSize, nil
	}
	c.mu.Unlock()
	var response struct {
		Images struct {
			SecureBaseURL string   `json:"secure_base_url"`
			PosterSizes   []string `json:"poster_sizes"`
			BackdropSizes []string `json:"backdrop_sizes"`
		} `json:"images"`
	}
	if err := c.get(ctx, "/3/configuration", nil, &response); err != nil {
		return "", "", "", err
	}
	parsed, err := url.Parse(response.Images.SecureBaseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "image.tmdb.org" || parsed.User != nil || parsed.Path != "/t/p/" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", "", fmt.Errorf("tmdb image configuration is invalid")
	}
	posterFound := false
	for _, size := range response.Images.PosterSizes {
		if size == "w500" {
			posterFound = true
			break
		}
	}
	backdropFound := false
	for _, size := range response.Images.BackdropSizes {
		if size == "w780" {
			backdropFound = true
			break
		}
	}
	if !posterFound || !backdropFound {
		return "", "", "", fmt.Errorf("tmdb image configuration is invalid")
	}
	c.mu.Lock()
	c.imageBase, c.posterSize, c.backdropSize = response.Images.SecureBaseURL, "w500", "w780"
	c.mu.Unlock()
	return response.Images.SecureBaseURL, "w500", "w780", nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, destination any) error {
	if err := c.pace(ctx); err != nil {
		return fmt.Errorf("tmdb request canceled")
	}
	endpoint := *c.base
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("tmdb request failed")
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("tmdb request failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
		return ErrStop
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("tmdb request failed")
	}
	reader := io.LimitReader(response.Body, maxResponseBytes+1)
	body, err := io.ReadAll(reader)
	if err != nil || len(body) > maxResponseBytes {
		return fmt.Errorf("tmdb response is invalid")
	}
	if err := json.Unmarshal(body, destination); err != nil {
		return fmt.Errorf("tmdb response is invalid")
	}
	return nil
}

func (c *Client) pace(ctx context.Context) error {
	c.mu.Lock()
	now := c.now()
	delay := c.nextStart.Sub(now)
	if delay < 0 {
		delay = 0
	}
	c.nextStart = now.Add(delay).Add(c.interval)
	c.mu.Unlock()
	if delay == 0 {
		return nil
	}
	return c.sleep(ctx, delay)
}

func validText(value string, maximum int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= maximum
}
