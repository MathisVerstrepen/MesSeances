package ugc

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"movieflow/api/internal/schedule"
)

const SitemapURL = "https://www.ugc.fr/dynamique/sitemaps/frontend/sitemap.xml"

type Getter interface {
	Get(context.Context, string, string) (FetchResult, error)
	RequestCount() int
}
type SyncOptions struct {
	From     string
	Through  string
	CinemaID string
	Now      time.Time
}
type SyncSummary struct {
	Scope       string
	Cinemas     int
	Dates       int
	Requests    int
	Showtimes   int
	Skipped     int
	GeneratedAt time.Time
}

func Sync(ctx context.Context, client Getter, options SyncOptions) (schedule.Dataset, SyncSummary, error) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, err
	}
	from, err := time.ParseInLocation("2006-01-02", options.From, location)
	if err != nil || from.Format("2006-01-02") != options.From {
		return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("invalid from date")
	}
	through, err := time.ParseInLocation("2006-01-02", options.Through, location)
	if err != nil || through.Format("2006-01-02") != options.Through || !schedule.ValidInclusiveDateWindow(from, through) {
		return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("invalid date window")
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
	if len(selected) > schedule.MaxTheaters {
		return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("sitemap cinema limit exceeded")
	}
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
	data := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderUGC, Scope: scope, GeneratedAt: options.Now.UTC(), Timezone: schedule.Timezone, Window: schedule.Window{From: options.From, Through: options.Through}, Theaters: []schedule.TheaterRecord{}, Showtimes: []schedule.ShowtimeRecord{}}
	dateCount := 0
	skipped := 0
	seenCanonical := map[string]bool{}
	for _, id := range selected {
		cinemaURL := "https://www.ugc.fr/cinema.html?id=" + url.QueryEscape(id)
		page, requestErr := client.Get(ctx, "cinema "+id, cinemaURL)
		if requestErr != nil {
			return schedule.Dataset{}, SyncSummary{}, requestErr
		}
		canonicalID, inactive, finalURLErr := classifyCinemaFinalURL(page.FinalURL)
		if finalURLErr != nil {
			return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("cinema %s response has unexpected final URL", id)
		}
		if inactive {
			if scope == schedule.ScopeSingle {
				return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("cinema %s is inactive: redirected to UGC cinema directory", id)
			}
			skipped++
			continue
		}
		if seenCanonical[canonicalID] {
			skipped++
			continue
		}
		seenCanonical[canonicalID] = true
		cinema, parseErr := ParseCinema(bytes.NewReader(page.Body), canonicalID)
		if parseErr != nil {
			return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("parse cinema %s: %w", id, parseErr)
		}
		intersected := []string{}
		for _, value := range cinema.AdvertisedDates {
			date, parseDateErr := time.ParseInLocation("2006-01-02", value, location)
			if parseDateErr != nil {
				return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("invalid advertised date for cinema %s", id)
			}
			if !date.Before(from) && !date.After(through) {
				intersected = append(intersected, value)
			}
		}
		if len(data.Theaters) >= schedule.MaxTheaters {
			return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("sync theater limit exceeded")
		}
		theater := schedule.TheaterRecord{ID: "ugc-" + canonicalID, ProviderID: canonicalID, Slug: "ugc-" + canonicalID, Name: cinema.Name, Address: cinema.Address, City: cinema.City, PostalCode: cinema.PostalCode, AvailableDates: intersected, AcceptedPasses: []string{"UGC_ILLIMITE"}}
		data.Theaters = append(data.Theaters, theater)
		for _, serviceDate := range intersected {
			dateCount++
			parsed, _ := time.Parse("2006-01-02", serviceDate)
			values := url.Values{"cinemaId": []string{canonicalID}, "date": []string{parsed.Format("02/01/2006")}, "page": []string{"30007"}}
			showingURL := "https://www.ugc.fr/showingsCinemaAjaxAction!getShowingsForCinemaPage.action?" + values.Encode()
			showingPage, requestErr := client.Get(ctx, "showings cinema "+canonicalID+" date "+serviceDate, showingURL)
			if requestErr != nil {
				return schedule.Dataset{}, SyncSummary{}, requestErr
			}
			if !matchesFinalURL(showingPage.FinalURL, showingURL) {
				return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("showings cinema %s date %s response has unexpected final URL", id, serviceDate)
			}
			records, parseErr := ParseShowings(bytes.NewReader(showingPage.Body), cinema, serviceDate)
			if parseErr != nil {
				return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("parse showings cinema %s date %s: %w", id, serviceDate, parseErr)
			}
			if !canAppendShowtimes(len(data.Showtimes), len(records)) {
				return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("sync showing limit exceeded")
			}
			data.Showtimes = append(data.Showtimes, records...)
		}
	}
	if len(data.Theaters) == 0 {
		return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("sync produced no active cinemas")
	}
	if len(data.Showtimes) == 0 {
		return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("sync produced no showtimes")
	}
	if err := schedule.ValidateDataset(data, scope == schedule.ScopeAll); err != nil {
		return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("validate synchronized dataset: %w", err)
	}
	summary := SyncSummary{Scope: scope, Cinemas: len(data.Theaters), Dates: dateCount, Requests: client.RequestCount(), Showtimes: len(data.Showtimes), Skipped: skipped, GeneratedAt: data.GeneratedAt}
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

func canAppendShowtimes(current, additional int) bool {
	return current >= 0 && additional >= 0 && current <= schedule.MaxShowtimes && additional <= schedule.MaxShowtimes-current
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
