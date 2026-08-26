package cgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"sync"
	"time"

	"messeances/api/internal/schedule"
)

const (
	WorkerCount    = 16
	MovieBatchSize = 50
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
	Cinemas, Movies, Jobs, Requests, Showtimes int
	GeneratedAt                                time.Time
}

type datasetValidationError struct {
	message string
	cause   error
}

func (e datasetValidationError) Error() string        { return e.message }
func (e datasetValidationError) Unwrap() error        { return e.cause }
func (e datasetValidationError) Is(target error) bool { return target == schedule.ErrDatasetValidation }

var errProviderSnapshotChanged = errors.New("CGR provider snapshot changed during synchronization")

type programResult struct {
	program map[string][]string
}

type indexedJob[T any] struct {
	index int
	value T
}
type indexedResult[T any] struct {
	index int
	value T
}

func Sync(ctx context.Context, getter Getter, options SyncOptions) (result schedule.Dataset, summary SyncSummary, resultErr error) {
	result, summary, resultErr = syncSnapshot(ctx, getter, options)
	if errors.Is(resultErr, errProviderSnapshotChanged) {
		result, summary, resultErr = syncSnapshot(ctx, getter, options)
	}
	summary.Requests = getter.RequestCount()
	return result, summary, resultErr
}

func syncSnapshot(ctx context.Context, getter Getter, options SyncOptions) (result schedule.Dataset, summary SyncSummary, resultErr error) {
	summary.GeneratedAt = options.Now.UTC()
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return schedule.Dataset{}, summary, fmt.Errorf("load schedule timezone")
	}
	from, err := time.ParseInLocation("2006-01-02", options.From, location)
	if err != nil || from.Format("2006-01-02") != options.From {
		return schedule.Dataset{}, summary, fmt.Errorf("invalid from date")
	}
	body, err := getter.Get(ctx, OperationCinemas, CinemasURL)
	if err != nil {
		return schedule.Dataset{}, summary, err
	}
	cinemas, err := parseCinemas(body)
	if err != nil {
		return schedule.Dataset{}, summary, fmt.Errorf("parse CGR cinemas: %w", err)
	}
	summary.Cinemas = len(cinemas)
	programs, err := runJobs(ctx, cinemas, func(phaseCtx context.Context, theater cinema) (programResult, error) {
		body, fetchErr := getter.Get(phaseCtx, OperationProgram, programURL(theater.id))
		if fetchErr != nil {
			return programResult{}, fetchErr
		}
		program, parseErr := parseProgram(body, from, location)
		if parseErr != nil {
			return programResult{}, fmt.Errorf("parse CGR scheduled movies: %w", parseErr)
		}
		return programResult{program: program}, nil
	})
	if err != nil {
		return schedule.Dataset{}, summary, err
	}
	movieSet := map[string]bool{}
	for _, result := range programs {
		for id := range result.program {
			movieSet[id] = true
		}
	}
	movieIDs := make([]string, 0, len(movieSet))
	for id := range movieSet {
		movieIDs = append(movieIDs, id)
	}
	sort.Strings(movieIDs)
	batches := batchMovieIDs(movieIDs)
	movieGroups, err := runJobs(ctx, batches, func(phaseCtx context.Context, ids []string) (map[string]movie, error) {
		body, fetchErr := getter.Get(phaseCtx, OperationMovies, moviesURL(ids))
		if fetchErr != nil {
			return nil, fetchErr
		}
		items, parseErr := parseMovies(body)
		if parseErr != nil {
			return nil, fmt.Errorf("parse CGR movies: %w", parseErr)
		}
		for _, id := range ids {
			if _, ok := items[id]; !ok {
				return nil, fmt.Errorf("CGR movie batch is incomplete")
			}
		}
		if len(items) != len(ids) {
			return nil, fmt.Errorf("CGR movie batch is incomplete")
		}
		return items, nil
	})
	if err != nil {
		return schedule.Dataset{}, summary, err
	}
	movies := make(map[string]movie, len(movieIDs))
	for _, group := range movieGroups {
		for id, item := range group {
			if movies[id].id != "" {
				return schedule.Dataset{}, summary, datasetValidationError{message: "CGR sync produced duplicate movie identity"}
			}
			movies[id] = item
		}
	}
	summary.Movies = len(movies)
	type scheduleJob struct {
		index   int
		theater cinema
		program map[string][]string
		through string
	}
	type scheduleResult struct {
		index   int
		program map[string][]string
		records []schedule.ShowtimeRecord
	}
	jobs := []scheduleJob{}
	dataset := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderCGR, Scope: schedule.ScopeAll, GeneratedAt: options.Now.UTC(), Timezone: schedule.Timezone, Window: schedule.Window{From: options.From, Through: options.From}, Theaters: make([]schedule.TheaterRecord, 0, len(cinemas)), Showtimes: []schedule.ShowtimeRecord{}}
	for index, theater := range cinemas {
		dateSet := map[string]bool{}
		through := ""
		for _, dates := range programs[index].program {
			for _, date := range dates {
				dateSet[date] = true
				if date > through {
					through = date
				}
			}
		}
		dates := make([]string, 0, len(dateSet))
		for date := range dateSet {
			dates = append(dates, date)
		}
		sort.Strings(dates)
		dataset.Theaters = append(dataset.Theaters, schedule.TheaterRecord{Provider: schedule.ProviderCGR, ID: "cgr-" + theater.id, ProviderID: theater.id, Slug: "cgr-" + theater.id, Name: theater.name, Address: theater.address, City: theater.city, PostalCode: theater.postalCode, AvailableDates: dates, AcceptedPasses: []string{}})
		if through != "" {
			if through > dataset.Window.Through {
				dataset.Window.Through = through
			}
			jobs = append(jobs, scheduleJob{index: index, theater: theater, program: programs[index].program, through: through})
		}
	}
	summary.Jobs = len(jobs)
	allowMissingDate := expiringServiceDate(options.Now, location, options.From)
	groups, err := runJobs(ctx, jobs, func(phaseCtx context.Context, job scheduleJob) (scheduleResult, error) {
		currentProgram := job.program
		through := job.through
		records, fetchErr := fetchCompleteSchedule(phaseCtx, getter, job.theater, currentProgram, movies, location, options.From, through, allowMissingDate)
		if errors.Is(fetchErr, errProviderSnapshotChanged) {
			body, refreshErr := getter.Get(phaseCtx, OperationProgram, programURL(job.theater.id))
			if refreshErr != nil {
				return scheduleResult{}, refreshErr
			}
			currentProgram, refreshErr = parseProgram(body, from, location)
			if refreshErr != nil {
				return scheduleResult{}, fmt.Errorf("parse refreshed CGR scheduled movies: %w", refreshErr)
			}
			for movieID := range currentProgram {
				if _, ok := movies[movieID]; !ok {
					return scheduleResult{}, fmt.Errorf("%w: refreshed program references unknown movie", errProviderSnapshotChanged)
				}
			}
			_, through = programDates(currentProgram)
			if through == "" {
				records, fetchErr = []schedule.ShowtimeRecord{}, nil
			} else {
				records, fetchErr = fetchCompleteSchedule(phaseCtx, getter, job.theater, currentProgram, movies, location, options.From, through, allowMissingDate)
			}
		}
		if fetchErr != nil {
			return scheduleResult{}, fetchErr
		}
		return scheduleResult{index: job.index, program: currentProgram, records: records}, nil
	})
	if err != nil {
		return schedule.Dataset{}, summary, err
	}
	dataset.Window.Through = options.From
	seen := map[string]bool{}
	for _, group := range groups {
		dates, through := programDates(group.program)
		dataset.Theaters[group.index].AvailableDates = dates
		if through > dataset.Window.Through {
			dataset.Window.Through = through
		}
		for _, record := range group.records {
			if seen[record.ProviderShowingID] {
				return schedule.Dataset{}, summary, datasetValidationError{message: "CGR sync produced duplicate showing identity: theater=" + record.TheaterID + " showing=" + record.ProviderShowingID}
			}
			seen[record.ProviderShowingID] = true
			dataset.Showtimes = append(dataset.Showtimes, record)
		}
	}
	sort.Slice(dataset.Showtimes, func(i, j int) bool {
		a, b := dataset.Showtimes[i], dataset.Showtimes[j]
		if a.TheaterID != b.TheaterID {
			return a.TheaterID < b.TheaterID
		}
		if a.ServiceDate != b.ServiceDate {
			return a.ServiceDate < b.ServiceDate
		}
		if !a.StartTime.Equal(b.StartTime) {
			return a.StartTime.Before(b.StartTime)
		}
		return a.ID < b.ID
	})
	if len(dataset.Theaters) == 0 {
		return schedule.Dataset{}, summary, datasetValidationError{message: "CGR sync produced no cinemas"}
	}
	if len(dataset.Showtimes) == 0 {
		return schedule.Dataset{}, summary, datasetValidationError{message: "CGR sync produced no showtimes"}
	}
	if err := schedule.ValidateDataset(dataset, true); err != nil {
		return schedule.Dataset{}, summary, datasetValidationError{message: "validate synchronized CGR dataset: " + err.Error(), cause: err}
	}
	summary.Showtimes = len(dataset.Showtimes)
	return dataset, summary, nil
}

