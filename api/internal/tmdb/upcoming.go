package tmdb

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

type DiscoverPage struct {
	Page         int
	TotalPages   int
	TotalResults int
	IDs          []int64
}

// DiscoverMovies returns candidates, never verified release evidence.
func (c *Client) DiscoverMovies(ctx context.Context, from, through string, page int) (DiscoverPage, error) {
	for _, value := range []string{from, through} {
		if date, err := time.Parse(time.DateOnly, value); err != nil || date.Format(time.DateOnly) != value {
			return DiscoverPage{}, fmt.Errorf("tmdb discover window is invalid")
		}
	}
	if from > through || page < 1 || page > 500 {
		return DiscoverPage{}, fmt.Errorf("tmdb discover query is invalid")
	}
	var response struct {
		Page         *int `json:"page"`
		TotalPages   *int `json:"total_pages"`
		TotalResults *int `json:"total_results"`
		Results      []struct {
			ID int64 `json:"id"`
		} `json:"results"`
	}
	query := url.Values{"language": {"fr-FR"}, "region": {"FR"}, "include_adult": {"false"}, "include_video": {"false"}, "with_release_type": {"2|3"}, "release_date.gte": {from}, "release_date.lte": {through}, "sort_by": {"primary_release_date.asc"}, "page": {strconv.Itoa(page)}}
	if err := c.get(ctx, "/3/discover/movie", query, &response); err != nil {
		return DiscoverPage{}, err
	}
	if response.Page == nil || response.TotalPages == nil || response.TotalResults == nil || response.Results == nil {
		return DiscoverPage{}, fmt.Errorf("tmdb discover response is invalid")
	}
	result := DiscoverPage{Page: *response.Page, TotalPages: *response.TotalPages, TotalResults: *response.TotalResults, IDs: []int64{}}
	if result.Page != page || result.TotalPages < 0 || result.TotalPages > 500 || result.TotalResults < 0 || result.TotalResults > 10000 || result.TotalPages != (result.TotalResults+19)/20 || (page > result.TotalPages && (page != 1 || result.TotalPages != 0)) {
		return DiscoverPage{}, fmt.Errorf("tmdb discover pagination is invalid")
	}
	expected := 20
	if page >= result.TotalPages {
		expected = result.TotalResults - (page-1)*20
	}
	if len(response.Results) != expected {
		return DiscoverPage{}, fmt.Errorf("tmdb discover results are incomplete")
	}
	for _, item := range response.Results {
		if item.ID <= 0 {
			return DiscoverPage{}, fmt.Errorf("tmdb discover movie ID is invalid")
		}
		result.IDs = append(result.IDs, item.ID)
	}
	return result, nil
}

var releaseTimestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`)

// FrenchTheatricalReleaseDate preserves the written calendar date, not its UTC conversion.
// Empty evidence is distinct from an invalid response and from HTTP 404.
func (c *Client) FrenchTheatricalReleaseDate(ctx context.Context, id int64) (string, error) {
	if id <= 0 {
		return "", fmt.Errorf("tmdb movie ID is invalid")
	}
	var response struct {
		ID      int64 `json:"id"`
		Results []struct {
			Country string `json:"iso_3166_1"`
			Dates   []struct {
				Type int    `json:"type"`
				Date string `json:"release_date"`
			} `json:"release_dates"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/3/movie/"+strconv.FormatInt(id, 10)+"/release_dates", nil, &response); err != nil {
		return "", err
	}
	if response.ID != id || response.Results == nil {
		return "", fmt.Errorf("tmdb release response is invalid")
	}
	earliest := ""
	for _, country := range response.Results {
		if country.Country != "FR" {
			continue
		}
		if country.Dates == nil {
			return "", fmt.Errorf("tmdb release response is invalid")
		}
		for _, release := range country.Dates {
			if release.Type != 2 && release.Type != 3 {
				continue
			}
			if !releaseTimestamp.MatchString(release.Date) {
				return "", fmt.Errorf("tmdb release date is invalid")
			}
			if _, err := time.Parse(time.RFC3339Nano, release.Date); err != nil {
				return "", fmt.Errorf("tmdb release date is invalid")
			}
			date := release.Date[:10]
			if earliest == "" || date < earliest {
				earliest = date
			}
		}
	}
	return earliest, nil
}
