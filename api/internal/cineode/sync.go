package cineode

import (
	"context"
	"errors"
	"fmt"
	"time"

	"messeances/api/internal/parallel"
	"messeances/api/internal/schedule"
)

type Getter interface {
	Page(context.Context, string, string) ([]byte, error)
	RequestCount() int
}
type SyncOptions struct {
	From string
	Now  time.Time
}
type SyncSummary struct {
	Cinemas, Movies, Jobs, Requests, Showtimes int
	GeneratedAt                                time.Time
}

// Sync returns no dataset until all 37 recognized programmes have succeeded.
// Session-date unions are not acquisition coverage evidence.
func Sync(ctx context.Context, getter Getter, options SyncOptions) (result schedule.Dataset, summary SyncSummary, resultErr error) {
	defer func() { summary.Requests = getter.RequestCount() }()
	summary.GeneratedAt = options.Now.UTC()
	location, err := schedule.SyncLocation(options.From)
	if err != nil {
		return result, summary, err
	}
	if options.Now.IsZero() {
		return result, summary, fmt.Errorf("invalid sync date")
	}
	summary.Cinemas, summary.Jobs = len(cinemaIDs), len(cinemaIDs)
	programmes, err := parallel.MapOrdered(ctx, cinemaIDs[:], parallel.Options{Workers: 2}, func(ctx context.Context, id string) (programme, error) {
		body, err := getter.Page(ctx, id, "seances")
		if err != nil {
			return programme{}, err
		}
		p, err := parseProgramme(body, id, options.From, options.Now, location)
		// Reacquire a contradictory empty page once. Only a fully validated
		// replacement may contribute to the dataset; persistent failures remain
		// atomic and must never clear this cinema's previously stored sessions.
		if errors.Is(err, errEmptyProgramme) {
			if err := ctx.Err(); err != nil {
				return programme{}, err
			}
			body, err = getter.Page(ctx, id, "seances")
			if err == nil {
				p, err = parseProgramme(body, id, options.From, options.Now, location)
			}
		}
		if err != nil {
			return programme{}, fmt.Errorf("cineode cinema %s programme: %w", id, err)
		}
		if p.theater.Address == "" {
			for _, route := range []string{"infos-cine", "contact"} {
				body, err := getter.Page(ctx, id, route)
				if err != nil {
					return programme{}, err
				}
				address, err := metadataAddress(body, id, route)
				if err != nil {
					return programme{}, err
				}
				if address != "" {
					setAddress(&p.theater, address)
					break
				}
			}
		}
		return p, nil
	})
	if err != nil {
		return result, summary, err
	}
	builder := schedule.NewDatasetBuilder(schedule.ProviderCineode, options.From, options.Now, schedule.BuilderOptions{RejectPosterConflicts: true})
	for _, p := range programmes {
		for _, m := range p.movies {
			if !builder.MergeMovie(m) {
				return result, summary, fmt.Errorf("%w: conflicting movie metadata", errShape)
			}
		}
	}
	summary.Movies = builder.MovieCount()
	for _, p := range programmes {
		if err := ctx.Err(); err != nil {
			return result, summary, err
		}
		builder.ExtendWindow(p.through)
		for _, s := range p.showtimes {
			s.Movie = builder.Movie(s.Movie.ProviderID)
			if prior, ok := builder.Showtime(s.ProviderShowingID); ok {
				if prior.Language != s.Language || prior.ProviderVersion != s.ProviderVersion || prior.Format != s.Format || prior.ServiceDate != s.ServiceDate || !prior.StartTime.Equal(s.StartTime) {
					return result, summary, fmt.Errorf("%w: conflicting duplicate showing", errShape)
				}
				continue
			}
			if err := builder.AddShowtime(s); err != nil {
				return result, summary, fmt.Errorf("%w: conflicting duplicate showing", errShape)
			}
		}
		builder.AddTheater(p.theater)
	}
	dataset, err := builder.Build("Cineode")
	if err != nil {
		return result, summary, err
	}
	summary.Showtimes = len(dataset.Showtimes)
	return dataset, summary, nil
}
