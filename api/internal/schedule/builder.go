package schedule

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"
)

// ErrDuplicateShowing reports a second showtime with an already added
// provider showing ID.
var ErrDuplicateShowing = errors.New("duplicate provider showing")

// BuilderOptions tunes provider-specific merge rules.
type BuilderOptions struct {
	// RejectPosterConflicts treats two different non-empty posters for the same
	// movie as contradictory metadata.
	RejectPosterConflicts bool
}

// DatasetBuilder assembles one provider snapshot. Connectors add theaters,
// movies and showtimes as they parse them; the builder merges movie metadata
// seen in several cinemas, rejects duplicate showings, derives each theater's
// available dates and returns a sorted, validated dataset.
//
// The window always starts at From. Connectors extend it with ExtendWindow
// because providers disagree on what the last advertised day is.
type DatasetBuilder struct {
	options   BuilderOptions
	data      Dataset
	movies    map[string]MovieRecord
	showtimes map[string]int
}

// NewDatasetBuilder starts an all-cinemas snapshot for provider, generated at
// now and covering from onward.
func NewDatasetBuilder(provider Provider, from string, now time.Time, options BuilderOptions) *DatasetBuilder {
	return &DatasetBuilder{
		options: options,
		data: Dataset{
			SchemaVersion: SchemaVersion, Provider: provider, Scope: ScopeAll, GeneratedAt: now.UTC(), Timezone: Timezone,
			Window: Window{From: from, Through: from}, Theaters: []TheaterRecord{}, Showtimes: []ShowtimeRecord{},
		},
		movies:    map[string]MovieRecord{},
		showtimes: map[string]int{},
	}
}

// MergeMovie records m under its ProviderID. A later sighting fills a missing
// runtime or poster; it reports false when the title or a known runtime (or,
// with RejectPosterConflicts, a known poster) contradicts an earlier sighting.
func (b *DatasetBuilder) MergeMovie(m MovieRecord) bool {
	prior, ok := b.movies[m.ProviderID]
	if !ok {
		b.movies[m.ProviderID] = m
		return true
	}
	if prior.Title != m.Title || prior.RuntimeMinutes != 0 && m.RuntimeMinutes != 0 && prior.RuntimeMinutes != m.RuntimeMinutes {
		return false
	}
	if b.options.RejectPosterConflicts && prior.PosterURL != "" && m.PosterURL != "" && prior.PosterURL != m.PosterURL {
		return false
	}
	if prior.RuntimeMinutes == 0 {
		prior.RuntimeMinutes = m.RuntimeMinutes
	}
	if prior.PosterURL == "" {
		prior.PosterURL = m.PosterURL
	}
	b.movies[m.ProviderID] = prior
	return true
}

// Movie returns the merged record for a provider movie ID.
func (b *DatasetBuilder) Movie(providerID string) MovieRecord { return b.movies[providerID] }

// UpdateMovie replaces an already merged movie, for example after a poster
// lookup. It ignores unknown IDs.
func (b *DatasetBuilder) UpdateMovie(m MovieRecord) {
	if _, ok := b.movies[m.ProviderID]; ok {
		b.movies[m.ProviderID] = m
	}
}

// MovieIDs returns the merged movie IDs in ascending order.
func (b *DatasetBuilder) MovieIDs() []string {
	ids := make([]string, 0, len(b.movies))
	for id := range b.movies {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// MovieCount returns the number of distinct merged movies.
func (b *DatasetBuilder) MovieCount() int { return len(b.movies) }

// AddTheater appends a theater. Its AvailableDates are derived by Build from
// the showtimes added for it, so any value set here is replaced.
func (b *DatasetBuilder) AddTheater(t TheaterRecord) {
	b.data.Theaters = append(b.data.Theaters, t)
}

// Showtime returns the showtime already added under providerShowingID, so a
// connector can apply its own duplicate policy before AddShowtime.
func (b *DatasetBuilder) Showtime(providerShowingID string) (ShowtimeRecord, bool) {
	i, ok := b.showtimes[providerShowingID]
	if !ok {
		return ShowtimeRecord{}, false
	}
	return b.data.Showtimes[i], true
}

// AddShowtime appends s. It fails with ErrDuplicateShowing when its provider
// showing ID was already added.
func (b *DatasetBuilder) AddShowtime(s ShowtimeRecord) error {
	if _, ok := b.showtimes[s.ProviderShowingID]; ok {
		return ErrDuplicateShowing
	}
	b.showtimes[s.ProviderShowingID] = len(b.data.Showtimes)
	b.data.Showtimes = append(b.data.Showtimes, s)
	return nil
}

// Showtimes exposes the added showtimes in insertion order. The slice shares
// storage with the builder so enrichment can refine records (end time, room)
// in place before Build; it must not change identities, theaters or dates.
func (b *DatasetBuilder) Showtimes() []ShowtimeRecord { return b.data.Showtimes }

// ShowtimeCount returns the number of added showtimes.
func (b *DatasetBuilder) ShowtimeCount() int { return len(b.data.Showtimes) }

// ExtendWindow moves the window end to date when date is later.
func (b *DatasetBuilder) ExtendWindow(date string) {
	if date > b.data.Window.Through {
		b.data.Window.Through = date
	}
}

// Build derives available dates, sorts showtimes by theater, start and ID,
// and validates the snapshot. label names the provider in the error.
func (b *DatasetBuilder) Build(label string) (Dataset, error) {
	dates := map[string]map[string]bool{}
	for _, s := range b.data.Showtimes {
		if dates[s.TheaterID] == nil {
			dates[s.TheaterID] = map[string]bool{}
		}
		dates[s.TheaterID][s.ServiceDate] = true
	}
	for i := range b.data.Theaters {
		available := make([]string, 0, len(dates[b.data.Theaters[i].ID]))
		for date := range dates[b.data.Theaters[i].ID] {
			available = append(available, date)
		}
		sort.Strings(available)
		b.data.Theaters[i].AvailableDates = available
	}
	SortShowtimes(b.data.Showtimes)
	if err := ValidateDataset(b.data, true); err != nil {
		return Dataset{}, fmt.Errorf("%w: invalid synchronized %s dataset", ErrDatasetValidation, label)
	}
	return b.data, nil
}

// SortShowtimes orders showtimes by theater, start time and ID.
func SortShowtimes(showtimes []ShowtimeRecord) {
	slices.SortStableFunc(showtimes, func(a, b ShowtimeRecord) int {
		if a.TheaterID != b.TheaterID {
			if a.TheaterID < b.TheaterID {
				return -1
			}
			return 1
		}
		if c := a.StartTime.Compare(b.StartTime); c != 0 {
			return c
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
}

// SyncLocation loads the schedule timezone and checks that from is a
// canonical date in it.
func SyncLocation(from string) (*time.Location, error) {
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		return nil, fmt.Errorf("load schedule timezone")
	}
	day, err := time.ParseInLocation(time.DateOnly, from, location)
	if err != nil || day.Format(time.DateOnly) != from {
		return nil, fmt.Errorf("invalid sync date")
	}
	return location, nil
}
