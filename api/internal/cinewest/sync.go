package cinewest

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"messeances/api/internal/parallel"
	"messeances/api/internal/schedule"
)

type SyncOptions struct {
	From string
	Now  time.Time
}
type SyncSummary struct {
	Cinemas, Movies, Jobs, Requests, Showtimes int
	GeneratedAt                                time.Time
}
type cinemaProgram struct {
	theater schedule.TheaterRecord
	shows   []schedule.ShowtimeRecord
}

func Sync(ctx context.Context, fetcher Fetcher, options SyncOptions) (result schedule.Dataset, summary SyncSummary, resultErr error) {
	defer func() { summary.Requests = fetcher.RequestCount() }()
	summary.GeneratedAt = options.Now.UTC()
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return result, summary, errShape
	}
	from, err := time.ParseInLocation("2006-01-02", options.From, location)
	if err != nil || from.Format("2006-01-02") != options.From || options.Now.IsZero() {
		return result, summary, errShape
	}
	bootstrap, err := fetcher.Bootstrap(ctx)
	if err != nil {
		return result, summary, err
	}
	token, err := bootstrapToken(bootstrap)
	if err != nil {
		return result, summary, err
	}
	body, err := fetcher.Catalog(ctx, "cinemas", token)
	if err != nil {
		return result, summary, err
	}
	cinemas, err := parseOfficeCinemas(body)
	if err != nil {
		return result, summary, err
	}
	ids := schedule.CinewestTheaterIDs()
	programs, err := parallel.MapOrdered(ctx, ids, parallel.Options{Workers: 2}, func(ctx context.Context, id string) (cinemaProgram, error) {
		switch {
		case strings.HasPrefix(id, "cineoffice-"):
			return officeProgram(ctx, fetcher, cinemas[strings.TrimPrefix(id, "cineoffice-")], location)
		case strings.HasPrefix(id, "ticketingcine-"):
			return loadTicketProgram(ctx, fetcher, strings.TrimPrefix(id, "ticketingcine-"), location)
		default:
			return webProgram(ctx, fetcher, from, location)
		}
	})
	if err != nil {
		return result, summary, err
	}
	dataset := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderCinewest, Scope: schedule.ScopeAll, GeneratedAt: options.Now.UTC(), Timezone: schedule.Timezone, Window: schedule.Window{From: options.From, Through: options.From}, Theaters: []schedule.TheaterRecord{}, Showtimes: []schedule.ShowtimeRecord{}}
	movies := map[string]schedule.MovieRecord{}
	seen := map[string]schedule.ShowtimeRecord{}
	for _, p := range programs {
		if err := ctx.Err(); err != nil {
			return result, summary, err
		}
		dates := map[string]bool{}
		for _, s := range p.shows {
			if old, ok := seen[s.ID]; ok {
				if !reflect.DeepEqual(old, s) {
					return result, summary, errShape
				}
				continue
			}
			seen[s.ID] = s
			if old, ok := movies[s.Movie.ProviderID]; ok && !reflect.DeepEqual(old, s.Movie) {
				return result, summary, errShape
			}
			movies[s.Movie.ProviderID] = s.Movie
			if s.ServiceDate < options.From {
				continue
			}
			dates[s.ServiceDate] = true
			if s.StartTime.In(location).Format("2006-01-02") > dataset.Window.Through {
				dataset.Window.Through = s.StartTime.In(location).Format("2006-01-02")
			}
			dataset.Showtimes = append(dataset.Showtimes, s)
		}
		p.theater.AvailableDates = make([]string, 0, len(dates))
		for date := range dates {
			p.theater.AvailableDates = append(p.theater.AvailableDates, date)
		}
		sort.Strings(p.theater.AvailableDates)
		dataset.Theaters = append(dataset.Theaters, p.theater)
	}
	sort.Slice(dataset.Theaters, func(i, j int) bool { return dataset.Theaters[i].ID < dataset.Theaters[j].ID })
	sort.Slice(dataset.Showtimes, func(i, j int) bool {
		a, b := dataset.Showtimes[i], dataset.Showtimes[j]
		if a.TheaterID != b.TheaterID {
			return a.TheaterID < b.TheaterID
		}
		if !a.StartTime.Equal(b.StartTime) {
			return a.StartTime.Before(b.StartTime)
		}
		return a.ID < b.ID
	})
	if err := schedule.ValidateDataset(dataset, true); err != nil {
		return result, summary, fmt.Errorf("%w: invalid synchronized Cinewest dataset", schedule.ErrDatasetValidation)
	}
	summary.Cinemas, summary.Jobs, summary.Movies, summary.Showtimes = len(dataset.Theaters), len(ids), len(movies), len(dataset.Showtimes)
	return dataset, summary, nil
}

func theater(id, name, address, city, zip string) schedule.TheaterRecord {
	return schedule.TheaterRecord{Provider: schedule.ProviderCinewest, ProviderID: id, ID: "cinewest-" + id, Slug: "cinewest-" + id, Name: clean(name), Address: clean(address), City: clean(city), PostalCode: clean(zip), AvailableDates: []string{}, AcceptedPasses: []string{}}
}
func movie(id, title, poster string, runtime int) (schedule.MovieRecord, error) {
	if !schedule.ValidCinewestIdentity("movie", id) || clean(title) == "" {
		return schedule.MovieRecord{}, errShape
	}
	if _, ok := schedule.RuntimeDuration(runtime); !ok && runtime != 0 {
		return schedule.MovieRecord{}, errShape
	}
	if !schedule.ValidCinewestPosterURL(poster) {
		poster = ""
	}
	return schedule.MovieRecord{Provider: schedule.ProviderCinewest, ProviderID: id, Slug: "cinewest-film-" + id, Title: clean(title), RuntimeMinutes: runtime, PosterURL: poster}, nil
}
func showing(theaterID, rawID string, m schedule.MovieRecord, start, end time.Time, language schedule.Language, version string, format schedule.Format, room, booking string, firstPart int) (schedule.ShowtimeRecord, error) {
	id, ok := schedule.CinewestShowingID(theaterID, rawID)
	if !ok || clean(room) == "" {
		return schedule.ShowtimeRecord{}, errShape
	}
	if booking == "" || !schedule.ValidCinewestBookingURL(booking, theaterID, id) {
		booking = schedule.CinewestWebsite(theaterID)
	}
	service := start
	if start.Hour() < 3 {
		service = service.AddDate(0, 0, -1)
	}
	return schedule.ShowtimeRecord{Provider: schedule.ProviderCinewest, ID: "cinewest-showing-" + id, ProviderShowingID: id, TheaterID: "cinewest-" + theaterID, ServiceDate: service.Format("2006-01-02"), Movie: m, StartTime: start, EndTime: end, FirstPartDurationMinutes: firstPart, Language: language, ProviderVersion: version, Format: format, Room: clean(room), BookingURL: booking}, nil
}
func secondsRuntime(seconds minutes) (int, error) {
	value := int(seconds) / 60
	if seconds > 0 && value == 0 {
		return 0, errShape
	}
	if _, ok := schedule.RuntimeDuration(value); !ok && value != 0 {
		return 0, errShape
	}
	return value, nil
}
func offsetTime(value string, location *time.Location) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999-0700"} {
		parsed, err := time.Parse(layout, value)
		if err != nil {
			continue
		}
		if parsed.IsZero() {
			return time.Time{}, errShape
		}
		return parsed.In(location), nil
	}
	return time.Time{}, errShape
}
