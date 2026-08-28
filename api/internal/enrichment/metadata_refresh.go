package enrichment

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"messeances/api/internal/tmdb"
)

var (
	ErrMetadataRefreshInProgress  = errors.New("TMDB metadata refresh already in progress")
	ErrMetadataRefreshUnavailable = errors.New("TMDB metadata refresh unavailable")
)

type MetadataRefreshSummary struct {
	Processed int `json:"processed"`
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
	Failed    int `json:"failed"`
}

type MetadataRefreshStore interface {
	MatchedTMDBIDs(context.Context) ([]int64, error)
	Metadata(context.Context, string, int64, string) (Metadata, bool, error)
	RefreshMetadata(context.Context, []Metadata) error
}

type MetadataRefreshService struct {
	store    MetadataRefreshStore
	provider interface {
		Details(context.Context, int64) (tmdb.Details, error)
	}
	now  func() time.Time
	gate *TMDBRunGate
}

func NewMetadataRefreshService(store MetadataRefreshStore, provider interface {
	Details(context.Context, int64) (tmdb.Details, error)
}, now func() time.Time, gate *TMDBRunGate) *MetadataRefreshService {
	if now == nil {
		now = time.Now
	}
	if gate == nil {
		gate = NewTMDBRunGate()
	}
	return &MetadataRefreshService{store: store, provider: provider, now: now, gate: gate}
}

func (s *MetadataRefreshService) Refresh(ctx context.Context) (MetadataRefreshSummary, error) {
	if !s.available() {
		return MetadataRefreshSummary{}, ErrMetadataRefreshUnavailable
	}
	if !s.gate.tryAcquire() {
		return MetadataRefreshSummary{}, ErrMetadataRefreshInProgress
	}
	defer s.gate.release()
	return s.refresh(ctx)
}

func (s *MetadataRefreshService) refresh(ctx context.Context) (MetadataRefreshSummary, error) {
	matchedIDs, err := s.store.MatchedTMDBIDs(ctx)
	if err != nil {
		return MetadataRefreshSummary{}, fmt.Errorf("read matched TMDB IDs failed: %w", err)
	}
	ids := distinctPositiveIDs(matchedIDs)
	summary := MetadataRefreshSummary{Processed: len(ids)}
	refreshed := make([]Metadata, 0, len(ids))
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return MetadataRefreshSummary{}, err
		}
		details, err := s.provider.Details(ctx, id)
		if err != nil {
			if errors.Is(err, tmdb.ErrStop) {
				return MetadataRefreshSummary{}, ErrMetadataRefreshUnavailable
			}
			if ctx.Err() != nil {
				return MetadataRefreshSummary{}, ctx.Err()
			}
			summary.Failed++
			continue
		}
		if details.ID != id {
			summary.Failed++
			continue
		}
		metadata := metadataFromDetails(details, 0, s.now().UTC())
		if err := validateMetadata(metadata); err != nil {
			summary.Failed++
			continue
		}
		cached, found, err := s.store.Metadata(ctx, ProviderTMDB, id, LocaleFrench)
		if err != nil {
			return MetadataRefreshSummary{}, fmt.Errorf("read cached TMDB metadata failed: %w", err)
		}
		refreshed = append(refreshed, metadata)
		if found && sameMetadataContent(cached, metadata) {
			summary.Unchanged++
		} else {
			summary.Updated++
		}
	}
	if len(refreshed) > 0 {
		if err := s.store.RefreshMetadata(ctx, refreshed); err != nil {
			return MetadataRefreshSummary{}, fmt.Errorf("publish refreshed TMDB metadata failed: %w", err)
		}
	}
	return summary, nil
}

func (s *MetadataRefreshService) available() bool {
	return s != nil && s.store != nil && s.provider != nil && s.gate != nil
}

func distinctPositiveIDs(ids []int64) []int64 {
	unique := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id > 0 {
			unique[id] = struct{}{}
		}
	}
	result := make([]int64, 0, len(unique))
	for id := range unique {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func sameMetadataContent(left, right Metadata) bool {
	if left.Provider != right.Provider || left.ProviderMovieID != right.ProviderMovieID || left.IMDBID != right.IMDBID || left.Locale != right.Locale || left.ProviderTitle != right.ProviderTitle || left.LocalizedTitle != right.LocalizedTitle || left.Overview != right.Overview || left.ReleaseDate != right.ReleaseDate || left.PosterURL != right.PosterURL || left.BackdropURL != right.BackdropURL || left.TrailerVFYouTubeKey != right.TrailerVFYouTubeKey || left.TrailerVOYouTubeKey != right.TrailerVOYouTubeKey || left.RuntimeMinutes != right.RuntimeMinutes || len(left.Genres) != len(right.Genres) {
		return false
	}
	for index := range left.Genres {
		if left.Genres[index] != right.Genres[index] {
			return false
		}
	}
	return true
}
