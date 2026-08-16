package enrichment

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	SourceUGC    = "ugc"
	ProviderTMDB = "tmdb"
	LocaleFrench = "fr-FR"

	StatusMatched        = "matched"
	StatusReviewRequired = "review_required"
	StatusUnmatched      = "unmatched"
	StatusRejected       = "rejected"
)

type Candidate struct {
	ID            int64   `json:"id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title,omitempty"`
	Runtime       int     `json:"runtime_minutes,omitempty"`
	Score         float64 `json:"score,omitempty"`
	PosterURL     string  `json:"poster_url,omitempty"`
	DetailURL     string  `json:"detail_url,omitempty"`
}

type Match struct {
	SourceProvider        string
	SourceMovieID         string
	MetadataProvider      string
	Status                string
	MetadataMovieID       int64
	Score                 float64
	NormalizedSourceTitle string
	SourceRuntimeMinutes  int
	Candidates            []Candidate
	EvaluatedAt           time.Time
	RetryAfter            time.Time
}

type Metadata struct {
	Provider        string
	ProviderMovieID int64
	Locale          string
	ProviderTitle   string
	LocalizedTitle  string
	Overview        string
	ReleaseDate     string
	PosterURL       string
	BackdropURL     string
	RuntimeMinutes  int
	Genres          []string
	FetchedAt       time.Time
	RefreshAfter    time.Time
}

type Movie struct {
	ProviderID     string
	Title          string
	RuntimeMinutes int
}

type PendingMatch struct {
	SourceMovieID        string      `json:"source_movie_id"`
	SourceTitle          string      `json:"source_title"`
	SourceRuntimeMinutes int         `json:"source_runtime_minutes"`
	SourcePosterURL      string      `json:"source_poster_url,omitempty"`
	SourceDetailURL      string      `json:"source_detail_url"`
	Status               string      `json:"status"`
	Candidates           []Candidate `json:"candidates"`
	EvaluatedAt          time.Time   `json:"evaluated_at"`
}

var (
	ErrReviewNotFound    = fmt.Errorf("review match not found")
	ErrReviewConflict    = fmt.Errorf("review match conflict")
	ErrReviewUnavailable = fmt.Errorf("review provider unavailable")
)

func validateMatch(match Match) error {
	movieID, parseErr := strconv.ParseInt(match.SourceMovieID, 10, 64)
	if match.SourceProvider != SourceUGC || match.MetadataProvider != ProviderTMDB || parseErr != nil || movieID <= 0 || strings.TrimSpace(match.NormalizedSourceTitle) == "" || len(match.NormalizedSourceTitle) > 1024 {
		return fmt.Errorf("invalid match identity")
	}
	if match.SourceRuntimeMinutes < 1 || match.SourceRuntimeMinutes > 600 || len(match.Candidates) > 5 || match.EvaluatedAt.IsZero() || match.RetryAfter.IsZero() {
		return fmt.Errorf("invalid match data")
	}
	for _, candidate := range match.Candidates {
		if candidate.ID <= 0 || strings.TrimSpace(candidate.Title) == "" || len(candidate.Title) > 1024 || len(candidate.OriginalTitle) > 1024 || candidate.Runtime < 0 || candidate.Runtime > 600 || candidate.Score < 0 || candidate.Score > 1 || candidate.DetailURL != "" || candidate.PosterURL != "" && !validTMDBPosterURL(candidate.PosterURL) {
			return fmt.Errorf("invalid match candidate")
		}
	}
	if match.Status == StatusMatched {
		if match.MetadataMovieID <= 0 || match.Score < 0 || match.Score > 1 {
			return fmt.Errorf("invalid matched decision")
		}
	} else if match.Status == StatusReviewRequired || match.Status == StatusUnmatched || match.Status == StatusRejected {
		if match.MetadataMovieID != 0 || match.Score != 0 {
			return fmt.Errorf("invalid unresolved decision")
		}
	} else {
		return fmt.Errorf("invalid match status")
	}
	return nil
}

func validateMetadata(metadata Metadata) error {
	if metadata.Provider != ProviderTMDB || metadata.ProviderMovieID <= 0 || metadata.Locale != LocaleFrench || strings.TrimSpace(metadata.ProviderTitle) == "" || len(metadata.ProviderTitle) > 1024 || strings.TrimSpace(metadata.LocalizedTitle) == "" || len(metadata.LocalizedTitle) > 1024 {
		return fmt.Errorf("invalid metadata identity")
	}
	if metadata.RuntimeMinutes < 1 || metadata.RuntimeMinutes > 600 || len(metadata.Overview) > 10000 || len(metadata.Genres) > 32 || metadata.FetchedAt.IsZero() || metadata.RefreshAfter.IsZero() {
		return fmt.Errorf("invalid metadata")
	}
	if metadata.ReleaseDate != "" {
		parsed, err := time.Parse("2006-01-02", metadata.ReleaseDate)
		if err != nil || parsed.Format("2006-01-02") != metadata.ReleaseDate {
			return fmt.Errorf("invalid metadata release date")
		}
	}
	if metadata.PosterURL != "" {
		parsed, err := url.Parse(metadata.PosterURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || len(metadata.PosterURL) > 4096 {
			return fmt.Errorf("invalid metadata poster URL")
		}
	}
	if metadata.BackdropURL != "" && !validTMDBBackdropURL(metadata.BackdropURL) {
		return fmt.Errorf("invalid metadata backdrop URL")
	}
	for _, genre := range metadata.Genres {
		if strings.TrimSpace(genre) == "" || len(genre) > 256 {
			return fmt.Errorf("invalid metadata genre")
		}
	}
	return nil
}
