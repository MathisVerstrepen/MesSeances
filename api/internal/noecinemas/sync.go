package noecinemas

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
	RequestCount() int
}
type SyncOptions struct {
	From string
	Now  time.Time
}
type SyncSummary struct {
	Cinemas, Movies, Jobs, Requests, Showtimes                          int
	RoomsAttempted, RoomsRecovered, RoomsUnresolved, RoomsBudgetSkipped int
	GeneratedAt                                                         time.Time
}

var errSnapshotChanged = errors.New("noecinemas snapshot changed")

func Sync(ctx context.Context, getter Getter, options SyncOptions) (data schedule.Dataset, summary SyncSummary, err error) {
	for attempt := 0; attempt < 2; attempt++ {
		data, summary, err = syncSnapshot(ctx, getter, options)
		if !errors.Is(err, errSnapshotChanged) {
			break
		}
	}
	if err == nil {
		err = enrichRooms(ctx, getter, data.Showtimes, &summary, roomBudget, roomLimit)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		if schedule.ValidateDataset(data, true) != nil {
			err = fmt.Errorf("%w: invalid Noé Cinémas dataset", schedule.ErrDatasetValidation)
		}
	}
	summary.Requests = getter.RequestCount()
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
	body, err := g.Get(ctx, OperationCinemas, CinemasURL)
	if err != nil {
		return schedule.Dataset{}, summary, err
	}
	cinemas, err := parseCinemas(body)
	if err != nil {
		return schedule.Dataset{}, summary, err
	}
	summary.Cinemas = len(cinemas)
	programs, err := parallel.MapOrdered(ctx, cinemas, parallel.Options{Workers: min(WorkerCount, len(cinemas))}, func(ctx context.Context, c cinema) (map[string][]string, error) {
		body, err := g.Get(ctx, OperationProgram, programURL(c.ID))
		if err != nil {
			return nil, err
		}
		return parseProgram(body, o.From, location)
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
		items, err := parseMovies(body)
		if err != nil {
			return nil, err
		}
		if len(items) != len(ids) {
			return nil, errSnapshotChanged
		}
		for _, id := range ids {
			if items[id].ProviderID == "" {
				return nil, errSnapshotChanged
			}
		}
		return items, nil
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
	data := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderNoeCinemas, Scope: schedule.ScopeAll, Timezone: schedule.Timezone, GeneratedAt: o.Now.UTC(), Window: schedule.Window{From: o.From, Through: o.From}, Theaters: []schedule.TheaterRecord{}, Showtimes: []schedule.ShowtimeRecord{}}
	type job struct {
		cinema  cinema
		program map[string][]string
		through string
	}
	jobs := []job{}
	for i, c := range cinemas {
		dates, through := programDates(programs[i])
		l := c.PracticalInfo.Location
		id := "noecinemas-" + c.ID
		data.Theaters = append(data.Theaters, schedule.TheaterRecord{Provider: schedule.ProviderNoeCinemas, ProviderID: c.ID, ID: id, Slug: id, Name: c.Name, Address: l.Address, City: l.City, PostalCode: l.Zip, AvailableDates: dates, AcceptedPasses: []string{}})
		if through != "" {
			jobs = append(jobs, job{c, programs[i], through})
			if through > data.Window.Through {
				data.Window.Through = through
			}
		}
	}
	summary.Jobs = len(jobs)
	allowMissing := ""
	service := o.Now.In(location)
	if service.Hour() < 3 {
		service = service.AddDate(0, 0, -1)
	}
	if service.Format(time.DateOnly) == o.From {
		allowMissing = o.From
	}
	showings, err := parallel.MapOrdered(ctx, jobs, parallel.Options{Workers: min(WorkerCount, len(jobs))}, func(ctx context.Context, j job) ([]schedule.ShowtimeRecord, error) {
		f := scheduleFetcher{getter: g, cinema: j.cinema, movies: movies, location: location, allowMissing: allowMissing, cache: map[string][]schedule.ShowtimeRecord{}}
		return f.fetch(ctx, j.program, o.From, j.through)
	})
	if err != nil {
		return schedule.Dataset{}, summary, err
	}
	for _, group := range showings {
		data.Showtimes = append(data.Showtimes, group...)
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
		return schedule.Dataset{}, summary, fmt.Errorf("%w: invalid Noé Cinémas snapshot", schedule.ErrDatasetValidation)
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

type scheduleFetcher struct {
	getter       Getter
	cinema       cinema
	movies       map[string]schedule.MovieRecord
	location     *time.Location
	allowMissing string
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
	records, err := parseSchedule(body, f.cinema, filtered, f.movies, f.location, f.allowMissing)
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
	dates, _ := programDates(p)
	for _, date := range dates {
		if date >= from && date <= through {
			if _, err := f.window(ctx, p, date, date); err != nil {
				return nil, err
			}
			break
		}
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
func fatalRequest(err error) bool {
	var r *RequestError
	return errors.As(err, &r) && r.Kind == syncproxy.FailureChallenge
}
