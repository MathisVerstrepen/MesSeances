package pathe

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"time"

	"messeances/api/internal/parallel"
	"messeances/api/internal/schedule"
)

const WorkerCount = 24

type Getter interface {
	Get(context.Context, Operation, string) ([]byte, error)
	RequestCount() int
}

type SyncOptions struct {
	From string
	Now  time.Time
}

type SyncSummary struct {
	Cinemas     int
	Movies      int
	Events      int
	Jobs        int
	Requests    int
	Showtimes   int
	GeneratedAt time.Time
}

type datasetValidationError struct {
	message string
	cause   error
}

func (e datasetValidationError) Error() string { return e.message }
func (e datasetValidationError) Unwrap() error { return e.cause }
func (e datasetValidationError) Is(target error) bool {
	return target == schedule.ErrDatasetValidation
}

type showtimeJob struct {
	operation      Operation
	rawURL         string
	theater        cinema
	movie          show
	advertisedDate string
	dates          []string
}

func Sync(ctx context.Context, getter Getter, options SyncOptions) (schedule.Dataset, SyncSummary, error) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("load schedule timezone")
	}
	from, err := time.ParseInLocation("2006-01-02", options.From, location)
	if err != nil || from.Format("2006-01-02") != options.From {
		return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("invalid from date")
	}

	cinemaBody, err := getter.Get(ctx, OperationCinemas, CinemasURL)
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, err
	}
	cinemas, err := parseCinemas(cinemaBody)
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("parse Pathé cinemas: %w", err)
	}
	if len(cinemas) == 0 {
		return schedule.Dataset{}, SyncSummary{}, datasetValidationError{message: "Pathé sync produced no active cinemas"}
	}
	showBody, err := getter.Get(ctx, OperationShows, ShowsURL)
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, err
	}
	shows, err := parseShows(showBody)
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("parse Pathé shows: %w", err)
	}

	programs, err := parallel.MapOrdered(ctx, cinemas, parallel.Options{Workers: WorkerCount}, func(phaseCtx context.Context, theater cinema) (map[string][]string, error) {
		body, fetchErr := getter.Get(phaseCtx, OperationCinemaProgram, cinemaProgramURL(theater.slug))
		if fetchErr != nil {
			return nil, fetchErr
		}
		pairs, parseErr := parseProgram(body, shows, from, location)
		if parseErr != nil {
			return nil, fmt.Errorf("parse Pathé cinema program: %w", parseErr)
		}
		return pairs, nil
	})
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, err
	}

	dataset := schedule.Dataset{
		SchemaVersion: schedule.SchemaVersion,
		Provider:      schedule.ProviderPathe,
		Scope:         schedule.ScopeAll,
		GeneratedAt:   options.Now.UTC(),
		Timezone:      schedule.Timezone,
		Window:        schedule.Window{From: options.From},
		Theaters:      make([]schedule.TheaterRecord, 0, len(cinemas)),
		Showtimes:     []schedule.ShowtimeRecord{},
	}
	jobs := []showtimeJob{}
	movieJobs, eventJobs := 0, 0
	for index, theater := range cinemas {
		dateSet := map[string]bool{}
		showSlugs := make([]string, 0, len(programs[index]))
		for showSlug, dates := range programs[index] {
			showSlugs = append(showSlugs, showSlug)
			for _, date := range dates {
				dateSet[date] = true
				if date > dataset.Window.Through {
					dataset.Window.Through = date
				}
			}
		}
		sort.Strings(showSlugs)
		availableDates := make([]string, 0, len(dateSet))
		for date := range dateSet {
			availableDates = append(availableDates, date)
		}
		sort.Strings(availableDates)
		dataset.Theaters = append(dataset.Theaters, schedule.TheaterRecord{
			Provider:       schedule.ProviderPathe,
			ID:             "pathe-" + theater.slug,
			ProviderID:     theater.slug,
			Slug:           "pathe-" + theater.slug,
			Name:           theater.name,
			Address:        theater.address,
			City:           theater.city,
			PostalCode:     theater.postalCode,
			AvailableDates: availableDates,
			AcceptedPasses: []string{},
		})
		for _, showSlug := range showSlugs {
			movie := shows[showSlug]
			dates := append([]string(nil), programs[index][showSlug]...)
			if movie.isMovie {
				jobs = append(jobs, showtimeJob{operation: OperationMovieTimes, rawURL: movieShowtimesURL(movie.slug, theater.slug), theater: theater, movie: movie, dates: dates})
				movieJobs++
				continue
			}
			for _, date := range dates {
				jobs = append(jobs, showtimeJob{operation: OperationEventTimes, rawURL: eventShowtimesURL(movie.slug, theater.slug, date), theater: theater, movie: movie, advertisedDate: date})
				eventJobs++
			}
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].rawURL < jobs[j].rawURL })

	showtimeGroups, err := parallel.MapOrdered(ctx, jobs, parallel.Options{Workers: WorkerCount}, func(phaseCtx context.Context, job showtimeJob) ([]schedule.ShowtimeRecord, error) {
		body, fetchErr := getter.Get(phaseCtx, job.operation, job.rawURL)
		if fetchErr != nil {
			return nil, fetchErr
		}
		if job.operation == OperationMovieTimes {
			return parseMovieShowtimeResponse(body, job, location)
		}
		var response []sessionResponse
		if decodeErr := decodeJSON(body, &response); decodeErr != nil {
			return nil, fmt.Errorf("parse Pathé event showtimes: %w", decodeErr)
		}
		if response == nil {
			return nil, fmt.Errorf("parse Pathé event showtimes: response is incomplete")
		}
		return parseSessions(response, job.movie, job.theater, job.advertisedDate, location)
	})
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, err
	}
	seenShowingIDs := map[string]bool{}
	for _, group := range showtimeGroups {
		for _, record := range group {
			if seenShowingIDs[record.ProviderShowingID] {
				return schedule.Dataset{}, SyncSummary{}, datasetValidationError{message: "Pathé sync produced duplicate showing identity"}
			}
			seenShowingIDs[record.ProviderShowingID] = true
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
		return schedule.Dataset{}, SyncSummary{}, datasetValidationError{message: "Pathé sync produced no active cinemas"}
	}
	if len(dataset.Showtimes) == 0 {
		return schedule.Dataset{}, SyncSummary{}, datasetValidationError{message: "Pathé sync produced no showtimes"}
	}
	if err := schedule.ValidateDataset(dataset, true); err != nil {
		return schedule.Dataset{}, SyncSummary{}, datasetValidationError{message: "validate synchronized Pathé dataset: " + err.Error(), cause: err}
	}
	summary := SyncSummary{Cinemas: len(dataset.Theaters), Movies: movieJobs, Events: eventJobs, Jobs: len(jobs), Requests: getter.RequestCount(), Showtimes: len(dataset.Showtimes), GeneratedAt: dataset.GeneratedAt}
	return dataset, summary, nil
}

