package cinemovida

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
	Get(context.Context, Operation, string) ([]byte, error)
	Checkout(context.Context, string, time.Time) ([]byte, error)
	RequestCount() int
}
type SyncOptions struct {
	From string
	Now  time.Time
}
type SyncSummary struct {
	Cinemas, Movies, Jobs, Requests, Showtimes                      int
	EndsAttempted, EndsRecovered, EndsUnresolved, EndsBudgetSkipped int
	GeneratedAt                                                     time.Time
}

var errSnapshotChanged = errors.New("cinemovida snapshot changed")

func Sync(ctx context.Context, g Getter, o SyncOptions) (data schedule.Dataset, summary SyncSummary, err error) {
	for attempt := 0; attempt < 2; attempt++ {
		data, summary, err = syncSnapshot(ctx, g, o)
		if !errors.Is(err, errSnapshotChanged) {
			break
		}
	}
	if err == nil {
		err = enrichEnds(ctx, g, data.Showtimes, &summary, 20*time.Minute, 4096)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && schedule.ValidateDataset(data, true) != nil {
		err = fmt.Errorf("%w: invalid Ciné-Movida dataset", schedule.ErrDatasetValidation)
	}
	summary.Requests = g.RequestCount()
	if err != nil {
		return schedule.Dataset{}, summary, err
	}
	return data, summary, nil
}

func syncSnapshot(ctx context.Context, g Getter, o SyncOptions) (schedule.Dataset, SyncSummary, error) {
	summary := SyncSummary{GeneratedAt: o.Now.UTC()}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return schedule.Dataset{}, summary, fmt.Errorf("load schedule timezone")
	}
	from, err := time.ParseInLocation(time.DateOnly, o.From, location)
	if err != nil || from.Format(time.DateOnly) != o.From || o.Now.IsZero() {
		return schedule.Dataset{}, summary, fmt.Errorf("invalid sync window")
	}
	// Never advertise stale source days, even for an explicitly older request.
	if today := o.Now.In(location).Format(time.DateOnly); o.From < today {
		o.From = today
	}
	from, _ = time.ParseInLocation(time.DateOnly, o.From, location)
	sourceFrom := from.AddDate(0, 0, -1).Format(time.DateOnly)
	body, err := g.Get(ctx, OperationCinemas, CinemasURL)
	if err != nil {
		return schedule.Dataset{}, summary, err
	}
	items, err := parseCinemas(body)
	if err != nil {
		return schedule.Dataset{}, summary, err
	}
	summary.Cinemas = len(items)
	programs, err := parallel.MapOrdered(ctx, items, parallel.Options{Workers: min(WorkerCount, len(items))}, func(ctx context.Context, c cinema) (map[string][]string, error) {
		body, err := g.Get(ctx, OperationProgram, programURL(c.ID))
		if err != nil {
			return nil, err
		}
		return parseProgram(body, sourceFrom, location)
	})
	if err != nil {
		return schedule.Dataset{}, summary, err
	}
	set := map[string]bool{}
	for _, p := range programs {
		for id, dates := range p {
			if len(dates) > 0 {
				set[id] = true
			}
		}
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	batches := batchMovieIDs(ids)
	groups, err := parallel.MapOrdered(ctx, batches, parallel.Options{Workers: min(WorkerCount, len(batches))}, func(ctx context.Context, ids []string) (map[string]schedule.MovieRecord, error) {
		body, err := g.Get(ctx, OperationMovies, moviesURL(ids))
		if err != nil {
			return nil, err
		}
		movies, err := parseMovies(body)
		if err != nil {
			return nil, err
		}
		if len(movies) != len(ids) {
			return nil, errSnapshotChanged
		}
		for _, id := range ids {
			if movies[id].ProviderID == "" {
				return nil, errSnapshotChanged
			}
		}
		return movies, nil
	})
	if err != nil {
		return schedule.Dataset{}, summary, err
	}
	movies := map[string]schedule.MovieRecord{}
	for _, group := range groups {
		for id, m := range group {
			movies[id] = m
		}
	}
	summary.Movies = len(movies)
	data := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderCinemovida, Scope: schedule.ScopeAll, Timezone: schedule.Timezone, GeneratedAt: o.Now.UTC(), Window: schedule.Window{From: o.From, Through: o.From}, Theaters: []schedule.TheaterRecord{}, Showtimes: []schedule.ShowtimeRecord{}}
	type job struct {
		cinema  cinema
		program map[string][]string
		through string
	}
	jobs := []job{}
	for i, c := range items {
		dates, through := programDates(calendarProgram(programs[i], o.From))
		l := c.PracticalInfo.Location
		id := "cinemovida-" + c.ID
		data.Theaters = append(data.Theaters, schedule.TheaterRecord{Provider: schedule.ProviderCinemovida, ProviderID: c.ID, ID: id, Slug: id, Name: c.Name, Address: l.Address, City: l.City, PostalCode: l.Zip, AvailableDates: dates, AcceptedPasses: []string{}})
		// A stale source bucket can still contain a current calendar-day
		// midnight session. Fetch it even when no future bucket is advertised.
		_, sourceThrough := programDates(programs[i])
		if sourceThrough != "" {
			through = max(through, sourceThrough)
			jobs = append(jobs, job{c, programs[i], through})
			if through > data.Window.Through {
				data.Window.Through = through
			}
		}
	}
	summary.Jobs = len(jobs)
	allowMissing := ""
	if o.Now.In(location).Format(time.DateOnly) == o.From {
		allowMissing = o.From
	}
	type result struct {
		records  []schedule.ShowtimeRecord
		coverage []schedule.PublicationCoverage
	}
	results, err := parallel.MapOrdered(ctx, jobs, parallel.Options{Workers: min(WorkerCount, len(jobs))}, func(ctx context.Context, j job) (result, error) {
		f := scheduleFetcher{getter: g, cinema: j.cinema, movies: movies, location: location, calendarFrom: o.From, allowMissing: allowMissing, unknown: map[string]bool{}, cache: map[string][]schedule.ShowtimeRecord{}}
		records, err := f.fetch(ctx, j.program, sourceFrom, j.through)
		return result{records, calendarCoverage("cinemovida-"+j.cinema.ID, j.program, records, o.From, f.unknown)}, err
	})
	if err != nil {
		return schedule.Dataset{}, summary, err
	}
	for _, r := range results {
		data.Showtimes = append(data.Showtimes, r.records...)
		data.Coverage = append(data.Coverage, r.coverage...)
	}
	for i := range data.Theaters {
		theater := &data.Theaters[i]
		dates := map[string]bool{}
		for _, date := range theater.AvailableDates {
			dates[date] = true
		}
		for _, record := range data.Showtimes {
			if record.TheaterID == theater.ID {
				dates[record.ServiceDate] = true
				data.Window.Through = max(data.Window.Through, record.ServiceDate)
			}
		}
		theater.AvailableDates = theater.AvailableDates[:0]
		for date := range dates {
			theater.AvailableDates = append(theater.AvailableDates, date)
		}
		sort.Strings(theater.AvailableDates)
	}
	sort.Slice(data.Showtimes, func(i, j int) bool {
		a, b := data.Showtimes[i], data.Showtimes[j]
		if a.TheaterID != b.TheaterID {
			return a.TheaterID < b.TheaterID
		}
		if !a.StartTime.Equal(b.StartTime) {
			return a.StartTime.Before(b.StartTime)
		}
		return a.ID < b.ID
	})
	summary.Showtimes = len(data.Showtimes)
	if schedule.ValidateDataset(data, true) != nil {
		return schedule.Dataset{}, summary, fmt.Errorf("%w: invalid Ciné-Movida snapshot", schedule.ErrDatasetValidation)
	}
	return data, summary, nil
}

