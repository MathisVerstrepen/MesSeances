package ugc

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"

	"messeances/api/internal/schedule"
)

const SitemapURL = "https://www.ugc.fr/dynamique/sitemaps/frontend/sitemap.xml"
const ugcWorkerCount = 10

type Getter interface {
	Get(context.Context, string, string) (FetchResult, error)
	RequestCount() int
}
type SyncOptions struct {
	From     string
	CinemaID string
	Now      time.Time
}
type SyncSummary struct {
	Scope       schedule.Scope
	Cinemas     int
	Dates       int
	Requests    int
	Showtimes   int
	Skipped     int
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

func newDatasetValidationError(message string, cause error) error {
	return datasetValidationError{message: message, cause: cause}
}

type indexedJob[T any] struct {
	index int
	value T
}

type indexedResult[T any] struct {
	index int
	value T
}

func runIndexedPhase[J, R any](ctx context.Context, jobs []J, work func(context.Context, J) (R, error)) ([]R, error) {
	if len(jobs) == 0 {
		return []R{}, nil
	}
	phaseCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobQueue := make(chan indexedJob[J], len(jobs))
	results := make(chan indexedResult[R], len(jobs))
	for index, job := range jobs {
		jobQueue <- indexedJob[J]{index: index, value: job}
	}
	close(jobQueue)

	var workers sync.WaitGroup
	var firstError error
	var captureError sync.Once
	workers.Add(ugcWorkerCount)
	for range ugcWorkerCount {
		go func() {
			defer workers.Done()
			for {
				select {
				case <-phaseCtx.Done():
					return
				case job, ok := <-jobQueue:
					if !ok {
						return
					}
					if phaseCtx.Err() != nil {
						return
					}
					result, err := work(phaseCtx, job.value)
					if err != nil {
						captureError.Do(func() {
							firstError = err
							cancel()
						})
						return
					}
					results <- indexedResult[R]{index: job.index, value: result}
				}
			}
		}()
	}
	workers.Wait()
	close(results)
	if firstError != nil {
		return nil, firstError
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	ordered := make([]R, len(jobs))
	count := 0
	for result := range results {
		ordered[result.index] = result.value
		count++
	}
	if count != len(jobs) {
		return nil, fmt.Errorf("UGC phase canceled")
	}
	return ordered, nil
}

type cinemaFetchJob struct {
	requestedID string
}

type cinemaFetchResult struct {
	requestedID string
	canonicalID string
	inactive    bool
	body        []byte
}

type showingsFetchJob struct {
	requestedID string
	cinema      Cinema
	serviceDate string
	rawURL      string
}

func Sync(ctx context.Context, client Getter, options SyncOptions) (schedule.Dataset, SyncSummary, error) {
	location, err := scheduleLocation()
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, err
	}
	from, err := time.ParseInLocation("2006-01-02", options.From, location)
	if err != nil || from.Format("2006-01-02") != options.From {
		return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("invalid from date")
	}
	sitemap, err := client.Get(ctx, "sitemap", SitemapURL)
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, err
	}
	if !matchesFinalURL(sitemap.FinalURL, SitemapURL) {
		return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("sitemap response has unexpected final URL")
	}
	ids, err := ParseSitemap(bytes.NewReader(sitemap.Body))
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, err
	}
	selected := ids
	scope := schedule.ScopeAll
	if options.CinemaID != "" {
		found := false
		for _, id := range ids {
			if id == options.CinemaID {
				found = true
				break
			}
		}
		if !found {
			return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("requested cinema is absent from sitemap")
		}
		selected = []string{options.CinemaID}
		scope = schedule.ScopeSingle
	}
	data := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderUGC, Scope: scope, GeneratedAt: options.Now.UTC(), Timezone: schedule.Timezone, Window: schedule.Window{From: options.From}, Theaters: []schedule.TheaterRecord{}, Showtimes: []schedule.ShowtimeRecord{}}
	latestAdvertisedDate := ""
	skipped := 0
	seenCanonical := map[string]bool{}
	cinemaJobs := make([]cinemaFetchJob, len(selected))
	for index, id := range selected {
		cinemaJobs[index] = cinemaFetchJob{requestedID: id}
	}
	cinemaResults, err := runIndexedPhase(ctx, cinemaJobs, func(phaseCtx context.Context, job cinemaFetchJob) (cinemaFetchResult, error) {
		cinemaURL := "https://www.ugc.fr/cinema.html?id=" + url.QueryEscape(job.requestedID)
		page, requestErr := client.Get(phaseCtx, "cinema "+job.requestedID, cinemaURL)
		if requestErr != nil {
			return cinemaFetchResult{}, requestErr
		}
		canonicalID, inactive, finalURLErr := classifyCinemaFinalURL(page.FinalURL)
		if finalURLErr != nil {
			return cinemaFetchResult{}, fmt.Errorf("cinema %s response has unexpected final URL", job.requestedID)
		}
		return cinemaFetchResult{requestedID: job.requestedID, canonicalID: canonicalID, inactive: inactive, body: page.Body}, nil
	})
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, err
	}
	showingsJobs := []showingsFetchJob{}
	for _, result := range cinemaResults {
		if result.inactive {
			if scope == schedule.ScopeSingle {
				return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("cinema %s is inactive: redirected to UGC cinema directory", result.requestedID)
			}
			skipped++
			continue
		}
		if seenCanonical[result.canonicalID] {
			skipped++
			continue
		}
		seenCanonical[result.canonicalID] = true
		cinema, parseErr := ParseCinema(bytes.NewReader(result.body), result.canonicalID)
		if parseErr != nil {
			return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("parse cinema %s: %w", result.requestedID, parseErr)
		}
		included := []string{}
		for _, value := range cinema.AdvertisedDates {
			date, parseDateErr := time.ParseInLocation("2006-01-02", value, location)
			if parseDateErr != nil {
				return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("invalid advertised date for cinema %s", result.requestedID)
			}
			if !date.Before(from) {
				included = append(included, value)
				if value > latestAdvertisedDate {
					latestAdvertisedDate = value
				}
			}
		}
		theater := schedule.TheaterRecord{ID: "ugc-" + result.canonicalID, ProviderID: result.canonicalID, Slug: "ugc-" + result.canonicalID, Name: cinema.Name, Address: cinema.Address, City: cinema.City, PostalCode: cinema.PostalCode, AvailableDates: included, AcceptedPasses: []string{"UGC_ILLIMITE"}}
		data.Theaters = append(data.Theaters, theater)
		for _, serviceDate := range included {
			parsed, _ := time.Parse("2006-01-02", serviceDate)
			values := url.Values{"cinemaId": []string{result.canonicalID}, "date": []string{parsed.Format("02/01/2006")}, "page": []string{"30007"}}
			showingURL := "https://www.ugc.fr/showingsCinemaAjaxAction!getShowingsForCinemaPage.action?" + values.Encode()
			showingsJobs = append(showingsJobs, showingsFetchJob{requestedID: result.requestedID, cinema: cinema, serviceDate: serviceDate, rawURL: showingURL})
		}
	}
	showingsResults, err := runIndexedPhase(ctx, showingsJobs, func(phaseCtx context.Context, job showingsFetchJob) ([]schedule.ShowtimeRecord, error) {
		showingPage, requestErr := client.Get(phaseCtx, "showings cinema "+job.cinema.ProviderID+" date "+job.serviceDate, job.rawURL)
		if requestErr != nil {
			return nil, requestErr
		}
		if !matchesFinalURL(showingPage.FinalURL, job.rawURL) {
			return nil, fmt.Errorf("showings cinema %s date %s response has unexpected final URL", job.requestedID, job.serviceDate)
		}
		records, parseErr := ParseShowings(bytes.NewReader(showingPage.Body), job.cinema, job.serviceDate)
		if parseErr != nil {
			return nil, fmt.Errorf("parse showings cinema %s date %s: %w", job.requestedID, job.serviceDate, parseErr)
		}
		return records, nil
	})
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, err
	}
	for _, records := range showingsResults {
		data.Showtimes = append(data.Showtimes, records...)
	}
	if len(data.Theaters) == 0 {
		return schedule.Dataset{}, SyncSummary{}, newDatasetValidationError("sync produced no active cinemas", nil)
	}
	if len(data.Showtimes) == 0 {
		return schedule.Dataset{}, SyncSummary{}, newDatasetValidationError("sync produced no showtimes", nil)
	}
	data.Window.Through = latestAdvertisedDate
	if err := schedule.ValidateDataset(data, scope == schedule.ScopeAll); err != nil {
		return schedule.Dataset{}, SyncSummary{}, newDatasetValidationError("validate synchronized dataset: "+err.Error(), err)
	}
	summary := SyncSummary{Scope: scope, Cinemas: len(data.Theaters), Dates: len(showingsJobs), Requests: client.RequestCount(), Showtimes: len(data.Showtimes), Skipped: skipped, GeneratedAt: data.GeneratedAt}
	return data, summary, nil
}

