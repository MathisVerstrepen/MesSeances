package mk2

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type Fetcher interface {
	FetchCinemas(context.Context) ([]byte, error)
	FetchFilms(context.Context) ([]byte, error)
	FetchComplex(context.Context, string) ([]byte, error)
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
		return fail(payloadError(OperationCinemas))
	}
	if err := ctx.Err(); err != nil {
		return fail(typedError(err, OperationCinemas))
	}
	body, err := fetcher.FetchCinemas(ctx)
	if err != nil {
		return fail(typedError(err, OperationCinemas))
	}
	rows, err := parseCatalog[cinema](body, OperationCinemas)
	if err != nil || len(rows) == 0 {
		return fail(payloadError(OperationCinemas))
	}
	catalog := map[string]cinema{}
	slugSet := map[string]bool{}
	for _, row := range rows {
		c, err := normalizeCinema(row)
		if err != nil {
			return fail(err)
		}
		if prior, ok := catalog[c.ID]; ok && prior != c {
			return fail(payloadError(OperationCinemas))
		}
		catalog[c.ID], slugSet[c.ComplexSlug] = c, true
	}
	body, err = fetcher.FetchFilms(ctx)
	if err != nil {
		return fail(typedError(err, OperationFilms))
	}
	films, err := parseCatalog[film](body, OperationFilms)
	if err != nil {
		return fail(err)
	}
	movies := map[string]schedule.MovieRecord{}
	for _, f := range films {
		m, err := parseMovie(f)
		if err != nil {
			return fail(err)
		}
		m, err = mergeMovie(movies[m.ProviderID], m)
		if err != nil {
			return fail(err)
		}
		movies[m.ProviderID] = m
	}
	slugs := make([]string, 0, len(slugSet))
	for slug := range slugSet {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	pages, err := fetchComplexes(ctx, fetcher, slugs)
	if err != nil {
		return fail(err)
	}
	data, movieCount, err := normalize(ctx, catalog, movies, pages, options)
	if err != nil {
		return fail(err)
	}
	requests := 2 + len(slugs)
	if counter, ok := fetcher.(interface{ RequestCount() int }); ok {
		requests = counter.RequestCount()
	}
	return data, SyncSummary{Cinemas: len(data.Theaters), Movies: movieCount, Showtimes: len(data.Showtimes), Requests: requests, GeneratedAt: data.GeneratedAt}, nil
}

func fetchComplexes(ctx context.Context, fetcher Fetcher, slugs []string) ([]complex, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	pages := make([]complex, len(slugs))
	jobs := make(chan int, len(slugs))
	for i := range slugs {
		jobs <- i
	}
	close(jobs)
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	for range 2 {
		wg.Go(func() {
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				body, err := fetcher.FetchComplex(ctx, slugs[i])
				if err == nil {
					pages[i], err = parseComplex(body, slugs[i])
				}
				if err != nil {
					once.Do(func() { firstErr = typedError(err, OperationComplex); cancel() })
					return
				}
			}
		})
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, typedError(err, OperationComplex)
	}
	return pages, nil
}

