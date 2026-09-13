package enrichment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/tmdb"
)

type UpcomingRelease struct {
	TMDBID            int64
	FrenchReleaseDate string
	Active            bool
	FrenchReleases    []tmdb.FrenchReleaseRow
	ReasonCodes       []string
}

type UpcomingPublication struct {
	CompletedAt time.Time
	Window      schedule.Window
	Releases    []UpcomingRelease
	Metadata    []Metadata
}

type UpcomingStore interface {
	RetainedUpcomingIDs(context.Context) ([]int64, error)
	Metadata(context.Context, string, int64, string) (Metadata, bool, error)
	PublishUpcoming(context.Context, UpcomingPublication) error
}

type UpcomingProvider interface {
	DiscoverMovies(context.Context, string, string, int) (tmdb.DiscoverPage, error)
	FrenchReleaseEvidence(context.Context, int64) (tmdb.ReleaseEvidence, error)
	Details(context.Context, int64) (tmdb.Details, error)
}

type UpcomingService struct {
	store    UpcomingStore
	provider UpcomingProvider
	now      func() time.Time
	gate     *TMDBRunGate
}

func NewUpcomingService(store UpcomingStore, provider UpcomingProvider, now func() time.Time, gate *TMDBRunGate) *UpcomingService {
	if now == nil {
		now = time.Now
	}
	if gate == nil {
		gate = NewTMDBRunGate()
	}
	return &UpcomingService{store: store, provider: provider, now: now, gate: gate}
}

// sync gathers all remote evidence before the single atomic publication. The manager owns the gate and session lease.
func (s *UpcomingService) sync(ctx context.Context) error {
	now := s.now().UTC()
	publication := UpcomingPublication{Window: schedule.UpcomingWindow(now), Releases: []UpcomingRelease{}, Metadata: []Metadata{}}
	retained, err := s.store.RetainedUpcomingIDs(ctx)
	if err != nil {
		return fmt.Errorf("read upcoming candidates failed")
	}
	ids := append([]int64{}, retained...)
	pages, total := 1, -1
	for page := 1; page <= pages; page++ {
		result, err := s.provider.DiscoverMovies(ctx, publication.Window.From, publication.Window.Through, page)
		if err != nil {
			return fmt.Errorf("discover upcoming movies failed")
		}
		if result.Page != page || result.TotalPages < 0 || result.TotalPages > 500 || result.TotalResults < 0 || result.TotalResults > 10000 || result.TotalPages != (result.TotalResults+19)/20 || (total >= 0 && (result.TotalResults != total || result.TotalPages != pages)) {
			return fmt.Errorf("upcoming pagination is inconsistent")
		}
		pages, total = result.TotalPages, result.TotalResults
		expected := 20
		if page >= pages {
			expected = total - (page-1)*20
		}
		if len(result.IDs) != expected {
			return fmt.Errorf("upcoming pagination is incomplete")
		}
		for _, id := range result.IDs {
			if id <= 0 {
				return fmt.Errorf("upcoming candidate is invalid")
			}
		}
		ids = append(ids, result.IDs...)
	}
	for _, id := range distinctPositiveIDs(ids) {
		if err := ctx.Err(); err != nil {
			return err
		}
		evidence, err := s.provider.FrenchReleaseEvidence(ctx, id)
		if errors.Is(err, tmdb.ErrNotFound) {
			evidence, err = tmdb.ReleaseEvidence{Rows: []tmdb.FrenchReleaseRow{}}, nil
		}
		if err != nil {
			return fmt.Errorf("verify upcoming release failed")
		}
		date := evidence.FrenchReleaseDate
		if date != "" {
			if parsed, err := time.Parse(time.DateOnly, date); err != nil || parsed.Format(time.DateOnly) != date {
				return fmt.Errorf("upcoming release date is invalid")
			}
		}
		release := UpcomingRelease{TMDBID: id, FrenchReleaseDate: date, Active: date >= publication.Window.From && date <= publication.Window.Through, FrenchReleases: evidence.Rows, ReasonCodes: AssessUpcoming(evidence.Rows, date)}
		if release.Active {
			cached, found, err := s.store.Metadata(ctx, ProviderTMDB, id, LocaleFrench)
			if err != nil {
				return fmt.Errorf("read upcoming metadata failed")
			}
			if !found || !now.Before(cached.RefreshAfter) {
				details, err := s.provider.Details(ctx, id)
				if errors.Is(err, tmdb.ErrNotFound) {
					release.Active, release.FrenchReleaseDate = false, ""
					release.FrenchReleases, release.ReasonCodes = []tmdb.FrenchReleaseRow{}, []string{}
				} else {
					if err != nil || details.ID != id {
						return fmt.Errorf("read upcoming metadata failed")
					}
					cached = metadataFromDetails(details, 0, now)
					if err := validateMetadata(cached); err != nil {
						return fmt.Errorf("upcoming metadata is invalid")
					}
					publication.Metadata = append(publication.Metadata, cached)
				}
			} else if err := validateMetadata(cached); err != nil {
				return fmt.Errorf("upcoming cached metadata is invalid")
			}
		}
		publication.Releases = append(publication.Releases, release)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	publication.CompletedAt = s.now().UTC()
	return s.store.PublishUpcoming(ctx, publication)
}