func batchMovieIDs(ids []string) [][]string {
	result := [][]string{}
	for start := 0; start < len(ids); {
		end := start + 1
		for end < len(ids) && end-start < MovieBatchSize && len(moviesURL(ids[start:end+1])) <= MaxRequestURLBytes {
			end++
		}
		result = append(result, append([]string(nil), ids[start:end]...))
		start = end
	}
	return result
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

func calendarProgram(program map[string][]string, from string) map[string][]string {
	result := map[string][]string{}
	for id, dates := range program {
		for _, date := range dates {
			if date >= from {
				result[id] = append(result[id], date)
			}
		}
	}
	return result
}

func calendarCoverage(theater string, program map[string][]string, records []schedule.ShowtimeRecord, from string, unknown map[string]bool) []schedule.PublicationCoverage {
	// A missing source bucket also leaves its possible next-day starts unproven.
	calendarUnknown := map[string]bool{}
	for date := range unknown {
		calendarUnknown[date] = true
		day, _ := time.Parse(time.DateOnly, date)
		calendarUnknown[day.AddDate(0, 0, 1).Format(time.DateOnly)] = true
	}
	coverage := schedule.ProgramCoverage(theater, calendarProgram(program, from), calendarUnknown)
	seen := map[string]bool{}
	for _, c := range coverage {
		seen[c.ServiceDate] = true
	}
	for _, record := range records {
		if !seen[record.ServiceDate] {
			seen[record.ServiceDate] = true
			coverage = append(coverage, schedule.PublicationCoverage{TheaterID: theater, ServiceDate: record.ServiceDate, Status: schedule.CoverageUnknown, Basis: schedule.CoverageUnproven})
		}
	}
	sort.Slice(coverage, func(i, j int) bool { return coverage[i].ServiceDate < coverage[j].ServiceDate })
	return coverage
}

type scheduleFetcher struct {
	getter       Getter
	cinema       cinema
	movies       map[string]schedule.MovieRecord
	location     *time.Location
	calendarFrom string
	allowMissing string
	unknown      map[string]bool
	cache        map[string][]schedule.ShowtimeRecord
}

func (f *scheduleFetcher) window(ctx context.Context, p map[string][]string, from, through string) ([]schedule.ShowtimeRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	filtered := map[string][]string{}
	for id, dates := range p {
		for _, d := range dates {
			if d >= from && d <= through {
				filtered[id] = append(filtered[id], d)
			}
		}
	}
	if len(filtered) == 0 {
		return []schedule.ShowtimeRecord{}, nil
	}
	key := from + "/" + through
	if prior, ok := f.cache[key]; ok {
		return prior, nil
	}
	body, err := f.getter.Get(ctx, OperationSchedule, scheduleURL(f.cinema.ID, from, through))
	if err != nil {
		return nil, err
	}
	records, err := parseSchedule(body, f.cinema, p, f.movies, f.location, scheduleRange{from, through, f.calendarFrom, f.allowMissing}, f.unknown)
	if err == nil {
		f.cache[key] = records
	}
	return records, err
}
func (f *scheduleFetcher) fetch(ctx context.Context, p map[string][]string, from, through string) ([]schedule.ShowtimeRecord, error) {
	records, err := f.window(ctx, p, from, through)
	var r *RequestError
	if err == nil || from == through || !errors.As(err, &r) || r.Kind != syncproxy.FailureServer {
		return records, err
	}
	start, _ := time.Parse(time.DateOnly, from)
	end, _ := time.Parse(time.DateOnly, through)
	middle := start.AddDate(0, 0, int(end.Sub(start).Hours()/24)/2)
	left, err := f.fetch(ctx, p, from, middle.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	right, err := f.fetch(ctx, p, middle.AddDate(0, 0, 1).Format(time.DateOnly), through)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}
