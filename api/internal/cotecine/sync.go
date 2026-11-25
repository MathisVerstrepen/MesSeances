// Package cotecine synchronizes independent cinemas whose Cinegroup site
// (sold through Cotecine) publishes its whole programme on one "seances/"
// page. Cinemas are configured as sync sources: the Cinegroup theater id, the
// site base URL and the cinema address, which the page does not reliably show.
package cotecine

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"messeances/api/internal/parallel"
	"messeances/api/internal/schedule"
)

const workers = 4

// ErrNoSources reports a synchronization without any configured cinema.
var ErrNoSources = errors.New("no Cotecine source is configured")

var postalCode = regexp.MustCompile(`^(?:[0-9]{5}|2[AB][0-9]{3})$`)

// Source is one configured cinema.
type Source struct {
	TheaterID string
	// Website is the site base URL; the programme is Website + "seances/".
	Website                   string
	Address, PostalCode, City string
}

type Getter interface {
	Programme(context.Context, string) ([]byte, error)
	RequestCount() int
}

type SyncOptions struct {
	From    string
	Now     time.Time
	Sources []Source
}

type SyncSummary struct {
	Cinemas, Movies, Jobs, Requests, Showtimes int
	// Skipped counts sessions without a usable version or whose booking
	// route contradicts the displayed time, plus repeated sessions.
	Skipped int
	// MovieConflicts counts films whose title or runtime differed from the
	// record kept for an earlier site.
	MovieConflicts int
	GeneratedAt    time.Time
}

func validSource(s Source) bool {
	return schedule.ValidCotecineIdentity("theater", s.TheaterID) && schedule.ValidCotecineWebsite(s.Website) &&
		clean(s.Address) != "" && clean(s.City) != "" && postalCode.MatchString(s.PostalCode)
}

// Sync publishes every configured cinema atomically: a transport failure or
// an unrecognized page fails the snapshot.
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
	for i, s := range options.Sources {
		if !validSource(s) || slices.ContainsFunc(options.Sources[:i], func(o Source) bool { return o.TheaterID == s.TheaterID }) {
			return result, summary, fmt.Errorf("%w: source", ErrShape)
		}
	}
	summary.Jobs = len(options.Sources)
	programmes, err := parallel.MapOrdered(ctx, options.Sources, parallel.Options{Workers: min(workers, len(options.Sources))}, func(ctx context.Context, s Source) (programme, error) {
		p, err := fetchProgramme(ctx, getter, s, options.From, options.Now, location)
		if err != nil {
			return programme{}, fmt.Errorf("cinema %s: %w", s.TheaterID, err)
		}
		return p, nil
	})
	if err != nil {
		return result, summary, err
	}

	builder := schedule.NewDatasetBuilder(schedule.ProviderCotecine, options.From, options.Now, schedule.BuilderOptions{})
	// Merge every site's films first so sessions bind to the final record.
	for _, p := range programmes {
		for _, m := range p.movies {
			if !builder.MergeMovie(m) {
				summary.MovieConflicts++
			}
		}
	}
	for i, p := range programmes {
		if err := ctx.Err(); err != nil {
			return result, summary, err
		}
		s := options.Sources[i]
		theaterID := string(schedule.ProviderCotecine) + "-" + s.TheaterID
		builder.AddTheater(schedule.TheaterRecord{Provider: schedule.ProviderCotecine, ID: theaterID, ProviderID: s.TheaterID, Slug: theaterID, Name: p.name, Address: clean(s.Address), City: clean(s.City), PostalCode: s.PostalCode, AcceptedPasses: []string{}})
		summary.Cinemas++
		summary.Skipped += p.skipped
		builder.ExtendWindow(p.through)
		for _, x := range p.sessions {
			m := builder.Movie(x.movie)
			end := x.start
			if m.RuntimeMinutes > 0 {
				end = x.start.Add(time.Duration(m.RuntimeMinutes) * time.Minute)
			}
			id := schedule.CotecineShowingID(s.TheaterID, m.ProviderID, x.start)
			showing := schedule.ShowtimeRecord{
				Provider: schedule.ProviderCotecine, ID: string(schedule.ProviderCotecine) + "-showing-" + id, ProviderShowingID: id,
				TheaterID: theaterID, ServiceDate: x.date, Movie: m, StartTime: x.start, EndTime: end,
				Language: x.language, ProviderVersion: x.version, Format: x.format, BookingURL: x.booking,
			}
			if err := builder.AddShowtime(showing); err != nil {
				// The same film at the same instant in two auditoriums.
				summary.Skipped++
			}
		}
	}
	summary.Movies = builder.MovieCount()
	dataset, err := builder.Build("Cotecine")
	if err != nil {
		return result, summary, err
	}
	summary.Showtimes = len(dataset.Showtimes)
	return dataset, summary, nil
}

// fetchProgramme reads one site, fetching an empty programme a second time:
// an empty page may be transient and would clear the cinema's sessions.
func fetchProgramme(ctx context.Context, getter Getter, s Source, from string, now time.Time, location *time.Location) (programme, error) {
	body, err := getter.Programme(ctx, s.Website)
	if err != nil {
		return programme{}, err
	}
	p, err := parseProgramme(body, s, from, now, location)
	if !errors.Is(err, errEmptyProgramme) {
		return p, err
	}
	if body, err = getter.Programme(ctx, s.Website); err != nil {
		return programme{}, err
	}
	p, err = parseProgramme(body, s, from, now, location)
	if errors.Is(err, errEmptyProgramme) {
		// Confirmed empty: the cinema publishes no session.
		return p, nil
	}
	return p, err
}