func normalize(ctx context.Context, catalog map[string]cinema, movies map[string]schedule.MovieRecord, pages []complex, options SyncOptions) (schedule.Dataset, int, error) {
	fail := func() (schedule.Dataset, int, error) { return schedule.Dataset{}, 0, payloadError(OperationComplex) }
	data := schedule.Dataset{Provider: schedule.ProviderMK2, SchemaVersion: schedule.SchemaVersion, Scope: schedule.ScopeAll, Timezone: schedule.Timezone, GeneratedAt: options.Now.UTC(), Window: schedule.Window{From: options.From, Through: options.From}}
	theaters := map[string]schedule.TheaterRecord{}
	embeddedMovies := map[string]schedule.MovieRecord{}
	// Join complete cinema identities and all movie metadata before emitting sessions.
	for _, p := range pages {
		for _, row := range p.Cinemas {
			c, err := normalizeCinema(row)
			if err != nil || c != catalog[c.ID] || c.ComplexSlug != p.Slug {
				return fail()
			}
			if _, duplicate := theaters[c.ID]; duplicate {
				return fail()
			}
			id := "mk2-" + c.ID
			theaters[c.ID] = schedule.TheaterRecord{Provider: schedule.ProviderMK2, ProviderID: c.ID, ID: id, Slug: id, Name: c.Name, Address: c.Address, City: c.City, PostalCode: strings.TrimSpace(p.Zipcode), AvailableDates: []string{}, AcceptedPasses: []string{}}
		}
		for _, typ := range p.Types {
			for _, g := range typ.Groups {
				c, err := normalizeCinema(g.Cinema)
				if err != nil || c != catalog[c.ID] || c.ComplexSlug != p.Slug {
					return fail()
				}
				m, err := parseMovie(g.Film)
				if err != nil || movies[m.ProviderID].ProviderID == "" && embeddedMovies[m.ProviderID].ProviderID == "" && m.Title == "" {
					return fail()
				}
				// Validate embedded duplicates independently of the catalog so a
				// canonical metadata cannot hide conflicts within the same source.
				m, err = mergeMovie(embeddedMovies[m.ProviderID], m)
				if err != nil {
					return fail()
				}
				embeddedMovies[m.ProviderID] = m
			}
		}
	}
	if len(theaters) != len(catalog) {
		return fail()
	}
	for id, embedded := range embeddedMovies {
		canonical := movies[id]
		// Complex pages can use event labels and different runtimes for a
		// catalog film ID. Prefer nonempty catalog values only at this join.
		if canonical.Title != "" {
			embedded.Title = canonical.Title
		}
		if canonical.RuntimeMinutes != 0 {
			embedded.RuntimeMinutes = canonical.RuntimeMinutes
		}
		merged, err := mergeMovie(canonical, embedded)
		if err != nil {
			return fail()
		}
		movies[id] = merged
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return fail()
	}
	seen := map[string]schedule.ShowtimeRecord{}
	usedMovies := map[string]bool{}
	dates := map[string]map[string]bool{}
	for _, p := range pages {
		for _, typ := range p.Types {
			for _, g := range typ.Groups {
				for _, s := range g.Sessions {
					if err := ctx.Err(); err != nil {
						return schedule.Dataset{}, 0, typedError(err, OperationComplex)
					}
					if s.CinemaID != g.Cinema.ID || s.FilmID != g.Film.ID || s.ID != s.CinemaID+"-"+s.SessionID || s.ScheduledFilmID != s.CinemaID+"-"+s.FilmID || len(s.ScheduledFilmID) > 128 || !schedule.ValidMK2Identity("showing", s.ID) {
						return fail()
					}
					start, err := time.Parse(time.RFC3339, s.ShowTime)
					if err != nil {
						return fail()
					}
					start = start.In(location)
					date := start.Format(time.DateOnly)
					language, version, format, err := parseAttributes(s.Attributes)
					if err != nil {
						return fail()
					}
					r := schedule.ShowtimeRecord{Provider: schedule.ProviderMK2, ID: "mk2-showing-" + s.ID, ProviderShowingID: s.ID, TheaterID: "mk2-" + s.CinemaID, ServiceDate: date, Movie: movies[s.FilmID], StartTime: start, EndTime: start, Language: language, ProviderVersion: version, Format: format, BookingURL: schedule.MK2BookingPrefix + s.CinemaID + "&sessionId=" + s.SessionID}
					if prior, ok := seen[s.ID]; ok {
						if !reflect.DeepEqual(prior, r) {
							return fail()
						}
						continue
					}
					seen[s.ID] = r
					if date < options.From {
						continue
					}
					data.Showtimes = append(data.Showtimes, r)
					if dates[s.CinemaID] == nil {
						dates[s.CinemaID] = map[string]bool{}
					}
					dates[s.CinemaID][date], usedMovies[s.FilmID] = true, true
					if date > data.Window.Through {
						data.Window.Through = date
					}
				}
			}
		}
	}
	for id, t := range theaters {
		for date := range dates[id] {
			t.AvailableDates = append(t.AvailableDates, date)
		}
		sort.Strings(t.AvailableDates)
		data.Theaters = append(data.Theaters, t)
	}
	sort.Slice(data.Theaters, func(i, j int) bool { return data.Theaters[i].ID < data.Theaters[j].ID })
	sort.Slice(data.Showtimes, func(i, j int) bool { return data.Showtimes[i].ID < data.Showtimes[j].ID })
	if schedule.ValidateDataset(data, true) != nil {
		return fail()
	}
	return data, len(usedMovies), nil
}
func payloadError(op Operation) error {
	return &RequestError{Operation: op, Kind: syncproxy.FailureInvalidJSON, cause: schedule.ErrDatasetValidation}
}
func typedError(err error, op Operation) error {
	safe := &RequestError{Operation: op, Kind: syncproxy.FailureTransport}
	var re *RequestError
	if errors.As(err, &re) {
		safe.Kind, safe.StatusCode = re.Kind, re.StatusCode
	}
	switch {
	case errors.Is(err, context.Canceled):
		safe.Kind, safe.cause = syncproxy.FailureCanceled, context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		safe.Kind, safe.cause = syncproxy.FailureCanceled, context.DeadlineExceeded
	case errors.Is(err, schedule.ErrDatasetValidation):
		safe.cause = schedule.ErrDatasetValidation
	}
	return safe
}
