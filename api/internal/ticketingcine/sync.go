package ticketingcine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"messeances/api/internal/parallel"
	"messeances/api/internal/schedule"
)

// ErrNoSources reports a synchronization without any configured site.
var ErrNoSources = errors.New("no TicketingCiné source is configured")

// Source is one configured TicketingCiné cinema.
type Source struct {
	SiteID string
}

type Getter interface {
	Program(context.Context, string) ([]byte, error)
	RequestCount() int
}

type SyncOptions struct {
	From    string
	Now     time.Time
	Sources []Source
}

type SyncSummary struct {
	Cinemas, Movies, Jobs, Requests, Showtimes int
	// MovieConflicts counts national films whose title or runtime differed
	// from the record kept for an earlier site.
	MovieConflicts int
	GeneratedAt    time.Time
}

// Sync publishes every configured site atomically: one invalid or failed
// programme fails the whole snapshot.
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
	sites := make([]string, len(options.Sources))
	seenSites := map[string]bool{}
	for i, source := range options.Sources {
		if !SiteID.MatchString(source.SiteID) || seenSites[source.SiteID] {
			return result, summary, fmt.Errorf("%w: source site", ErrShape)
		}
		seenSites[source.SiteID] = true
		sites[i] = source.SiteID
	}
	summary.Cinemas, summary.Jobs = len(sites), len(sites)
	programs, err := parallel.MapOrdered(ctx, sites, parallel.Options{Workers: 2}, func(ctx context.Context, site string) (Program, error) {
		body, err := getter.Program(ctx, site)
		if err != nil {
			return Program{}, err
		}
		p, err := parseSiteProgram(body, site)
		if err != nil {
			return Program{}, fmt.Errorf("site %s: %w", site, err)
		}
		return p, nil
	})
	if err != nil {
		return result, summary, err
	}

	builder := schedule.NewDatasetBuilder(schedule.ProviderTicketingcine, options.From, options.Now, schedule.BuilderOptions{})
	for i, p := range programs {
		for _, e := range p.Events {
			id := movieID(sites[i], e.ID)
			movie := schedule.MovieRecord{Provider: schedule.ProviderTicketingcine, ProviderID: id, Slug: string(schedule.ProviderTicketingcine) + "-film-" + id, Title: Clean(e.Title), RuntimeMinutes: int(e.Duration), PosterURL: PosterURL(e.BillURL, e.ID)}
			// Independent sites may retitle a national film or round its
			// runtime differently. Sources are ordered by site, so the first
			// site's record deterministically wins instead of failing the
			// snapshot; the other sites only fill missing runtime or poster.
			if !builder.MergeMovie(movie) {
				summary.MovieConflicts++
			}
		}
	}
	summary.Movies = builder.MovieCount()
	seenSessions := map[string]bool{}
	for i, p := range programs {
		if err := ctx.Err(); err != nil {
			return result, summary, err
		}
		site := sites[i]
		theaterID := string(schedule.ProviderTicketingcine) + "-" + site
		for _, e := range p.Events {
			m := builder.Movie(movieID(site, e.ID))
			for _, s := range e.Sessions {
				// Duplicates are rejected before date filtering, past sessions included.
				if seenSessions[s.ID] {
					return result, summary, fmt.Errorf("%w: duplicate TicketingCiné session", schedule.ErrDatasetValidation)
				}
				seenSessions[s.ID] = true
				start, ok := ParseStart(s.Date, location)
				if !ok {
					return result, summary, fmt.Errorf("%w: session start", ErrShape)
				}
				service := start
				if service.Hour() < 3 {
					service = service.AddDate(0, 0, -1)
				}
				date := service.Format(time.DateOnly)
				if date < options.From {
					continue
				}
				end, ok := schedule.MegaramaEnd(start, m.RuntimeMinutes, int(s.FirstPartDuration))
				if !ok {
					return result, summary, fmt.Errorf("%w: session end", ErrShape)
				}
				language, format, version, err := Attributes(s)
				if err != nil {
					return result, summary, err
				}
				builder.ExtendWindow(start.Format(time.DateOnly))
				showing := schedule.ShowtimeRecord{
					Provider: schedule.ProviderTicketingcine, ID: string(schedule.ProviderTicketingcine) + "-showing-" + s.ID, ProviderShowingID: s.ID,
					TheaterID: theaterID, ServiceDate: date, Movie: m, StartTime: start, EndTime: end,
					FirstPartDurationMinutes: int(s.FirstPartDuration), Language: language, ProviderVersion: version, Format: format,
					Room: Clean(s.HallName), BookingURL: bookingURL(s.BookingURL, site, s.ID, m.ProviderID),
				}
				if err := builder.AddShowtime(showing); err != nil {
					return result, summary, fmt.Errorf("%w: duplicate TicketingCiné session", schedule.ErrDatasetValidation)
				}
			}
		}
		builder.AddTheater(schedule.TheaterRecord{Provider: schedule.ProviderTicketingcine, ID: theaterID, ProviderID: site, Slug: theaterID, Name: Clean(p.Name), Address: Clean(p.Address), City: Clean(p.City), PostalCode: Clean(p.ZipCode), AcceptedPasses: []string{}})
	}
	dataset, err := builder.Build("TicketingCiné")
	if err != nil {
		return result, summary, err
	}
	summary.Showtimes = len(dataset.Showtimes)
	return dataset, summary, nil
}

