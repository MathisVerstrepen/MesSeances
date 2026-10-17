package cineode

import (
	"context"
	"fmt"
	"sort"
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
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return result, summary, fmt.Errorf("load schedule timezone")
	}
	from, err := time.ParseInLocation(time.DateOnly, options.From, location)
	if err != nil || from.Format(time.DateOnly) != options.From || options.Now.IsZero() {
		return result, summary, fmt.Errorf("invalid sync date")
	}
	summary.Cinemas, summary.Jobs = len(cinemaIDs), len(cinemaIDs)
	programmes, err := parallel.MapOrdered(ctx, cinemaIDs[:], parallel.Options{Workers: 2}, func(ctx context.Context, id string) (programme, error) {
		body, err := getter.Page(ctx, id, "seances")
		if err != nil {
			return programme{}, err
		}
		p, err := parseProgramme(body, id, options.From, options.Now, location)
		if err != nil {
			return programme{}, err
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
	movies := map[string]schedule.MovieRecord{}
	for _, p := range programmes {
		for _, m := range p.movies {
			if prior, ok := movies[m.ProviderID]; ok {
				if prior.Title != m.Title || prior.RuntimeMinutes != 0 && m.RuntimeMinutes != 0 && prior.RuntimeMinutes != m.RuntimeMinutes || prior.PosterURL != "" && m.PosterURL != "" && prior.PosterURL != m.PosterURL {
					return result, summary, fmt.Errorf("%w: conflicting movie metadata", errShape)
				}
				if prior.RuntimeMinutes == 0 {
					prior.RuntimeMinutes = m.RuntimeMinutes
				}
				if prior.PosterURL == "" {
					prior.PosterURL = m.PosterURL
				}
				m = prior
			}
			movies[m.ProviderID] = m
		}
	}
	summary.Movies = len(movies)
	dataset := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderCineode, Scope: schedule.ScopeAll, GeneratedAt: options.Now.UTC(), Timezone: schedule.Timezone, Window: schedule.Window{From: options.From, Through: options.From}, Theaters: []schedule.TheaterRecord{}, Showtimes: []schedule.ShowtimeRecord{}}
	seen := map[string]schedule.ShowtimeRecord{}
	for _, p := range programmes {
		if err := ctx.Err(); err != nil {
			return result, summary, err
		}
		if p.through > dataset.Window.Through {
			dataset.Window.Through = p.through
		}
		dates := map[string]bool{}
		for _, s := range p.showtimes {
			s.Movie = movies[s.Movie.ProviderID]
			if prior, ok := seen[s.ProviderShowingID]; ok {
				if prior.Language != s.Language || prior.ProviderVersion != s.ProviderVersion || prior.Format != s.Format || prior.ServiceDate != s.ServiceDate || !prior.StartTime.Equal(s.StartTime) {
					return result, summary, fmt.Errorf("%w: conflicting duplicate showing", errShape)
				}
				continue
			}
			seen[s.ProviderShowingID] = s
			dates[s.ServiceDate] = true
			dataset.Showtimes = append(dataset.Showtimes, s)
		}
		for date := range dates {
			p.theater.AvailableDates = append(p.theater.AvailableDates, date)
		}
		sort.Strings(p.theater.AvailableDates)
		dataset.Theaters = append(dataset.Theaters, p.theater)
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
		return result, summary, fmt.Errorf("%w: invalid synchronized Cineode dataset", schedule.ErrDatasetValidation)
	}
	summary.Showtimes = len(dataset.Showtimes)
	return dataset, summary, nil
}