func classifyCinemaFinalURL(raw string) (string, bool, error) {
	parsed, err := parseFinalURL(raw)
	if err != nil {
		return "", false, err
	}
	if parsed.Scheme != "https" || parsed.Host != "www.ugc.fr" {
		return "", false, fmt.Errorf("unexpected cinema authority")
	}
	if parsed.Path == "/cinema.html" && parsed.EscapedPath() == "/cinema.html" {
		canonicalID, queryErr := canonicalPositiveIDQuery(parsed)
		if queryErr != nil {
			return "", false, queryErr
		}
		return canonicalID, false, nil
	}
	if parsed.Path != "/cinemas.html" || parsed.EscapedPath() != "/cinemas.html" {
		return "", false, fmt.Errorf("unexpected cinema path")
	}
	if parsed.RawQuery == "" {
		return "", true, nil
	}
	if _, queryErr := canonicalPositiveIDQuery(parsed); queryErr != nil {
		return "", false, fmt.Errorf("unexpected cinema directory query")
	}
	return "", true, nil
}

func canonicalPositiveIDQuery(parsed *url.URL) (string, error) {
	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil || len(values) != 1 || len(values["id"]) != 1 {
		return "", fmt.Errorf("unexpected cinema query")
	}
	id := values["id"][0]
	number, err := strconv.ParseUint(id, 10, 64)
	if err != nil || number == 0 || strconv.FormatUint(number, 10) != id || parsed.RawQuery != "id="+id {
		return "", fmt.Errorf("unexpected cinema query")
	}
	return id, nil
}

func matchesFinalURL(raw string, expected string) bool {
	actual, err := parseFinalURL(raw)
	if err != nil {
		return false
	}
	want, err := parseFinalURL(expected)
	return err == nil && actual.Path == want.Path && actual.RawQuery == want.RawQuery
}

func parseFinalURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || !isAllowedUGCURL(parsed) || parsed.Fragment != "" || parsed.Opaque != "" || parsed.ForceQuery {
		return nil, fmt.Errorf("invalid final URL")
	}
	return parsed, nil
}

func ValidateCinemaID(value string) error {
	if value == "" {
		return nil
	}
	number, err := strconv.ParseUint(value, 10, 64)
	if err != nil || number == 0 {
		return fmt.Errorf("cinema-id must be a positive integer")
	}
	return nil
}