// parseSiteProgram validates one site's programme identities and metadata.
func parseSiteProgram(body []byte, site string) (Program, error) {
	p, err := DecodeProgram(body)
	if err != nil {
		return Program{}, err
	}
	if p.Events == nil || p.EMS.ID != "" && p.EMS.ID != SiteNumber(site) {
		return Program{}, fmt.Errorf("%w: programme identity", ErrShape)
	}
	if Clean(p.Name) == "" || Clean(p.Address) == "" || Clean(p.City) == "" || Clean(p.ZipCode) == "" {
		return Program{}, fmt.Errorf("%w: cinema name or address", ErrShape)
	}
	seen := map[string]bool{}
	for _, e := range p.Events {
		id := movieID(site, e.ID)
		if !schedule.ValidTicketingcineIdentity("movie", id) || seen[id] || Clean(e.Title) == "" || e.Sessions == nil {
			return Program{}, fmt.Errorf("%w: event metadata", ErrShape)
		}
		seen[id] = true
		for _, s := range e.Sessions {
			if !SessionID.MatchString(s.ID) || !strings.HasPrefix(s.ID, "emsx"+SiteNumber(site)) || !DateID.MatchString(s.Date) {
				return Program{}, fmt.Errorf("%w: session metadata", ErrShape)
			}
			if _, _, _, err := Attributes(s); err != nil {
				return Program{}, err
			}
		}
	}
	return p, nil
}

// movieID keeps national films global and scopes local events to their site.
func movieID(site, id string) string {
	if GlobalMovieID.MatchString(id) {
		return id
	}
	if LocalMovieID.MatchString(id) && strings.HasPrefix(id, "emsx"+SiteNumber(site)) {
		return site + "-" + id
	}
	return ""
}

// bookingURL keeps a source session route bound to this site, session and
// film; anything else falls back to the site's session route.
func bookingURL(raw, site, session, movie string) string {
	if raw != "" && schedule.ValidTicketingcineBookingURL(raw, site, session) && schedule.TicketingcineBookingMatchesMovie(raw, site, movie) {
		return raw
	}
	return schedule.TicketingcineBookingURL(site, session)
}

// Attributes maps a session's version and features to stored language,
// format and provider version. Unknown feature tokens are ignored.
func Attributes(s Session) (schedule.Language, schedule.Format, string, error) {
	version := Clean(s.Version)
	if version != "VF" && version != "VO" && version != "VOF" && version != "VOST" {
		return "", "", "", fmt.Errorf("%w: session version", ErrShape)
	}
	features := map[string]bool{}
	for _, f := range s.Features {
		features[f] = true
	}
	for _, f := range strings.Split(s.Formats, ",") {
		features[strings.TrimSpace(f)] = true
	}
	language := schedule.Language(version)
	switch {
	case version == "VOST":
		language = schedule.LanguageVOSTFR
	case version == "VOF":
		language = schedule.LanguageVO
	case features["subtitle"] || features["ST"]:
		if version == "VF" {
			language, version = schedule.LanguageVFSTF, "VFSTF"
		} else {
			language, version = schedule.LanguageVOSTFR, "VOSTF"
		}
	}
	format := schedule.Format2D
	switch {
	case features["video_imax"]:
		format = schedule.FormatIMAX
	case features["video_motion"]:
		format = schedule.Format4DX
	case features["video_3d"] || features["3D"]:
		format = schedule.Format3D
	case features["video_immersif"] || strings.Contains(strings.ToUpper(s.HallName), "ICE"):
		format = schedule.FormatICE
	case features["audio_atmos"] || features["ATMOS"]:
		format = schedule.FormatDolby
	}
	return language, format, version, nil
}
