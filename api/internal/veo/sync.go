package veo

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	"messeances/api/internal/parallel"
	"messeances/api/internal/schedule"
)

type Getter interface {
	Get(context.Context, Operation, string) ([]byte, error)
	Checkout(context.Context, string, time.Time) (CheckoutResult, error)
	RequestCount() int
}
type SyncOptions struct {
	From string
	// Now is captured once by the caller. It also fixes the checkout cutoff for
	// the entire run, regardless of time spent acquiring catalogs and pages.
	Now time.Time
}
type SyncSummary struct {
	Cinemas, Movies, Jobs, Requests, Showtimes, Checkouts, ExactEnds int
	GeneratedAt                                                      time.Time
	// Provider-private observability; never copied into public sync models.
	CheckoutRetryAttempts, CheckoutRecoveries int
	FirstCheckoutRetry                        *CheckoutParseError
	FirstCheckoutRetryRecovered               bool
	ToulouseUnavailableCheckouts              int
	GueretUnavailableCheckouts                int
	RegularFinal404UnavailableCheckouts       int
	CheckoutTransientFailures                 int
	CheckoutBudgetSkipped                     int
}

func Sync(ctx context.Context, g Getter, o SyncOptions) (schedule.Dataset, SyncSummary, error) {
	s := SyncSummary{GeneratedAt: o.Now.UTC()}
	d, err := acquire(ctx, g, o, &s)
	if err == nil {
		err = enrichCheckouts(ctx, g, d.Showtimes, &s, o.Now)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && schedule.ValidateDataset(d, true) != nil {
		err = fmt.Errorf("%w: invalid Véo snapshot", schedule.ErrDatasetValidation)
	}
	s.Requests = g.RequestCount()
	if err != nil {
		return schedule.Dataset{}, s, err
	}
	s.Cinemas = len(d.Theaters)
	s.Showtimes = len(d.Showtimes)
	return d, s, nil
}
func acquire(ctx context.Context, g Getter, o SyncOptions, s *SyncSummary) (schedule.Dataset, error) {
	loc, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return schedule.Dataset{}, errShape
	}
	from, err := time.ParseInLocation(time.DateOnly, o.From, loc)
	if err != nil || from.Format(time.DateOnly) != o.From || o.Now.IsZero() {
		return schedule.Dataset{}, fmt.Errorf("invalid Véo sync window")
	}
	d := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderVeo, Scope: schedule.ScopeAll, Timezone: schedule.Timezone, GeneratedAt: o.Now.UTC(), Window: schedule.Window{From: o.From, Through: o.From}, Theaters: []schedule.TheaterRecord{}, Showtimes: []schedule.ShowtimeRecord{}}
	seenTheaters := map[string]bool{}
	seenMovies := map[string]schedule.MovieRecord{}
	allowMissing := ""
	if serviceDate(o.Now.In(loc)) == o.From {
		allowMissing = o.From
	}
	for _, origin := range []string{MainOrigin, SeteOrigin} {
		b, err := g.Get(ctx, OperationCinemas, origin+CatalogPath)
		if err != nil {
			return schedule.Dataset{}, err
		}
		cinemas, err := parseCinemas(b, origin)
		if err != nil {
			return schedule.Dataset{}, err
		}
		programs, err := parallel.MapOrdered(ctx, cinemas, parallel.Options{Workers: min(WorkerCount, len(cinemas))}, func(ctx context.Context, c cinema) (map[string][]string, error) {
			b, err := g.Get(ctx, OperationProgram, programURL(c.origin, c.sourceID()))
			if err != nil {
				return nil, err
			}
			return parseProgram(b, o.From, loc)
		})
		if err != nil {
			return schedule.Dataset{}, err
		}
		movies := map[string]schedule.MovieRecord{}
		movieOrigins := []string{origin}
		if origin == MainOrigin {
			movieOrigins = append(movieOrigins, GueretOrigin)
		}
		for _, movieOrigin := range movieOrigins {
			idsSet := map[string]bool{}
			for i, p := range programs {
				if cinemas[i].origin != movieOrigin {
					continue
				}
				for id, dates := range p {
					if len(dates) > 0 {
						idsSet[id] = true
					}
				}
			}
			ids := make([]string, 0, len(idsSet))
			for id := range idsSet {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, batch := range batchMovieIDs(movieOrigin, ids) {
				b, err := g.Get(ctx, OperationMovies, moviesURL(movieOrigin, batch))
				if err != nil {
					return schedule.Dataset{}, err
				}
				items, err := parseMovies(b)
				if err != nil {
					return schedule.Dataset{}, err
				}
				if len(items) != len(batch) {
					return schedule.Dataset{}, errSnapshotChanged
				}
				for _, id := range batch {
					m, ok := items[id]
					if !ok {
						return schedule.Dataset{}, errSnapshotChanged
					}
					if prior, ok := seenMovies[id]; ok && !reflect.DeepEqual(prior, m) {
						return schedule.Dataset{}, errSnapshotChanged
					}
					seenMovies[id] = m
					movies[id] = m
				}
			}
		}
		type result struct {
			rows     []schedule.ShowtimeRecord
			coverage []schedule.PublicationCoverage
		}
		results, err := parallel.MapOrdered(ctx, cinemas, parallel.Options{Workers: min(WorkerCount, len(cinemas))}, func(ctx context.Context, c cinema) (result, error) {
			index := sort.Search(len(cinemas), func(i int) bool { return cinemas[i].ID >= c.ID })
			p := programs[index]
			_, through := programDates(p)
			if through == "" {
				// An internally consistent empty advertised index supplies no
				// horizon or completeness evidence. Retain its cinema, as with
				// Grand Ecran, without inventing schedule dates. Contradictory
				// index/schedule evidence is rejected by the parsers, never used
				// to synthesize an advertised horizon.
				return result{rows: []schedule.ShowtimeRecord{}}, nil
			}
			b, err := g.Get(ctx, OperationSchedule, scheduleURL(c.origin, c.sourceID(), o.From, through))
			if err != nil {
				return result{}, err
			}
			rows, unknown, err := parseSchedule(b, c, p, movies, loc, allowMissing, o.From, through)
			return result{rows, schedule.ProgramCoverage("veo-"+c.ID, p, unknown)}, err
		})
		if err != nil {
			return schedule.Dataset{}, err
		}
		for i, c := range cinemas {
			if seenTheaters[c.ID] {
				return schedule.Dataset{}, errShape
			}
			seenTheaters[c.ID] = true
			dates, through := programDates(programs[i])
			l := c.PracticalInfo.Location
			id := "veo-" + c.ID
			d.Theaters = append(d.Theaters, schedule.TheaterRecord{Provider: schedule.ProviderVeo, ProviderID: c.ID, ID: id, Slug: id, Name: c.Name, Address: l.Address, City: l.City, PostalCode: l.Zip, AvailableDates: dates, AcceptedPasses: []string{}})
			if through > d.Window.Through {
				d.Window.Through = through
			}
			d.Showtimes = append(d.Showtimes, results[i].rows...)
			d.Coverage = append(d.Coverage, results[i].coverage...)
			s.Jobs++
		}
	}
	b, err := g.Get(ctx, OperationCastelia, ProgramURL)
	if err != nil {
		return schedule.Dataset{}, err
	}
	t, rows, err := parseCastelia(b, o.From, loc)
	if err != nil {
		return schedule.Dataset{}, err
	}
	d.Theaters = append(d.Theaters, t)
	d.Showtimes = append(d.Showtimes, rows...)
	for _, date := range t.AvailableDates {
		if date > d.Window.Through {
			d.Window.Through = date
		}
		d.Coverage = append(d.Coverage, schedule.PublicationCoverage{TheaterID: t.ID, ServiceDate: date, Status: schedule.CoverageUnknown, Basis: schedule.CoverageUnproven})
	}
	for _, r := range rows {
		seenMovies[r.Movie.ProviderID] = r.Movie
	}
	s.Movies = len(seenMovies)
	sort.Slice(d.Showtimes, func(i, j int) bool {
		a, b := d.Showtimes[i], d.Showtimes[j]
		if a.TheaterID != b.TheaterID {
			return a.TheaterID < b.TheaterID
		}
		if !a.StartTime.Equal(b.StartTime) {
			return a.StartTime.Before(b.StartTime)
		}
		return a.ID < b.ID
	})
	return d, nil
}
func programDates(p map[string][]string) ([]string, string) {
	set := map[string]bool{}
	for _, dates := range p {
		for _, d := range dates {
			set[d] = true
		}
	}
	dates := make([]string, 0, len(set))
	for d := range set {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	through := ""
	if len(dates) > 0 {
		through = dates[len(dates)-1]
	}
	return dates, through
}
func batchMovieIDs(origin string, ids []string) [][]string {
	batches := [][]string{}
	for start := 0; start < len(ids); {
		end := start + 1
		for end < len(ids) && end-start < MovieBatchSize && len(moviesURL(origin, ids[start:end+1])) <= MaxRequestURLBytes {
			end++
		}
		batches = append(batches, append([]string(nil), ids[start:end]...))
		start = end
	}
	return batches
}
