package tmdb

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type Poster struct {
	URL      string  `json:"url"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Language *string `json:"language"`
}

// Posters returns artwork in every language, including language-neutral images.
func (c *Client) Posters(ctx context.Context, id int64) ([]Poster, error) {
	if id <= 0 {
		return nil, fmt.Errorf("tmdb movie ID is invalid")
	}
	var response struct {
		ID      int64 `json:"id"`
		Posters []struct {
			Path     string  `json:"file_path"`
			Width    int     `json:"width"`
			Height   int     `json:"height"`
			Language *string `json:"iso_639_1"`
		} `json:"posters"`
	}
	// Do not send language or include_image_language: both filter available images.
	if err := c.get(ctx, "/3/movie/"+strconv.FormatInt(id, 10)+"/images", nil, &response); err != nil {
		return nil, err
	}
	if response.ID != id {
		return nil, fmt.Errorf("tmdb images response is invalid")
	}
	posters := make([]Poster, 0, len(response.Posters))
	seen := make(map[string]bool, len(response.Posters))
	for _, image := range response.Posters {
		if !validPosterImagePath(image.Path) || image.Width <= 0 || image.Height <= 0 || seen[image.Path] {
			continue
		}
		if image.Language != nil {
			if !validOriginalLanguage(*image.Language) {
				continue
			}
			if *image.Language == "" {
				image.Language = nil
			}
		}
		imageURL, err := c.posterURL(ctx, image.Path)
		if err != nil {
			return nil, err
		}
		seen[image.Path] = true
		posters = append(posters, Poster{URL: imageURL, Width: image.Width, Height: image.Height, Language: image.Language})
	}
	return posters, nil
}

// TMDB paths are filenames, not arbitrary URLs or encoded path expressions.
func validPosterImagePath(path string) bool {
	if len(path) < 2 || len(path) > 1024 || path[0] != '/' || strings.Contains(path, "..") {
		return false
	}
	for _, char := range path[1:] {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return path != "/."
}