func parseMovieShowtimeResponse(body []byte, job showtimeJob, location *time.Location) ([]schedule.ShowtimeRecord, error) {
	var response map[string][]sessionResponse
	if err := decodeJSON(body, &response); err != nil {
		return nil, fmt.Errorf("parse Pathé movie showtimes: %w", err)
	}
	if response == nil {
		return nil, fmt.Errorf("parse Pathé movie showtimes: response is incomplete")
	}
	result := []schedule.ShowtimeRecord{}
	for _, date := range job.dates {
		items, exists := response[date]
		if !exists {
			continue
		}
		if items == nil {
			return nil, fmt.Errorf("parse Pathé movie showtimes: advertised date response is incomplete")
		}
		records, err := parseSessions(items, job.movie, job.theater, date, location)
		if err != nil {
			return nil, err
		}
		result = append(result, records...)
	}
	return result, nil
}

func parseSessions(items []sessionResponse, movie show, theater cinema, advertisedDate string, location *time.Location) ([]schedule.ShowtimeRecord, error) {
	result := make([]schedule.ShowtimeRecord, 0, len(items))
	for _, item := range items {
		record, err := parseSession(item, movie, theater, advertisedDate, location)
		if err != nil {
			return nil, fmt.Errorf("parse Pathé showtime: %w", err)
		}
		result = append(result, record)
	}
	return result, nil
}

func cinemaProgramURL(cinemaSlug string) string {
	return APIBaseURL + "/api/cinema/" + url.PathEscape(cinemaSlug) + "/shows"
}

func movieShowtimesURL(showSlug, cinemaSlug string) string {
	return APIBaseURL + "/api/show/" + url.PathEscape(showSlug) + "/showtimes/" + url.PathEscape(cinemaSlug)
}

func eventShowtimesURL(showSlug, cinemaSlug, date string) string {
	return movieShowtimesURL(showSlug, cinemaSlug) + "/" + url.PathEscape(date)
}
