package enrichment

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"movieflow/api/internal/tmdb"
)

const reviewMetadataTTL = 30 * 24 * time.Hour

type ReviewStore interface {
	PendingMatches(context.Context, int, int) ([]PendingMatch, error)
	ReviewCandidate(context.Context, string, int64) (Candidate, error)
	ApproveReview(context.Context, string, int64, Metadata, time.Time) error
	RejectReview(context.Context, string, time.Time) error
}

type ReviewService struct {
	store    ReviewStore
	provider interface {
		Details(context.Context, int64) (tmdb.Details, error)
	}
	now func() time.Time
}

func NewReviewService(store ReviewStore, provider interface {
	Details(context.Context, int64) (tmdb.Details, error)
}, now func() time.Time) *ReviewService {
	if now == nil {
		now = time.Now
	}
	return &ReviewService{store: store, provider: provider, now: now}
}

func (s *ReviewService) Pending(ctx context.Context, limit, offset int) ([]PendingMatch, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("review service unavailable")
	}
	items, err := s.store.PendingMatches(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	for itemIndex := range items {
		item := &items[itemIndex]
		item.SourceDetailURL = ugcDetailURL(item.SourceMovieID)
		if !validUGCPosterURL(item.SourcePosterURL) {
			item.SourcePosterURL = ""
		}
		for candidateIndex := range item.Candidates {
			candidate := &item.Candidates[candidateIndex]
			candidate.DetailURL = tmdbDetailURL(candidate.ID)
			if !validTMDBPosterURL(candidate.PosterURL) {
				candidate.PosterURL = ""
			}
		}
	}
	return items, nil
}

func ugcDetailURL(sourceMovieID string) string {
	id, err := strconv.ParseInt(sourceMovieID, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != sourceMovieID {
		return ""
	}
	result := (&url.URL{Scheme: "https", Host: "www.ugc.fr", Path: "/film.html", RawQuery: url.Values{"id": {sourceMovieID}}.Encode()}).String()
	if !validUGCDetailURL(result, sourceMovieID) {
		return ""
	}
	return result
}

func tmdbDetailURL(candidateID int64) string {
	if candidateID <= 0 {
		return ""
	}
	id := strconv.FormatInt(candidateID, 10)
	result := (&url.URL{Scheme: "https", Host: "www.themoviedb.org", Path: "/movie/" + id, RawQuery: url.Values{"language": {LocaleFrench}}.Encode()}).String()
	if !validTMDBDetailURL(result, id) {
		return ""
	}
	return result
}

func validUGCDetailURL(raw, sourceMovieID string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "www.ugc.fr" || parsed.User != nil || parsed.Path != "/film.html" || parsed.Fragment != "" {
		return false
	}
	query := parsed.Query()
	return len(query) == 1 && len(query["id"]) == 1 && query.Get("id") == sourceMovieID
}

func validTMDBDetailURL(raw, candidateID string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "www.themoviedb.org" || parsed.User != nil || parsed.Path != "/movie/"+candidateID || parsed.Fragment != "" {
		return false
	}
	query := parsed.Query()
	return len(query) == 1 && len(query["language"]) == 1 && query.Get("language") == LocaleFrench
}

func validUGCPosterURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.Host != strings.ToLower(parsed.Hostname()) || parsed.Path == "" {
		return false
	}
	host := parsed.Hostname()
	return host == "ugc.fr" || strings.HasSuffix(host, ".ugc.fr")
}

func validTMDBPosterURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && len(raw) <= 4096 && parsed.Scheme == "https" && parsed.Host == "image.tmdb.org" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && strings.HasPrefix(parsed.Path, "/t/p/") && len(parsed.Path) > len("/t/p/") && !strings.Contains(parsed.Path, "..")
}

func validTMDBBackdropURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	suffix := strings.TrimPrefix(parsed.Path, "/t/p/w780/")
	return len(raw) <= 4096 && strings.HasPrefix(raw, "https://image.tmdb.org/t/p/w780/") && parsed.Scheme == "https" && parsed.Host == "image.tmdb.org" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.RawPath == "" && strings.HasPrefix(parsed.Path, "/t/p/w780/") && suffix != "" && !strings.HasPrefix(suffix, "/") && !strings.Contains(parsed.Path, "..") && !strings.Contains(parsed.Path, "\\")
}

func (s *ReviewService) Approve(ctx context.Context, sourceMovieID string, candidateID int64) error {
	if s == nil || s.store == nil || candidateID <= 0 {
		return fmt.Errorf("review service unavailable")
	}
	if s.provider == nil {
		return ErrReviewUnavailable
	}
	if _, err := s.store.ReviewCandidate(ctx, sourceMovieID, candidateID); err != nil {
		return err
	}
	details, err := s.provider.Details(ctx, candidateID)
	if err != nil {
		return fmt.Errorf("fetch reviewed movie metadata failed")
	}
	if details.ID != candidateID {
		return fmt.Errorf("reviewed movie metadata is invalid")
	}
	now := s.now().UTC()
	metadata := metadataFromDetails(details, now)
	metadata.RefreshAfter = now.Add(reviewMetadataTTL)
	return s.store.ApproveReview(ctx, sourceMovieID, candidateID, metadata, now)
}

func (s *ReviewService) Reject(ctx context.Context, sourceMovieID string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("review service unavailable")
	}
	return s.store.RejectReview(ctx, sourceMovieID, s.now().UTC())
}
