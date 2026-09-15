package cineville

import (
	"context"
	"errors"
	"sort"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type Fetcher interface {
	Fetch(context.Context) ([]byte, error)
	FetchCinema(context.Context, string, string) ([]byte, error)
}
type SyncOptions struct {
	From string
	Now  time.Time
}
type SyncSummary struct {
	Cinemas, Movies, Showtimes, Requests int
	GeneratedAt                          time.Time
}

func Sync(ctx context.Context, fetcher Fetcher, options SyncOptions) (schedule.Dataset, SyncSummary, error) {
	fail := func(err error) (schedule.Dataset, SyncSummary, error) { return schedule.Dataset{}, SyncSummary{}, err }
	from, err := time.Parse(time.DateOnly, options.From)
	if err != nil || from.Format(time.DateOnly) != options.From || options.Now.IsZero() {
		return fail(schedule.ErrDatasetValidation)
	}
	build, catalog, err := bootstrap(ctx, fetcher)
	if err != nil {
		return fail(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		pages := make([]pageProps, 0, len(catalog))
		restart := false
		for _, c := range catalog {
			if err := ctx.Err(); err != nil {
				return fail(typedError(err, OperationCinema))
			}
			body, err := fetcher.FetchCinema(ctx, build, c.Route)
			if err != nil {
				var re *RequestError
				if attempt == 0 && errors.As(err, &re) && re.Kind == syncproxy.FailureStatus && re.StatusCode == 404 {
					freshBuild, freshCatalog, refreshErr := bootstrap(ctx, fetcher)
					if refreshErr != nil {
						return fail(refreshErr)
					}
					if freshBuild == build {
						return fail(typedError(err, OperationCinema))
					}
					build, catalog, restart = freshBuild, freshCatalog, true
					break
				}
				return fail(typedError(err, OperationCinema))
			}
			p, err := parsePage(body, c)
			if err != nil {
				return fail(payloadError(OperationCinema))
			}
			pages = append(pages, p)
		}
		if restart {
			continue
		}
		data, movieCount, err := normalize(ctx, catalog, pages, options)
		if err != nil {
			return fail(err)
		}
		summary := SyncSummary{Cinemas: len(data.Theaters), Movies: movieCount, Showtimes: len(data.Showtimes), GeneratedAt: data.GeneratedAt}
		if counter, ok := fetcher.(interface{ RequestCount() int }); ok {
			summary.Requests = counter.RequestCount()
		}
		return data, summary, nil
	}
	return fail(payloadError(OperationCinema))
}
func bootstrap(ctx context.Context, fetcher Fetcher) (string, []cinema, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, typedError(err, OperationBootstrap)
	}
	body, err := fetcher.Fetch(ctx)
	if err != nil {
		return "", nil, typedError(err, OperationBootstrap)
	}
	build, catalog, err := parseBootstrap(body)
	if err != nil {
		return "", nil, payloadError(OperationBootstrap)
	}
	return build, catalog, nil
}
func payloadError(op Operation) error {
	return &RequestError{Operation: op, Kind: syncproxy.FailureInvalidJSON}
}
func typedError(err error, op Operation) error {
	var re *RequestError
	if errors.As(err, &re) {
		safe := &RequestError{Operation: op, Kind: re.Kind, StatusCode: re.StatusCode}
		if errors.Is(err, context.Canceled) {
			safe.cause = context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			safe.cause = context.DeadlineExceeded
		}
		return safe
	}
	if errors.Is(err, context.Canceled) {
		return &RequestError{Operation: op, Kind: syncproxy.FailureCanceled, cause: context.Canceled}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &RequestError{Operation: op, Kind: syncproxy.FailureCanceled, cause: context.DeadlineExceeded}
	}
	return &RequestError{Operation: op, Kind: syncproxy.FailureTransport}
}
func normalize(ctx context.Context, catalog []cinema, pages []pageProps, options SyncOptions) (schedule.Dataset, int, error) {
	data := schedule.Dataset{Provider: schedule.ProviderCineville, SchemaVersion: schedule.SchemaVersion, Scope: schedule.ScopeAll, Timezone: schedule.Timezone, GeneratedAt: options.Now.UTC(), Window: schedule.Window{From: options.From, Through: options.From}}
	movies := map[string]schedule.MovieRecord{}
	// Normalize all metadata first so missing optional fields cannot cause session conflicts.
	for _, p := range pages {
		for _, films := range [][]film{p.Program, p.Events} {
			for _, f := range films {
				if err := ctx.Err(); err != nil {
					return schedule.Dataset{}, 0, typedError(err, OperationCinema)
				}
				if f.Dates == nil {
					return schedule.Dataset{}, 0, payloadError(OperationCinema)
				}
				hasSessions := false
				for _, date := range f.Dates {
					if date.Showtimes == nil {
						return schedule.Dataset{}, 0, payloadError(OperationCinema)
					}
					hasSessions = hasSessions || len(date.Showtimes) > 0
				}
				if !hasSessions {
					continue
				}
				m, err := parseMovie(f)
				if err != nil {
					return schedule.Dataset{}, 0, payloadError(OperationCinema)
				}
				movies[m.ProviderID] = mergeMovie(movies[m.ProviderID], m)
			}
		}
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return schedule.Dataset{}, 0, payloadError(OperationCinema)
	}
	seen := map[string]schedule.ShowtimeRecord{}
	usedMovies := map[string]bool{}
	for i, c := range catalog {
		theaterID := "cineville-" + string(c.ID)
		dates := map[string]bool{}
		for _, films := range [][]film{pages[i].Program, pages[i].Events} {
			for _, f := range films {
				for _, d := range f.Dates {
					if d.Showtimes == nil {
						return schedule.Dataset{}, 0, payloadError(OperationCinema)
					}
					for _, s := range d.Showtimes {
						if err := ctx.Err(); err != nil {
							return schedule.Dataset{}, 0, typedError(err, OperationCinema)
						}
						if s.Cinema != c.ID || !schedule.ValidCinevilleIdentity("theater", string(s.ID)) || !schedule.ValidCinevilleIdentity("theater", string(s.Bordereau)) || !schedule.ValidCinevilleIdentity("theater", string(s.Room)) {
							return schedule.Dataset{}, 0, payloadError(OperationCinema)
						}
						start, err := parseStart(string(d.Date), s.Time, location)
						if err != nil {
							return schedule.Dataset{}, 0, payloadError(OperationCinema)
						}
						language, format, err := parseAttributes(s)
						if err != nil {
							return schedule.Dataset{}, 0, payloadError(OperationCinema)
						}
						date := start.Format(time.DateOnly)
						id := string(c.ID) + "-" + string(s.ID)
						r := schedule.ShowtimeRecord{Provider: schedule.ProviderCineville, ID: "cineville-showing-" + id, ProviderShowingID: id, TheaterID: theaterID, ServiceDate: date, Movie: movies[string(f.Visa)], StartTime: start, EndTime: start, Language: language, ProviderVersion: s.Version, Format: format, Room: string(s.Room), BookingURL: "https://www.cineville.fr/vad/" + string(c.ID) + "/" + string(s.ID) + "/" + string(s.Bordereau)}
						if prior, ok := seen[id]; ok {
							if prior.Movie.ProviderID != r.Movie.ProviderID || prior.ServiceDate != r.ServiceDate || !prior.StartTime.Equal(r.StartTime) || prior.Room != r.Room || prior.Language != r.Language || prior.ProviderVersion != r.ProviderVersion || prior.Format != r.Format || prior.BookingURL != r.BookingURL {
								return schedule.Dataset{}, 0, payloadError(OperationCinema)
							}
							continue
						}
						seen[id] = r
						if date < options.From {
							continue
						}
						data.Showtimes = append(data.Showtimes, r)
						dates[date] = true
						usedMovies[r.Movie.ProviderID] = true
						if date > data.Window.Through {
							data.Window.Through = date
						}
					}
				}
			}
		}
		available := make([]string, 0, len(dates))
		for date := range dates {
			available = append(available, date)
		}
		sort.Strings(available)
		data.Theaters = append(data.Theaters, schedule.TheaterRecord{Provider: schedule.ProviderCineville, ID: theaterID, ProviderID: string(c.ID), Slug: theaterID, Name: c.Name, Address: c.Address, City: c.City, PostalCode: c.Postal, AvailableDates: available, AcceptedPasses: []string{}})
	}
	if schedule.ValidateDataset(data, true) != nil {
		return schedule.Dataset{}, 0, schedule.ErrDatasetValidation
	}
	return data, len(usedMovies), nil
}
