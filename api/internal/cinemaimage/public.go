package cinemaimage

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

// PublicImage exposes only normalized dimensions and the current public route.
type PublicImage struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// PublicImage reads current metadata independently of schedule snapshots. It
// neither reads files nor creates metadata for theaters without an image.
func (s *Service) PublicImage(ctx context.Context, id Identity) (*PublicImage, error) {
	if !s.available() {
		return nil, nil
	}
	if !validIdentity(id) {
		return nil, ErrRequest
	}
	r, err := s.repo.Current(ctx, id)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrImageNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if r.Key == "" {
		return nil, nil
	}
	if r.Revision < 1 || r.Revision > MaxRevision || !validRecord(r) {
		return nil, ErrStorage
	}
	return &PublicImage{
		URL:   "/api/v1/theaters/" + url.PathEscape(id.Provider) + "/" + url.PathEscape(id.ProviderTheaterID) + "/image/" + strconv.FormatInt(r.Revision, 10),
		Width: r.Width, Height: r.Height,
	}, nil
}
