package schedule

import (
	"cmp"
	"slices"
	"time"
)

const HomePreviewLimit = 4

type PreviewTheater struct {
	TheaterSummary
	Slug string `json:"slug"`
}

type NextScreening struct {
	MovieSlug   string         `json:"movie_slug"`
	Title       string         `json:"title"`
	PosterURL   *string        `json:"poster_url"`
	Theater     PreviewTheater `json:"theater"`
	StartTime   time.Time      `json:"start_time"`
	ServiceDate string         `json:"service_date"`
}

// NextScreenings picks at most one upcoming screening per canonical movie from one snapshot.
func (s *Service) NextScreenings(movieSlugs, theaterIDs []string) ([]NextScreening, error) {
	result := []NextScreening{}
	if len(movieSlugs) == 0 || len(theaterIDs) == 0 {
		return result, nil
	}
	if s == nil {
		return nil, ErrNoCompleteSnapshot
	}
	view := s.source.Snapshot()
	if view == nil || view.catalogOnly {
		return nil, ErrNoCompleteSnapshot
	}
	now := s.now()
	selected := make(map[string]bool, len(theaterIDs))
	for _, id := range theaterIDs {
		selected[id] = true
	}
	seen := make(map[string]bool, len(movieSlugs))
	for _, slug := range movieSlugs {
		canonical, ok := view.resolveMovieSlug(slug)
		if !ok || seen[canonical] {
			continue
		}
		seen[canonical] = true
		if item, found := nextScreening(view, now, canonical, selected); found {
			result = append(result, item)
		}
	}
	slices.SortFunc(result, func(a, b NextScreening) int {
		return cmp.Or(a.StartTime.Compare(b.StartTime), cmp.Compare(a.MovieSlug, b.MovieSlug))
	})
	return result[:min(len(result), HomePreviewLimit)], nil
}

func nextScreening(view *SnapshotView, now time.Time, slug string, selected map[string]bool) (NextScreening, bool) {
	var best *ShowtimeRecord
	for _, date := range view.movieDates[slug] {
		for _, position := range view.movieDate[movieDateKey{slug: slug, date: date}] {
			record := &view.data.Showtimes[position]
			if !selected[record.TheaterID] || !record.StartTime.After(now) {
				continue
			}
			if best == nil || cmp.Or(record.StartTime.Compare(best.StartTime), cmp.Compare(record.TheaterID, best.TheaterID), cmp.Compare(record.ID, best.ID)) < 0 {
				best = record
			}
		}
	}
	if best == nil {
		return NextScreening{}, false
	}
	theater := view.data.Theaters[view.theaterByID[best.TheaterID]]
	movie := materializeCatalogMovie(view, best.Movie)
	return NextScreening{
		MovieSlug: slug, Title: movie.Title, PosterURL: movie.PosterURL,
		Theater: PreviewTheater{
			TheaterSummary: TheaterSummary{ID: theater.ID, Name: theater.Name, City: theater.City, Provider: recordProvider(theater.Provider, theater.ID)},
			Slug:           theater.Slug,
		},
		StartTime: best.StartTime.UTC(), ServiceDate: best.ServiceDate,
	}, true
}

type ActivityPreviewItem struct {
	ActivityEvent
	Theater PreviewTheater `json:"theater"`
}
