// Package webedia synchronizes independent cinemas whose Webedia Movies Pro
// site exposes the Boxoffice API. Cinemas are configured as sync sources: a
// Boxoffice theater id and the site origin.
package webedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"messeances/api/internal/boxoffice"
	"messeances/api/internal/parallel"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

const (
	movieBatchSize = 50
	workers        = 4
	// maxWindowDays bounds the requested schedule window.
	maxWindowDays = 62
)

var (
	// ErrShape reports a response that does not match the expected model.
	ErrShape = errors.New("invalid Webedia response structure")
	// ErrNoSources reports a synchronization without any configured cinema.
	ErrNoSources = errors.New("no Webedia source is configured")
)

var languageTag = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,15}$`)

// Source is one configured cinema: its Boxoffice theater id and site origin
// (https://host/).
type Source struct {
	TheaterID string
	Origin    string
}

type Getter interface {
	Get(context.Context, syncproxy.Operation, string) ([]byte, error)
	RequestCount() int
}

type SyncOptions struct {
	From    string
	Now     time.Time
	Sources []Source
}

type SyncSummary struct {
	Cinemas, Movies, Jobs, Requests, Showtimes int
	// Skipped counts sessions without a known film, language or start.
	Skipped        int
	MovieConflicts int
	GeneratedAt    time.Time
}

type theater struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	TimeZone      string `json:"timeZone"`
	PracticalInfo struct {
		Closed      bool `json:"closed"`
		Coordinates *struct {
			Latitude  *float64 `json:"latitude"`
			Longitude *float64 `json:"longitude"`
		} `json:"coordinates"`
		Location struct{ Address, City, Zip string } `json:"location"`
	} `json:"practicalInfo"`
}

type movie struct {
	ID          boxoffice.SourceID `json:"id"`
	Title       string             `json:"title"`
	Runtime     *int               `json:"runtime"`
	Poster      string             `json:"poster"`
	Genres      json.RawMessage    `json:"genres"`
	Synopsis    string             `json:"synopsis"`
	ReleaseDate *string            `json:"releaseDate"`
}

// site is everything fetched for one source.
type site struct {
	source   Source
	theater  theater
	program  map[string][]string
	movies   map[string]movie
	sessions map[string]map[string][]boxoffice.Showtime
}

// Sync publishes every configured cinema atomically: a transport failure or
// malformed response fails the snapshot. Sessions that cannot be mapped are
// skipped and counted.
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
		if !schedule.ValidWebediaIdentity("theater", s.TheaterID) || !schedule.ValidWebediaOrigin(s.Origin) || slices.ContainsFunc(options.Sources[:i], func(o Source) bool { return o.TheaterID == s.TheaterID }) {
			return result, summary, fmt.Errorf("%w: source", ErrShape)
		}
	}
	summary.Jobs = len(options.Sources)
	through := time.Date(0, 1, 1, 0, 0, 0, 0, location)
	if from, err := time.ParseInLocation(time.DateOnly, options.From, location); err == nil {
		through = from.AddDate(0, 0, maxWindowDays)
	}
	sites, err := parallel.MapOrdered(ctx, options.Sources, parallel.Options{Workers: min(workers, len(options.Sources))}, func(ctx context.Context, s Source) (site, error) {
		result, err := fetchSite(ctx, getter, s, options.From, through.Format(time.DateOnly), location)
		if err != nil {
			return site{}, fmt.Errorf("site %s: %w", s.TheaterID, err)
		}
		return result, nil
	})
	if err != nil {
		return result, summary, err
	}

	builder := schedule.NewDatasetBuilder(schedule.ProviderWebedia, options.From, options.Now, schedule.BuilderOptions{})
	for _, st := range sites {
		theaterID := string(schedule.ProviderWebedia) + "-" + st.source.TheaterID
		l := st.theater.PracticalInfo.Location
		record := schedule.TheaterRecord{Provider: schedule.ProviderWebedia, ID: theaterID, ProviderID: st.source.TheaterID, Slug: theaterID, Name: clean(st.theater.Name), Address: clean(l.Address), City: clean(l.City), PostalCode: clean(l.Zip), AcceptedPasses: []string{}}
		if c := st.theater.PracticalInfo.Coordinates; c != nil && c.Latitude != nil && c.Longitude != nil && *c.Latitude >= -90 && *c.Latitude <= 90 && *c.Longitude >= -180 && *c.Longitude <= 180 && (*c.Latitude != 0 || *c.Longitude != 0) {
			lat, lon := *c.Latitude, *c.Longitude
			record.Latitude, record.Longitude = &lat, &lon
		}
		builder.AddTheater(record)
		summary.Cinemas++
		ids := make([]string, 0, len(st.sessions))
		for id := range st.sessions {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, sourceMovie := range ids {
			m, ok := movieRecord(st, sourceMovie)
			if !ok {
				for _, sessions := range st.sessions[sourceMovie] {
					summary.Skipped += len(sessions)
				}
				continue
			}
			if !builder.MergeMovie(m) {
				summary.MovieConflicts++
			}
			m = builder.Movie(m.ProviderID)
			dates := make([]string, 0, len(st.sessions[sourceMovie]))
			for date := range st.sessions[sourceMovie] {
				dates = append(dates, date)
			}
			sort.Strings(dates)
			for _, date := range dates {
				for _, s := range st.sessions[sourceMovie][date] {
					showing, ok := showtimeRecord(st.source.TheaterID, theaterID, m, date, s, location)
					if !ok {
						summary.Skipped++
						continue
					}
					if showing.ServiceDate < options.From {
						continue
					}
					if err := builder.AddShowtime(showing); err != nil {
						// Identical session text repeated under several tags.
						summary.Skipped++
						continue
					}
					builder.ExtendWindow(showing.StartTime.Format(time.DateOnly))
				}
			}
		}
	}
	summary.Movies = builder.MovieCount()
	dataset, err := builder.Build("Webedia")
	if err != nil {
		return result, summary, err
	}
	summary.Showtimes = len(dataset.Showtimes)
	return dataset, summary, nil
}

func fetchSite(ctx context.Context, g Getter, s Source, from, through string, location *time.Location) (site, error) {
	origin := strings.TrimSuffix(s.Origin, "/")
	result := site{source: s}
	body, err := g.Get(ctx, OperationCinemas, origin+boxoffice.CatalogPath)
	if err != nil {
		return result, err
	}
	var catalog struct {
		Data struct{ AllTheater struct{ Nodes []theater } }
	}
	if boxoffice.DecodeJSON(body, &catalog) != nil {
		return result, fmt.Errorf("%w: catalog", ErrShape)
	}
	found := false
	for _, t := range catalog.Data.AllTheater.Nodes {
		if t.ID == s.TheaterID {
			result.theater, found = t, true
		}
	}
	l := result.theater.PracticalInfo.Location
	if !found || result.theater.TimeZone != schedule.Timezone || clean(result.theater.Name) == "" || clean(l.Address) == "" || clean(l.City) == "" || clean(l.Zip) == "" {
		return result, fmt.Errorf("%w: theater missing from catalog", ErrShape)
	}

	body, err = g.Get(ctx, OperationProgram, boxoffice.ProgramURL(origin, s.TheaterID))
	if err != nil {
		return result, err
	}
	if result.program, err = boxoffice.ParseProgram(body, from, location); err != nil {
		return result, err
	}
	ids := make([]string, 0, len(result.program))
	last := ""
	for id, dates := range result.program {
		if len(dates) > 0 {
			ids = append(ids, id)
			last = max(last, dates[len(dates)-1])
		}
	}
	sort.Strings(ids)
	result.movies = map[string]movie{}
	result.sessions = map[string]map[string][]boxoffice.Showtime{}
	if len(ids) == 0 {
		return result, nil
	}
	for start := 0; start < len(ids); start += movieBatchSize {
		batch := ids[start:min(start+movieBatchSize, len(ids))]
		body, err := g.Get(ctx, OperationMovies, boxoffice.MoviesURL(origin, batch))
		if err != nil {
			return result, err
		}
		var movies []movie
		if boxoffice.DecodeJSON(body, &movies) != nil || movies == nil {
			return result, fmt.Errorf("%w: movies", ErrShape)
		}
		for _, m := range movies {
			result.movies[string(m.ID)] = m
		}
	}
	end := min(last, through)
	body, err = g.Get(ctx, OperationSchedule, boxoffice.ScheduleURL(origin, s.TheaterID, from, end))
	if err != nil {
		return result, err
	}
	var sessions boxoffice.Schedule
	if boxoffice.DecodeJSON(body, &sessions) != nil {
		return result, fmt.Errorf("%w: schedule", ErrShape)
	}
	container, ok := sessions[s.TheaterID]
	if !ok || len(sessions) != 1 || container.Schedule == nil {
		return result, fmt.Errorf("%w: schedule", ErrShape)
	}
	result.sessions = container.Schedule
	return result, nil
}

// movieRecord maps a Boxoffice film; site-local "c" films are scoped by
// theater.
func movieRecord(st site, sourceID string) (schedule.MovieRecord, bool) {
	m, ok := st.movies[sourceID]
	title := clean(m.Title)
	if !ok || title == "" {
		return schedule.MovieRecord{}, false
	}
	id := sourceID
	if strings.HasPrefix(id, "c") {
		id = st.source.TheaterID + "-" + id
	}
	if !schedule.ValidWebediaIdentity("movie", id) {
		return schedule.MovieRecord{}, false
	}
	runtime := 0
	if m.Runtime != nil && *m.Runtime > 0 {
		if minutes := *m.Runtime / 60; minutes > 0 {
			if _, valid := schedule.RuntimeDuration(minutes); valid {
				runtime = minutes
			}
		}
	}
	poster := ""
	if schedule.ValidWebediaPosterURL(m.Poster) {
		poster = m.Poster
	}
	release := ""
	if m.ReleaseDate != nil && len(*m.ReleaseDate) >= 10 {
		if d, err := time.Parse(time.DateOnly, (*m.ReleaseDate)[:10]); err == nil {
			release = d.Format(time.DateOnly)
		}
	}
	overview := clean(m.Synopsis)
	if len(overview) > 10000 {
		overview = ""
	}
	return schedule.MovieRecord{Provider: schedule.ProviderWebedia, ProviderID: id, Slug: string(schedule.ProviderWebedia) + "-film-" + id, Title: title, RuntimeMinutes: runtime, PosterURL: poster, Overview: overview, ReleaseDate: release, Genres: genres(m.Genres)}, true
}

func showtimeRecord(theaterSourceID, theaterID string, m schedule.MovieRecord, date string, s boxoffice.Showtime, location *time.Location) (schedule.ShowtimeRecord, bool) {
	if strings.TrimSpace(s.ID) == "" || len(s.ID) > 4096 || strings.ContainsRune(s.ID, 0) {
		return schedule.ShowtimeRecord{}, false
	}
	start, err := boxoffice.ParseStart(s.StartsAt, location)
	if err != nil || boxoffice.ServiceDate(start) != date {
		return schedule.ShowtimeRecord{}, false
	}
	language, version, ok := normalizeVersion(s.Tags)
	if !ok {
		return schedule.ShowtimeRecord{}, false
	}
	booking := ""
	for _, entry := range s.Data.Ticketing {
		if entry.Provider == "default" && entry.Type == "DESKTOP" && len(entry.URLs) == 1 && schedule.ValidPublicHTTPSURL(entry.URLs[0]) {
			booking = entry.URLs[0]
			break
		}
	}
	room := ""
	if s.Screen != nil {
		room = clean(s.Screen.Name)
	}
	end := start
	if m.RuntimeMinutes > 0 {
		end = start.Add(time.Duration(m.RuntimeMinutes) * time.Minute)
	}
	// Exact source session text is theater-scoped; mutable metadata is excluded.
	sum := sha256.Sum256([]byte(theaterSourceID + "\x00" + s.ID))
	id := theaterSourceID + "-" + hex.EncodeToString(sum[:16])
	return schedule.ShowtimeRecord{
		Provider: schedule.ProviderWebedia, ID: string(schedule.ProviderWebedia) + "-showing-" + id, ProviderShowingID: id,
		TheaterID: theaterID, ServiceDate: date, Movie: m, StartTime: start, EndTime: end,
		Language: language, ProviderVersion: version, Format: boxoffice.NormalizeFormat(s.Tags), Room: room, BookingURL: booking,
	}, true
}

// normalizeVersion maps Boxoffice localization tags like the Noé Cinémas
// connector does, without failing on unknown tags.
func normalizeVersion(tags []string) (schedule.Language, string, bool) {
	available := map[string]bool{}
	for _, tag := range tags {
		available[strings.TrimSpace(tag)] = true
	}
	for _, tag := range tags {
		if value, ok := strings.CutPrefix(tag, "Localization.Language."); ok && strings.EqualFold(value, "VOF") {
			if available["Showtime.Accessibility.OpenCaption"] {
				return schedule.LanguageVOSTFR, tag, true
			}
			return schedule.LanguageVO, tag, true
		}
	}
	switch {
	case available["Localization.Version.Original"]:
		return schedule.LanguageVOSTFR, "VOSTFR", true
	case available["Localization.Language.French"] && available["Showtime.Accessibility.OpenCaption"]:
		return schedule.LanguageVFSTF, "VFSTF", true
	case available["Localization.Language.French"] || available["Showtime.Accessibility.Dubbed"]:
		return schedule.LanguageVF, "VF", true
	}
	for _, tag := range tags {
		if value, ok := strings.CutPrefix(tag, "Localization.Language."); ok {
			value = strings.ToUpper(value)
			if value != "ALL" && languageTag.MatchString(value) {
				return schedule.Language(value), value, true
			}
		}
	}
	return "", "", false
}

func genres(raw json.RawMessage) []string {
	var list []string
	var text string
	if json.Unmarshal(raw, &text) == nil {
		list = strings.Split(text, ",")
	} else if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	var result []string
	for _, g := range list {
		if g = clean(g); g != "" && !slices.Contains(result, g) && len(result) < 8 {
			result = append(result, g)
		}
	}
	return result
}

func clean(value string) string { return strings.Join(strings.Fields(value), " ") }
