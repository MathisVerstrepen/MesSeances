package ocine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"messeances/api/internal/boxoffice"
	"messeances/api/internal/parallel"
	"messeances/api/internal/schedule"
)

type Getter interface {
	Page(context.Context, string, string) ([]byte, error)
	Checkout(context.Context, string) ([]byte, error)
	RequestCount() int
}
type SyncOptions struct {
	From string
	Now  time.Time
	// Clock observes checkout failures independently of the fixed programme date.
	// It defaults to time.Now and must be safe for concurrent use.
	Clock func() time.Time
}
type SyncSummary struct {
	Cinemas, Movies, Requests, Showtimes             int
	GeneratedAt                                      time.Time
	EndsAttempted, EndsRecovered, RoomsMissing       int
	CheckoutTransientFailures, CheckoutBudgetSkipped int
}

// Sync requires the complete fixed census. Optional checkout enrichment runs
// within a separate budget after programme acquisition.
// Observed dates never attest full activity coverage.
func Sync(ctx context.Context, g Getter, o SyncOptions) (result schedule.Dataset, summary SyncSummary, resultErr error) {
	defer func() { summary.Requests = g.RequestCount() }()
	summary.GeneratedAt = o.Now.UTC()
	clock := o.Clock
	if clock == nil {
		clock = time.Now
	}
	loc, err := schedule.SyncLocation(o.From)
	if err != nil || o.Now.IsZero() || o.From != o.Now.In(loc).Format(time.DateOnly) {
		return result, summary, fmt.Errorf("invalid Ocine sync date")
	}
	source, native := g.(boxofficeGetter)
	programmes, err := parallel.MapOrdered(ctx, cinemaIDs[:], parallel.Options{Workers: 2}, func(ctx context.Context, id string) (programme, error) {
		if native {
			p, err := loadBoxofficeProgramme(ctx, source, id, o.From, loc)
			if errors.Is(err, boxoffice.ErrSnapshotChanged) && ctx.Err() == nil {
				p, err = loadBoxofficeProgramme(ctx, source, id, o.From, loc)
			}
			if err != nil {
				return programme{}, fmt.Errorf("cinema Ociné %s: %w", id, err)
			}
			return p, nil
		}
		body, err := g.Page(ctx, id, "seances")
		if err != nil {
			return programme{}, err
		}
		p, err := parseProgramme(body, id, o.From, o.Now, loc)
		if err != nil {
			return programme{}, err
		}
		body, err = g.Page(ctx, id, "plan-d-acces")
		if err != nil {
			return programme{}, err
		}
		p.theater.Address, p.theater.City, p.theater.PostalCode, err = parseAddress(body, id)
		return p, err
	})
	if err != nil {
		return result, summary, err
	}
	summary.Cinemas = len(programmes)
	builder := schedule.NewDatasetBuilder(schedule.ProviderOcine, o.From, o.Now, schedule.BuilderOptions{RejectPosterConflicts: true})
	for _, p := range programmes {
		for _, m := range p.movies {
			if !builder.MergeMovie(m) {
				return result, summary, fmt.Errorf("%w: conflicting movie metadata", errShape)
			}
		}
	}
	summary.Movies = builder.MovieCount()
	bookingOwners := map[string]string{}
	for _, p := range programmes {
		builder.ExtendWindow(p.through)
		for _, s := range p.showtimes {
			s.Movie = builder.Movie(s.Movie.ProviderID)
			if prior, ok := builder.Showtime(s.ProviderShowingID); ok {
				if !reflect.DeepEqual(prior, s) {
					return result, summary, fmt.Errorf("%w: conflicting duplicate showing", errShape)
				}
				continue
			}
			if s.BookingURL != "" {
				if owner, ok := bookingOwners[s.BookingURL]; ok && owner != s.ProviderShowingID {
					return result, summary, fmt.Errorf("%w: booking assigned to different films", errShape)
				}
				bookingOwners[s.BookingURL] = s.ProviderShowingID
			}
			if err := builder.AddShowtime(s); err != nil {
				return result, summary, fmt.Errorf("%w: conflicting duplicate showing", errShape)
			}
		}
		builder.AddTheater(p.theater)
	}
	if err := enrichCheckouts(ctx, g, builder.Showtimes(), &summary, clock, loc); err != nil {
		return result, summary, err
	}
	if err := ctx.Err(); err != nil {
		return result, summary, err
	}
	d, err := builder.Build("Ocine")
	if err != nil {
		return result, summary, err
	}
	summary.Showtimes = len(d.Showtimes)
	return d, summary, nil
}
