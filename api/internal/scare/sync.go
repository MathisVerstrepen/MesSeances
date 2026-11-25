package scare

import (
	"context"
	"fmt"
	"slices"
	"time"

	"messeances/api/internal/schedule"
)

// maxPages bounds one synchronization (10,000 rows per page).
const maxPages = 20

// Source is one configured SCARE cinema.
type Source struct {
	CinemaID string
}

type Getter interface {
	Lines(context.Context, Query) ([]byte, error)
	RequestCount() int
}

type SyncOptions struct {
	From    string
	Now     time.Time
	Sources []Source
}

type SyncSummary struct {
	Cinemas, Movies, Jobs, Requests, Showtimes int
	// Skipped counts rows that could not be mapped safely (missing identity,
	// start or title, unknown version, duplicate session).
	Skipped int
	// EmptyCinemas counts configured cinemas without any usable row; they are
	// not published.
	EmptyCinemas int
	// MovieConflicts counts films whose title or runtime differed between rows.
	MovieConflicts int
	GeneratedAt    time.Time
}

// Sync downloads the configured cinemas' rows and publishes them as one
// snapshot. Transport and page-shape failures fail the snapshot; individual
// rows that cannot be mapped are skipped and counted.
func Sync(ctx context.Context, getter Getter, options SyncOptions) (result schedule.Dataset, summary SyncSummary, resultErr error) {
	defer func() { summary.Requests = getter.RequestCount() }()
	summary.GeneratedAt = options.Now.UTC()
	location, err := schedule.SyncLocation(options.From)
	if err != nil {
		return result, summary, err
	}
	if len(options.Sources) == 0 {
		return result, summary, ErrNoSources
	}
	cinemas := make([]string, len(options.Sources))
	for i, source := range options.Sources {
		if !cinemaID.MatchString(source.CinemaID) || slices.Contains(cinemas[:i], source.CinemaID) {
			return result, summary, fmt.Errorf("%w: source cinema", ErrShape)
		}
		cinemas[i] = source.CinemaID
	}
	summary.Jobs = len(cinemas)

	rows := map[string][]Row{}
	after := ""
	for page := 0; ; page++ {
		if page == maxPages {
			return result, summary, fmt.Errorf("%w: too many pages", ErrShape)
		}
		body, err := getter.Lines(ctx, Query{Cinemas: cinemas, From: options.From, After: after})
		if err != nil {
			return result, summary, err
		}
		p, next, err := DecodePage(body)
		if err != nil {
			return result, summary, err
		}
		for _, r := range p.Results {
			id := r.CinemaID.String()
			if !slices.Contains(cinemas, id) {
				return result, summary, fmt.Errorf("%w: unrequested cinema", ErrShape)
			}
			rows[id] = append(rows[id], r)
		}
		if next == "" {
			break
		}
		after = next
	}

	builder := schedule.NewDatasetBuilder(schedule.ProviderScare, options.From, options.Now, schedule.BuilderOptions{})
	seen := map[string]bool{}
	var pending []pendingShowing
	for _, cinema := range cinemas {
		if err := ctx.Err(); err != nil {
			return result, summary, err
		}
		theater, ok := theaterRecord(cinema, rows[cinema])
		if !ok {
			summary.EmptyCinemas++
			summary.Skipped += len(rows[cinema])
			continue
		}
		for _, r := range rows[cinema] {
			showing, ok := showtimeRecord(cinema, theater.ID, r, location)
			if !ok {
				summary.Skipped++
				continue
			}
			if showing.ServiceDate < options.From {
				continue
			}
			if seen[showing.ProviderShowingID] {
				summary.Skipped++
				continue
			}
			seen[showing.ProviderShowingID] = true
			if !builder.MergeMovie(showing.Movie) {
				summary.MovieConflicts++
			}
			pending = append(pending, pendingShowing{showing, r})
		}
		builder.AddTheater(theater)
		summary.Cinemas++
	}
	// Later rows may complete a film (runtime, overview), so every session is
	// bound to the final merged record: publication requires one record per
	// film.
	for _, p := range pending {
		showing := p.showing
		showing.Movie = builder.Movie(showing.Movie.ProviderID)
		showing.EndTime = endTime(showing.StartTime, p.row, showing.Movie.RuntimeMinutes, location)
		builder.ExtendWindow(showing.StartTime.Format(time.DateOnly))
		if err := builder.AddShowtime(showing); err != nil {
			summary.Skipped++
		}
	}
	summary.Movies = builder.MovieCount()
	dataset, err := builder.Build("SCARE")
	if err != nil {
		return result, summary, err
	}
	summary.Showtimes = len(dataset.Showtimes)
	return dataset, summary, nil
}

// pendingShowing is a mapped session waiting for its film's final record.
type pendingShowing struct {
	showing schedule.ShowtimeRecord
	row     Row
}

// theaterRecord takes the cinema metadata of its first complete row.
func theaterRecord(cinema string, rows []Row) (schedule.TheaterRecord, bool) {
	for _, r := range rows {
		name, address, city, postal := Clean(r.Cinema), Clean(r.Address), Clean(r.City), PostalCode(r.PostalCode)
		if name == "" || city == "" || postal == "" {
			continue
		}
		id := string(schedule.ProviderScare) + "-" + cinema
		t := schedule.TheaterRecord{Provider: schedule.ProviderScare, ID: id, ProviderID: cinema, Slug: id, Name: name, Address: address, City: city, PostalCode: postal, AcceptedPasses: []string{}}
		if r.Latitude != nil && r.Longitude != nil && *r.Latitude >= -90 && *r.Latitude <= 90 && *r.Longitude >= -180 && *r.Longitude <= 180 {
			lat, lon := *r.Latitude, *r.Longitude
			t.Latitude, t.Longitude = &lat, &lon
		}
		return t, true
	}
	return schedule.TheaterRecord{}, false
}

func showtimeRecord(cinema, theaterID string, r Row, location *time.Location) (schedule.ShowtimeRecord, bool) {
	showing := ShowingID(cinema, r)
	title := Clean(r.Title)
	start, ok := ParseTime(r.Start, location)
	language, version, known := Attributes(r)
	if showing == "" || title == "" || !ok || !known {
		return schedule.ShowtimeRecord{}, false
	}
	service := start
	if service.Hour() < 3 {
		service = service.AddDate(0, 0, -1)
	}
	movieID := MovieID(cinema, r)
	overview := Clean(r.Storyline)
	if len(overview) > 10000 {
		overview = ""
	}
	movie := schedule.MovieRecord{Provider: schedule.ProviderScare, ProviderID: movieID, Slug: string(schedule.ProviderScare) + "-film-" + movieID, Title: title, RuntimeMinutes: RuntimeMinutes(r.Duration), Overview: overview, Genres: Genres(r.Genres)}
	return schedule.ShowtimeRecord{
		Provider: schedule.ProviderScare, ID: string(schedule.ProviderScare) + "-showing-" + showing, ProviderShowingID: showing,
		TheaterID: theaterID, ServiceDate: service.Format(time.DateOnly), Movie: movie, StartTime: start,
		Language: language, ProviderVersion: version, Format: schedule.Format2D, BookingURL: BookingURL(r.BookingURL),
	}, true
}

// endTime prefers the published end, then the runtime; unknown ends equal the
// start.
func endTime(start time.Time, r Row, runtime int, location *time.Location) time.Time {
	if end, ok := ParseTime(r.End, location); ok && !end.Before(start) && end.Sub(start) <= 12*time.Hour {
		return end
	}
	if runtime > 0 {
		return start.Add(time.Duration(runtime) * time.Minute)
	}
	return start
}
