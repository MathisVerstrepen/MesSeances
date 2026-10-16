package majestic

import (
	"context"
	"fmt"
	"sort"
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
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return result, summary, fmt.Errorf("load schedule timezone")
	}
	from, err := time.ParseInLocation("2006-01-02", options.From, location)
	if err != nil || from.Format("2006-01-02") != options.From {
		return result, summary, fmt.Errorf("invalid from date")
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
	movies := map[string]schedule.MovieRecord{}
	for i, p := range programs {
		for _, e := range p.Events {
			id := movieID(sites[i], e.ID)
			m := schedule.MovieRecord{Provider: schedule.ProviderMajestic, ProviderID: id, Slug: "majestic-film-" + id, Title: clean(e.Title), RuntimeMinutes: int(e.Duration), PosterURL: posterURL(e.BillURL, e.ID)}
			if prior, ok := movies[id]; ok {
				if prior.Title != m.Title || prior.RuntimeMinutes != 0 && m.RuntimeMinutes != 0 && prior.RuntimeMinutes != m.RuntimeMinutes {
					return result, summary, fmt.Errorf("%w: conflicting Majestic movie", schedule.ErrDatasetValidation)
				}
				if prior.RuntimeMinutes == 0 {
					prior.RuntimeMinutes = m.RuntimeMinutes
				}
				if prior.PosterURL == "" {
					prior.PosterURL = m.PosterURL
				}
				m = prior
			}
			movies[id] = m
		}
	}
	summary.Movies = len(movies)
	dataset := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderMajestic, Scope: schedule.ScopeAll, GeneratedAt: options.Now.UTC(), Timezone: schedule.Timezone, Window: schedule.Window{From: options.From, Through: options.From}, Theaters: []schedule.TheaterRecord{}, Showtimes: []schedule.ShowtimeRecord{}}
	seen := map[string]bool{}
	for i, p := range programs {
		if err := ctx.Err(); err != nil {
			return result, summary, err
		}
		site := sites[i]
		theaterID := "majestic-" + site
		dates := map[string]bool{}
		for _, e := range p.Events {
			m := movies[movieID(site, e.ID)]
			for _, s := range e.Sessions {
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
				dates[date] = true
				if date > dataset.Window.Through {
					dataset.Window.Through = date
				}
				dataset.Showtimes = append(dataset.Showtimes, schedule.ShowtimeRecord{Provider: schedule.ProviderMajestic, ID: "majestic-showing-" + s.ID, ProviderShowingID: s.ID, TheaterID: theaterID, ServiceDate: date, Movie: m, StartTime: start, EndTime: end, FirstPartDurationMinutes: int(s.FirstPartDuration), Language: language, ProviderVersion: version, Format: format, Room: s.HallName, BookingURL: booking})
			}
		}
		available := make([]string, 0, len(dates))
		for date := range dates {
			available = append(available, date)
		}
		sort.Strings(available)
		dataset.Theaters = append(dataset.Theaters, schedule.TheaterRecord{Provider: schedule.ProviderMajestic, ID: theaterID, ProviderID: site, Slug: theaterID, Name: clean(p.Name), Address: clean(p.Address), City: clean(p.City), PostalCode: clean(p.ZipCode), AvailableDates: available, AcceptedPasses: []string{}})
	}
	sort.Slice(dataset.Showtimes, func(i, j int) bool {
		a, b := dataset.Showtimes[i], dataset.Showtimes[j]
		if a.TheaterID != b.TheaterID {
			return a.TheaterID < b.TheaterID
		}
		if !a.StartTime.Equal(b.StartTime) {
			return a.StartTime.Before(b.StartTime)
		}
		return a.ID < b.ID
	})
	if err := schedule.ValidateDataset(dataset, true); err != nil {
		return result, summary, fmt.Errorf("%w: invalid synchronized Majestic dataset", schedule.ErrDatasetValidation)
	}
	summary.Showtimes = len(dataset.Showtimes)
	return dataset, summary, nil
}
