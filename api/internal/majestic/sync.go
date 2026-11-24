package majestic

import (
	"context"
	"fmt"
	"time"

	"messeances/api/internal/parallel"
	"messeances/api/internal/schedule"
)

type Getter interface {
	Program(context.Context, string) ([]byte, error)
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

// Sync stages every cinema before returning a publishable provider snapshot.
func Sync(ctx context.Context, getter Getter, options SyncOptions) (result schedule.Dataset, summary SyncSummary, resultErr error) {
	defer func() { summary.Requests = getter.RequestCount() }()
	summary.GeneratedAt = options.Now.UTC()
	location, err := schedule.SyncLocation(options.From)
	if err != nil {
		return result, summary, err
	}
	summary.Cinemas, summary.Jobs = len(sites), len(sites)
	programs, err := parallel.MapOrdered(ctx, sites[:], parallel.Options{Workers: 2}, func(ctx context.Context, id string) (program, error) {
		body, err := getter.Program(ctx, id)
		if err != nil {
			return program{}, err
		}
		return parseProgram(body, id)
	})
	if err != nil {
		return result, summary, err
	}
	builder := schedule.NewDatasetBuilder(schedule.ProviderMajestic, options.From, options.Now, schedule.BuilderOptions{})
	for i, p := range programs {
		for _, e := range p.Events {
			id := movieID(sites[i], e.ID)
			if !builder.MergeMovie(schedule.MovieRecord{Provider: schedule.ProviderMajestic, ProviderID: id, Slug: "majestic-film-" + id, Title: clean(e.Title), RuntimeMinutes: int(e.Duration), PosterURL: posterURL(e.BillURL, e.ID)}) {
				return result, summary, fmt.Errorf("%w: conflicting Majestic movie", schedule.ErrDatasetValidation)
			}
		}
	}
	summary.Movies = builder.MovieCount()
	seen := map[string]bool{}
	for i, p := range programs {
		if err := ctx.Err(); err != nil {
			return result, summary, err
		}
		site := sites[i]
		theaterID := "majestic-" + site
		for _, e := range p.Events {
			m := builder.Movie(movieID(site, e.ID))
			for _, s := range e.Sessions {
				// Duplicates are rejected before date filtering, past sessions included.
				if seen[s.ID] {
					return result, summary, fmt.Errorf("%w: duplicate Majestic showing", schedule.ErrDatasetValidation)
				}
				seen[s.ID] = true
				start, err := parseStart(s.Date, location)
				if err != nil {
					return result, summary, err
				}
				date := start.Format("2006-01-02")
				if date < options.From {
					continue
				}
				end, ok := schedule.MegaramaEnd(start, m.RuntimeMinutes, int(s.FirstPartDuration))
				if !ok {
					return result, summary, errShape
				}
				language, format, version, err := attributes(s)
				if err != nil {
					return result, summary, err
				}
				booking := s.BookingURL
				if booking == "" {
					booking = schedule.MajesticWebsite(site)
				}
				builder.ExtendWindow(date)
				if err := builder.AddShowtime(schedule.ShowtimeRecord{Provider: schedule.ProviderMajestic, ID: "majestic-showing-" + s.ID, ProviderShowingID: s.ID, TheaterID: theaterID, ServiceDate: date, Movie: m, StartTime: start, EndTime: end, FirstPartDurationMinutes: int(s.FirstPartDuration), Language: language, ProviderVersion: version, Format: format, Room: s.HallName, BookingURL: booking}); err != nil {
					return result, summary, fmt.Errorf("%w: duplicate Majestic showing", schedule.ErrDatasetValidation)
				}
			}
		}
		builder.AddTheater(schedule.TheaterRecord{Provider: schedule.ProviderMajestic, ID: theaterID, ProviderID: site, Slug: theaterID, Name: clean(p.Name), Address: clean(p.Address), City: clean(p.City), PostalCode: clean(p.ZipCode), AcceptedPasses: []string{}})
	}
	dataset, err := builder.Build("Majestic")
	if err != nil {
		return result, summary, err
	}
	summary.Showtimes = len(dataset.Showtimes)
	return dataset, summary, nil
}
