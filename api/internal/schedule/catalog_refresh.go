package schedule

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// RefreshPublishedMovie synchronously observes a committed catalog publication.
func (s *Service) RefreshPublishedMovie(ctx context.Context, slug string) error {
	if s == nil {
		return ErrNoCompleteSnapshot
	}
	refresher, ok := s.source.(interface{ Refresh(context.Context) error })
	if !ok {
		return fmt.Errorf("catalog refresh unavailable")
	}
	if err := refresher.Refresh(ctx); err != nil {
		return err
	}
	return s.RefreshMovie(ctx, slug)
}

// RefreshMovie refreshes a missing canonical identity only. Public arbitrary
// text/legacy aliases cannot trigger a database load; imports return film-ID.
func (s *Service) RefreshMovie(ctx context.Context, slug string) error {
	if s == nil {
		return ErrNoCompleteSnapshot
	}
	if view := s.source.Snapshot(); view != nil {
		if _, ok := view.resolveMovieSlug(slug); ok {
			return nil
		}
	}
	raw, ok := strings.CutPrefix(slug, "film-")
	id, err := strconv.ParseInt(raw, 10, 64)
	if !ok || err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw {
		return &NotFoundError{Message: "Film introuvable."}
	}
	refresher, ok := s.source.(interface{ Refresh(context.Context) error })
	if !ok {
		return &NotFoundError{Message: "Film introuvable."}
	}
	if err := refresher.Refresh(ctx); err != nil {
		return err
	}
	if view := s.source.Snapshot(); view != nil {
		if _, ok := view.resolveMovieSlug(slug); ok {
			return nil
		}
	}
	return fmt.Errorf("published movie unavailable")
}
