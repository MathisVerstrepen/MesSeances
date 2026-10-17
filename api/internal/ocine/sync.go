package ocine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"messeances/api/internal/parallel"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type Getter interface {
	Page(context.Context, string, string) ([]byte, error)
	Checkout(context.Context, string) ([]byte, error)
	RequestCount() int
}
type SyncOptions struct {
	From string
	Now  time.Time
	// Clock observes checkout failures independently of the fixed programme date.
	// It defaults to time.Now and must be safe for concurrent use.
	Clock func() time.Time
}
type SyncSummary struct {
	Cinemas, Movies, Requests, Showtimes       int
	GeneratedAt                                time.Time
	EndsAttempted, EndsRecovered, RoomsMissing int
}

// Sync publishes nothing until the complete fixed census and every unique
// checkout has succeeded or closed near session start. Observed dates never
// attest full activity coverage.
func Sync(ctx context.Context, g Getter, o SyncOptions) (result schedule.Dataset, summary SyncSummary, resultErr error) {
	defer func() { summary.Requests = g.RequestCount() }()
	summary.GeneratedAt = o.Now.UTC()
	clock := o.Clock
	if clock == nil {
		clock = time.Now
	}
	loc, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return result, summary, err
	}
	from, err := time.ParseInLocation(time.DateOnly, o.From, loc)
	if err != nil || from.Format(time.DateOnly) != o.From || o.Now.IsZero() || o.From != o.Now.In(loc).Format(time.DateOnly) {
		return result, summary, fmt.Errorf("invalid Ocine sync date")
	}
	programmes, err := parallel.MapOrdered(ctx, cinemaIDs[:], parallel.Options{Workers: 2}, func(ctx context.Context, id string) (programme, error) {
		body, err := g.Page(ctx, id, "seances")
		if err != nil {
			return programme{}, err
		}
		p, err := parseProgramme(body, id, o.From, o.Now, loc)
		if err != nil {
			return programme{}, err
		}
		body, err = g.Page(ctx, id, "plan-d-acces")
		if err != nil {
			return programme{}, err
		}
		p.theater.Address, p.theater.City, p.theater.PostalCode, err = parseAddress(body, id)
		return p, err
	})
	if err != nil {
		return result, summary, err
	}
	summary.Cinemas = len(programmes)
	movies := map[string]schedule.MovieRecord{}
	for _, p := range programmes {
		for _, m := range p.movies {
			if prior, ok := movies[m.ProviderID]; ok {
				if prior.Title != m.Title || prior.RuntimeMinutes != 0 && m.RuntimeMinutes != 0 && prior.RuntimeMinutes != m.RuntimeMinutes || prior.PosterURL != "" && m.PosterURL != "" && prior.PosterURL != m.PosterURL {
					return result, summary, fmt.Errorf("%w: conflicting movie metadata", errShape)
				}
				if prior.RuntimeMinutes == 0 {
					prior.RuntimeMinutes = m.RuntimeMinutes
				}
				if prior.PosterURL == "" {
					prior.PosterURL = m.PosterURL
				}
				m = prior
			}
			movies[m.ProviderID] = m
		}
	}
	summary.Movies = len(movies)
	d := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderOcine, Scope: schedule.ScopeAll, GeneratedAt: o.Now.UTC(), Timezone: schedule.Timezone, Window: schedule.Window{From: o.From, Through: o.From}, Theaters: []schedule.TheaterRecord{}, Showtimes: []schedule.ShowtimeRecord{}}
	seen := map[string]schedule.ShowtimeRecord{}
	bookingOwners := map[string]string{}
	for _, p := range programmes {
		if p.through > d.Window.Through {
			d.Window.Through = p.through
		}
		dates := map[string]bool{}
		for _, s := range p.showtimes {
			s.Movie = movies[s.Movie.ProviderID]
			if prior, ok := seen[s.ProviderShowingID]; ok {
				if !reflect.DeepEqual(prior, s) {
					return result, summary, fmt.Errorf("%w: conflicting duplicate showing", errShape)
				}
				continue
			}
			if s.BookingURL != "" {
				if owner, ok := bookingOwners[s.BookingURL]; ok && owner != s.ProviderShowingID {
					return result, summary, fmt.Errorf("%w: booking assigned to different films", errShape)
				}
				bookingOwners[s.BookingURL] = s.ProviderShowingID
			}
			seen[s.ProviderShowingID] = s
			dates[s.ServiceDate] = true
			d.Showtimes = append(d.Showtimes, s)
		}
		for date := range dates {
			p.theater.AvailableDates = append(p.theater.AvailableDates, date)
		}
		sort.Strings(p.theater.AvailableDates)
		d.Theaters = append(d.Theaters, p.theater)
	}
	indices := []int{}
	for i, s := range d.Showtimes {
		if s.BookingURL != "" {
			indices = append(indices, i)
		}
	}
	summary.EndsAttempted = len(indices)
	type checkout struct {
		end  time.Time
		room string
	}
	checkouts, err := parallel.MapOrdered(ctx, indices, parallel.Options{Workers: 2}, func(ctx context.Context, i int) (checkout, error) {
		s := d.Showtimes[i]
		body, err := g.Checkout(ctx, s.BookingURL)
		if err != nil {
			observedAt := clock()
			if ctx.Err() != nil {
				return checkout{}, ctx.Err()
			}
			var requestErr *RequestError
			if errors.As(err, &requestErr) && requestErr.Kind == syncproxy.FailureStatus && requestErr.StatusCode == http.StatusNotFound && !s.StartTime.After(observedAt.Add(2*time.Minute)) {
				// Booking closes shortly before start; the grid can retain its anchor. Keep its
				// identity, URL and grid room, without inventing checkout metadata.
				return checkout{end: s.StartTime}, nil
			}
			return checkout{}, err
		}
		end, room, err := parseCheckout(body, s, strings.TrimPrefix(s.TheaterID, "ocine-"), loc)
		return checkout{end, room}, err
	})
	if err != nil {
		return result, summary, err
	}
	for i, c := range checkouts {
		s := &d.Showtimes[indices[i]]
		s.EndTime = c.end
		if c.end.After(s.StartTime) {
			summary.EndsRecovered++
		}
		if s.Room == "" {
			s.Room = c.room
		}
	}
	for _, s := range d.Showtimes {
		if s.Room == "" {
			summary.RoomsMissing++
		}
	}
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
	if err := ctx.Err(); err != nil {
		return result, summary, err
	}
	if err := schedule.ValidateDataset(d, true); err != nil {
		return result, summary, fmt.Errorf("%w: invalid synchronized Ocine dataset", schedule.ErrDatasetValidation)
	}
	summary.Showtimes = len(d.Showtimes)
	return d, summary, nil
}
