package megarama

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"messeances/api/internal/parallel"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type Getter interface {
	Config(context.Context) ([]byte, error)
	Program(context.Context, string, string) ([]byte, error)
	Poster(context.Context, string) ([]byte, error)
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
	body, err := getter.Config(ctx)
	if err != nil {
		return result, summary, err
	}
	cinemas, err := parseConfig(body)
	if err != nil {
		return result, summary, err
	}
	summary.Cinemas, summary.Jobs = len(cinemas), len(cinemas)
	programs, err := parallel.MapOrdered(ctx, cinemas, parallel.Options{Workers: 2}, func(ctx context.Context, c cinema) (program, error) {
		body, err := getter.Program(ctx, c.ID, c.Website)
		if err != nil {
			return program{}, err
		}
		return parseProgram(body, c)
	})
	if err != nil {
		return result, summary, err
	}
	movies := map[string]schedule.MovieRecord{}
	for i, p := range programs {
		for _, e := range p.Events {
			id := movieID(cinemas[i].ID, e.ID)
			m := schedule.MovieRecord{Provider: schedule.ProviderMegarama, ProviderID: id, Slug: "megarama-film-" + id, Title: e.Title, RuntimeMinutes: int(e.Duration), PosterURL: posterURL(e.BillURL, e.ID)}
			if prior, ok := movies[id]; ok {
				if prior.Title != m.Title || prior.RuntimeMinutes != 0 && m.RuntimeMinutes != 0 && prior.RuntimeMinutes != m.RuntimeMinutes {
					return result, summary, fmt.Errorf("%w: conflicting Megarama film metadata", schedule.ErrDatasetValidation)
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
	ids := make([]string, 0, len(movies))
	for id := range movies {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		m := movies[id]
		if m.PosterURL != "" || !globalID.MatchString(id) {
			continue
		}
		body, err := getter.Poster(ctx, id)
		if err != nil {
			if !optionalPosterFailure(err) {
				return result, summary, err
			}
			continue
		}
		m.PosterURL = parsePoster(body, id)
		movies[id] = m
	}
	summary.Movies = len(movies)
	dataset := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderMegarama, Scope: schedule.ScopeAll, GeneratedAt: options.Now.UTC(), Timezone: schedule.Timezone, Window: schedule.Window{From: options.From, Through: options.From}, Theaters: []schedule.TheaterRecord{}, Showtimes: []schedule.ShowtimeRecord{}}
	seen := map[string]bool{}
	for i, p := range programs {
		if err := ctx.Err(); err != nil {
			return result, summary, err
		}
		c := cinemas[i]
		theaterID := "megarama-" + c.ID
		dates := map[string]bool{}
		for _, e := range p.Events {
			m := movies[movieID(c.ID, e.ID)]
			for _, s := range e.Sessions {
				if seen[s.ID] {
					return result, summary, fmt.Errorf("%w: duplicate Megarama showing", schedule.ErrDatasetValidation)
				}
				seen[s.ID] = true
				start, err := parseStart(s.Date, location)
				if err != nil {
					return result, summary, fmt.Errorf("%w: session start", err)
				}
				service := start
				if service.Hour() < 3 {
					service = service.AddDate(0, 0, -1)
				}
				date := service.Format("2006-01-02")
				if date < options.From {
					continue
				}
				end, ok := schedule.MegaramaEnd(start, m.RuntimeMinutes, int(s.FirstPartDuration))
				if !ok {
					return result, summary, fmt.Errorf("%w: session end", errShape)
				}
				language, format, err := attributes(s)
				if err != nil {
					return result, summary, err
				}
				booking := s.BookingURL
				if booking == "" {
					booking = c.Website
				}
				if !schedule.ValidMegaramaBookingURL(booking, c.ID, s.ID) {
					return result, summary, fmt.Errorf("%w: session booking URL", errShape)
				}
				dates[date] = true
				if actual := start.Format("2006-01-02"); actual > dataset.Window.Through {
					dataset.Window.Through = actual
				}
				dataset.Showtimes = append(dataset.Showtimes, schedule.ShowtimeRecord{Provider: schedule.ProviderMegarama, ID: "megarama-showing-" + s.ID, ProviderShowingID: s.ID, TheaterID: theaterID, ServiceDate: date, Movie: m, StartTime: start, EndTime: end, FirstPartDurationMinutes: int(s.FirstPartDuration), Language: language, ProviderVersion: string(language), Format: format, Room: clean(s.HallName), BookingURL: booking})
			}
		}
		available := make([]string, 0, len(dates))
		for date := range dates {
			available = append(available, date)
		}
		sort.Strings(available)
		dataset.Theaters = append(dataset.Theaters, schedule.TheaterRecord{Provider: schedule.ProviderMegarama, ID: theaterID, ProviderID: c.ID, Slug: theaterID, Name: fallback(p.Name, c.Name), Address: fallback(p.Address, c.Address.Street), City: fallback(p.City, c.Address.City), PostalCode: fallback(p.ZipCode, c.Address.ZipCode), AvailableDates: available, AcceptedPasses: []string{}})
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
		return result, summary, fmt.Errorf("%w: invalid synchronized Megarama dataset", schedule.ErrDatasetValidation)
	}
	summary.Showtimes = len(dataset.Showtimes)
	return dataset, summary, nil
}

func movieID(cinemaID, id string) string {
	if globalID.MatchString(id) {
		return id
	}
	return cinemaID + "-" + id
}

func optionalPosterFailure(err error) bool {
	var requestErr *RequestError
	if !errors.As(err, &requestErr) {
		return false
	}
	return requestErr.Kind == syncproxy.FailureTransport || requestErr.Kind == syncproxy.FailureResponseRead || requestErr.Kind == syncproxy.FailureServer || requestErr.Kind == syncproxy.FailureStatus && requestErr.StatusCode == 404
}
