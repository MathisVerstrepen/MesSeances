package megarama

import (
	"context"
	"errors"
	"fmt"
	"time"

	"messeances/api/internal/parallel"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type Getter interface {
	Config(context.Context) ([]byte, error)
	SiteConfig(context.Context, string) ([]byte, error)
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
	location, err := schedule.SyncLocation(options.From)
	if err != nil {
		return result, summary, err
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
	// Recover all missing roots before fetching programs. Invalid populated roots
	// already failed parseConfig and must never be replaced by a fallback.
	for i := range cinemas {
		c := &cinemas[i]
		if c.Website != "" {
			continue
		}
		body, err := getter.SiteConfig(ctx, c.ID)
		if err != nil {
			return result, summary, err
		}
		c.Website, err = parseSiteWebsite(body, *c)
		if err != nil {
			return result, summary, err
		}
	}
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
	builder := schedule.NewDatasetBuilder(schedule.ProviderMegarama, options.From, options.Now, schedule.BuilderOptions{})
	for i, p := range programs {
		for _, e := range p.Events {
			id := movieID(cinemas[i].ID, e.ID)
			if !builder.MergeMovie(schedule.MovieRecord{Provider: schedule.ProviderMegarama, ProviderID: id, Slug: "megarama-film-" + id, Title: e.Title, RuntimeMinutes: int(e.Duration), PosterURL: posterURL(e.BillURL, e.ID)}) {
				return result, summary, fmt.Errorf("%w: conflicting Megarama film metadata", schedule.ErrDatasetValidation)
			}
		}
	}
	for _, id := range builder.MovieIDs() {
		m := builder.Movie(id)
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
		builder.UpdateMovie(m)
	}
	summary.Movies = builder.MovieCount()
	seen := map[string]bool{}
	for i, p := range programs {
		if err := ctx.Err(); err != nil {
			return result, summary, err
		}
		c := cinemas[i]
		theaterID := "megarama-" + c.ID
		for _, e := range p.Events {
			m := builder.Movie(movieID(c.ID, e.ID))
			for _, s := range e.Sessions {
				// Duplicates are rejected before date filtering, past sessions included.
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
				builder.ExtendWindow(start.Format("2006-01-02"))
				version := string(language)
				if clean(s.Version) == "VOF" || clean(s.Version) == "VOST" {
					version = clean(s.Version)
				}
				if err := builder.AddShowtime(schedule.ShowtimeRecord{Provider: schedule.ProviderMegarama, ID: "megarama-showing-" + s.ID, ProviderShowingID: s.ID, TheaterID: theaterID, ServiceDate: date, Movie: m, StartTime: start, EndTime: end, FirstPartDurationMinutes: int(s.FirstPartDuration), Language: language, ProviderVersion: version, Format: format, Room: clean(s.HallName), BookingURL: booking}); err != nil {
					return result, summary, fmt.Errorf("%w: duplicate Megarama showing", schedule.ErrDatasetValidation)
				}
			}
		}
		builder.AddTheater(schedule.TheaterRecord{Provider: schedule.ProviderMegarama, ID: theaterID, ProviderID: c.ID, Slug: theaterID, Name: fallback(p.Name, c.Name), Address: fallback(p.Address, c.Address.Street), City: fallback(p.City, c.Address.City), PostalCode: fallback(p.ZipCode, c.Address.ZipCode), AcceptedPasses: []string{}})
	}
	dataset, err := builder.Build("Megarama")
	if err != nil {
		return result, summary, err
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
	var requestErr *syncproxy.RequestError
	if !errors.As(err, &requestErr) {
		return false
	}
	return requestErr.Kind == syncproxy.FailureTransport || requestErr.Kind == syncproxy.FailureResponseRead || requestErr.Kind == syncproxy.FailureServer || requestErr.Kind == syncproxy.FailureStatus && requestErr.StatusCode == 404
}