func programDates(program map[string][]string) ([]string, string) {
	dateSet := make(map[string]bool)
	through := ""
	for _, dates := range program {
		for _, date := range dates {
			dateSet[date] = true
			if date > through {
				through = date
			}
		}
	}
	dates := make([]string, 0, len(dateSet))
	for date := range dateSet {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	return dates, through
}

type scheduleFetcher struct {
	getter           Getter
	theater          cinema
	movies           map[string]movie
	location         *time.Location
	allowMissingDate string
	cache            map[string][]schedule.ShowtimeRecord
}

func fetchCompleteSchedule(ctx context.Context, getter Getter, theater cinema, program map[string][]string, movies map[string]movie, location *time.Location, from, through, allowMissingDate string) ([]schedule.ShowtimeRecord, error) {
	fetcher := scheduleFetcher{getter: getter, theater: theater, movies: movies, location: location, allowMissingDate: allowMissingDate, cache: map[string][]schedule.ShowtimeRecord{}}
	records, err := fetcher.fetchWindow(ctx, program, from, through)
	if err == nil || from == through || !isScheduleServerError(err) {
		return records, err
	}
	probeDate := firstProgramDate(program, from, through)
	if probeDate == "" {
		return nil, err
	}
	if _, probeErr := fetcher.fetchWindow(ctx, program, probeDate, probeDate); probeErr != nil {
		return nil, probeErr
	}
	return fetcher.splitFailedWindow(ctx, program, from, through)
}

func (f *scheduleFetcher) fetchOrSplit(ctx context.Context, program map[string][]string, from, through string) ([]schedule.ShowtimeRecord, error) {
	records, err := f.fetchWindow(ctx, program, from, through)
	if err == nil || from == through || !isScheduleServerError(err) {
		return records, err
	}
	return f.splitFailedWindow(ctx, program, from, through)
}

func (f *scheduleFetcher) splitFailedWindow(ctx context.Context, program map[string][]string, from, through string) ([]schedule.ShowtimeRecord, error) {
	start, _ := time.ParseInLocation("2006-01-02", from, f.location)
	end, _ := time.ParseInLocation("2006-01-02", through, f.location)
	middle := start.AddDate(0, 0, int(end.Sub(start).Hours()/24)/2)
	leftThrough := middle.Format("2006-01-02")
	rightFrom := middle.AddDate(0, 0, 1).Format("2006-01-02")
	left, err := f.fetchOrSplit(ctx, program, from, leftThrough)
	if err != nil {
		return nil, err
	}
	right, err := f.fetchOrSplit(ctx, program, rightFrom, through)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

func (f *scheduleFetcher) fetchWindow(ctx context.Context, program map[string][]string, from, through string) ([]schedule.ShowtimeRecord, error) {
	windowProgram := filterProgramDates(program, from, through)
	if len(windowProgram) == 0 {
		return []schedule.ShowtimeRecord{}, nil
	}
	key := from + "\x00" + through
	if records, ok := f.cache[key]; ok {
		return append([]schedule.ShowtimeRecord(nil), records...), nil
	}
	body, err := f.getter.Get(ctx, OperationSchedule, scheduleURL(f.theater.id, f.theater.timeZone, from, through))
	if err != nil {
		return nil, err
	}
	records, err := parseSchedule(body, f.theater, windowProgram, f.movies, f.location, f.allowMissingDate)
	if err != nil {
		return nil, fmt.Errorf("parse CGR schedule: %w", err)
	}
	f.cache[key] = append([]schedule.ShowtimeRecord(nil), records...)
	return records, nil
}

func expiringServiceDate(now time.Time, location *time.Location, from string) string {
	serviceDay := now.In(location)
	if serviceDay.Hour() < 3 {
		serviceDay = serviceDay.AddDate(0, 0, -1)
	}
	date := serviceDay.Format("2006-01-02")
	if date == from {
		return date
	}
	return ""
}

func filterProgramDates(program map[string][]string, from, through string) map[string][]string {
	result := make(map[string][]string, len(program))
	for movieID, dates := range program {
		for _, date := range dates {
			if date >= from && date <= through {
				result[movieID] = append(result[movieID], date)
			}
		}
	}
	return result
}

func firstProgramDate(program map[string][]string, from, through string) string {
	first := ""
	for _, dates := range program {
		for _, date := range dates {
			if date >= from && date <= through && (first == "" || date < first) {
				first = date
			}
		}
	}
	return first
}

func isScheduleServerError(err error) bool {
	var requestErr *RequestError
	return errors.As(err, &requestErr) && requestErr.Operation == OperationSchedule && requestErr.Category == CategoryServer
}

func programURL(theaterID string) string {
	query := url.Values{"theaterId": {theaterID}}
	return APIBaseURL + "/api/gatsby-source-boxofficeapi/scheduledMovies?" + query.Encode()
}

func scheduleURL(theaterID, timeZone, from, through string) string {
	theater, _ := json.Marshal(struct {
		ID       string `json:"id"`
		TimeZone string `json:"timeZone"`
	}{ID: theaterID, TimeZone: timeZone})
	start, _ := time.Parse("2006-01-02", from)
	end, _ := time.Parse("2006-01-02", through)
	query := url.Values{"theaters": {string(theater)}, "from": {start.Format("2006-01-02") + "T03:00:00"}, "to": {end.AddDate(0, 0, 1).Format("2006-01-02") + "T03:00:00"}}
	return APIBaseURL + "/api/gatsby-source-boxofficeapi/schedule?" + query.Encode()
}

func moviesURL(ids []string) string {
	query := url.Values{"basic": {"false"}, "castingLimit": {"3"}}
	for _, id := range ids {
		query.Add("ids", id)
	}
	return APIBaseURL + "/api/gatsby-source-boxofficeapi/movies?" + query.Encode()
}

func batchMovieIDs(ids []string) [][]string {
	result := make([][]string, 0, (len(ids)+MovieBatchSize-1)/MovieBatchSize)
	for start := 0; start < len(ids); start += MovieBatchSize {
		end := min(start+MovieBatchSize, len(ids))
		result = append(result, append([]string(nil), ids[start:end]...))
	}
	return result
}

func runJobs[J, R any](ctx context.Context, jobs []J, work func(context.Context, J) (R, error)) ([]R, error) {
	if len(jobs) == 0 {
		return []R{}, nil
	}
	phaseCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	queue := make(chan indexedJob[J])
	results := make(chan indexedResult[R], len(jobs))
	var workers sync.WaitGroup
	var firstError error
	var capture sync.Once
	count := min(WorkerCount, len(jobs))
	workers.Add(count)
	for range count {
		go func() {
			defer workers.Done()
			for job := range queue {
				if phaseCtx.Err() != nil {
					return
				}
				value, err := work(phaseCtx, job.value)
				if err != nil {
					capture.Do(func() { firstError = err; cancel() })
					return
				}
				results <- indexedResult[R]{index: job.index, value: value}
			}
		}()
	}
	for index, job := range jobs {
		select {
		case <-phaseCtx.Done():
			break
		case queue <- indexedJob[J]{index: index, value: job}:
		}
		if phaseCtx.Err() != nil {
			break
		}
	}
	close(queue)
	workers.Wait()
	close(results)
	if firstError != nil {
		return nil, firstError
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ordered := make([]R, len(jobs))
	received := 0
	for result := range results {
		ordered[result.index] = result.value
		received++
	}
	if received != len(jobs) {
		return nil, errors.New("CGR phase canceled")
	}
	return ordered, nil
}
