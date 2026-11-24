package ocine

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"messeances/api/internal/boxoffice"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type boxofficeSite struct{ origin, sourceID string }

func ocineBoxofficeSite(id string) boxofficeSite {
	switch id {
	case "309":
		return boxofficeSite{"https://maubeuge.ocine.fr", "P2879"}
	case "310":
		return boxofficeSite{"https://saint-omer.ocine.fr", "P0911"}
	case "461":
		return boxofficeSite{"https://dunkerque.ocine.fr", "P0795"}
	default:
		return boxofficeSite{}
	}
}

type boxofficeGetter interface {
	GetBoxoffice(context.Context, string) ([]byte, error)
	CheckoutBoxoffice(context.Context, schedule.ShowtimeRecord, string) ([]byte, string, error)
}

func (c *Client) GetBoxoffice(ctx context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || !slices.ContainsFunc(cinemaIDs[:], func(id string) bool { return boxofficeURL(u, id) }) {
		return nil, syncproxy.InvalidRequest(sourceName, operationPage)
	}
	return c.fetch(ctx, raw, nil, false)
}

func boxofficeURL(u *url.URL, theater string) bool {
	if u == nil || len(u.String()) > 8192 || u.Scheme != "https" || u.User != nil || u.Opaque != "" ||
		u.RawPath != "" || u.Fragment != "" || u.ForceQuery {
		return false
	}
	site := ocineBoxofficeSite(theater)
	if "https://"+u.Host != site.origin {
		return false
	}
	raw := u.String()
	switch u.Path {
	case boxoffice.CatalogPath:
		return raw == site.origin+boxoffice.CatalogPath
	case "/api/gatsby-source-boxofficeapi/scheduledMovies":
		return raw == boxoffice.ProgramURL(site.origin, site.sourceID)
	case "/api/gatsby-source-boxofficeapi/movies":
		ids := u.Query()["ids"]
		return len(ids) > 0 && len(ids) <= 50 && raw == boxoffice.MoviesURL(site.origin, ids) &&
			!slices.ContainsFunc(ids, func(id string) bool { return !schedule.ValidGrandEcranIdentity("movie", id) })
	case "/api/gatsby-source-boxofficeapi/schedule":
		q := u.Query()
		from, fromOK := strings.CutSuffix(q.Get("from"), "T03:00:00")
		to, err := time.Parse("2006-01-02T15:04:05", q.Get("to"))
		if _, dateErr := time.Parse(time.DateOnly, from); !fromOK || dateErr != nil || err != nil {
			return false
		}
		through := to.AddDate(0, 0, -1).Format(time.DateOnly)
		return through >= from && raw == boxoffice.ScheduleURL(site.origin, site.sourceID, from, through)
	default:
		return false
	}
}

func loadBoxofficeProgramme(ctx context.Context, g boxofficeGetter, id, from string, loc *time.Location) (programme, error) {
	site := ocineBoxofficeSite(id)
	result := programme{through: from}
	body, err := g.GetBoxoffice(ctx, site.origin+boxoffice.CatalogPath)
	if err != nil {
		return result, err
	}
	theater, err := parseBoxofficeTheater(body, id, site.sourceID)
	if err != nil {
		return result, err
	}
	result.theater = theater
	body, err = g.GetBoxoffice(ctx, boxoffice.ProgramURL(site.origin, site.sourceID))
	if err != nil {
		return result, err
	}
	date, _ := time.ParseInLocation(time.DateOnly, from, loc)
	// The preceding Boxoffice service day may contain this calendar day's first
	// three hours. Its already-removed screenings do not establish a gap.
	first := date.AddDate(0, 0, -1).Format(time.DateOnly)
	index, err := boxoffice.ParseProgram(body, first, loc)
	if err != nil {
		return result, err
	}
	ids, through := boxofficeMovieIDs(index)
	if len(ids) == 0 {
		return result, nil
	}
	movies := map[string]schedule.MovieRecord{}
	for start := 0; start < len(ids); {
		end := start + 1
		for end < len(ids) && end-start < 50 && len(boxoffice.MoviesURL(site.origin, ids[start:end+1])) <= 8192 {
			end++
		}
		body, err := g.GetBoxoffice(ctx, boxoffice.MoviesURL(site.origin, ids[start:end]))
		if err != nil {
			return result, err
		}
		if err := addBoxofficeMovies(body, ids[start:end], movies); err != nil {
			return result, err
		}
		start = end
	}
	body, err = g.GetBoxoffice(ctx, boxoffice.ScheduleURL(site.origin, site.sourceID, first, through))
	if err != nil {
		return result, err
	}
	result.showtimes, err = parseBoxofficeSchedule(body, id, site.sourceID, first, from, through, index, movies, loc)
	if err != nil {
		return result, err
	}
	used := map[string]bool{}
	for _, row := range result.showtimes {
		if !used[row.Movie.ProviderID] {
			used[row.Movie.ProviderID] = true
			result.movies = append(result.movies, row.Movie)
		}
		result.through = max(result.through, row.ServiceDate)
	}
	result.through = max(result.through, through)
	return result, nil
}

func parseBoxofficeTheater(body []byte, id, sourceID string) (schedule.TheaterRecord, error) {
	var response struct {
		Data struct {
			AllTheater struct {
				Nodes []struct {
					ID, Name, TimeZone string
					PracticalInfo      struct {
						Location struct{ Address, City, Zip string }
					}
				}
			}
		}
	}
	if boxoffice.DecodeJSON(body, &response) != nil || len(response.Data.AllTheater.Nodes) != 1 {
		return schedule.TheaterRecord{}, fmt.Errorf("%w: cinema catalog", errShape)
	}
	c := response.Data.AllTheater.Nodes[0]
	address := c.PracticalInfo.Location
	if c.ID != sourceID || c.TimeZone != schedule.Timezone || strings.TrimSpace(c.Name) == "" ||
		strings.TrimSpace(address.Address) == "" || strings.TrimSpace(address.City) == "" || strings.TrimSpace(address.Zip) == "" {
		return schedule.TheaterRecord{}, fmt.Errorf("%w: cinema metadata", errShape)
	}
	key := "ocine-" + id
	return schedule.TheaterRecord{Provider: schedule.ProviderOcine, ProviderID: id, ID: key, Slug: key,
		Name: strings.TrimSpace(c.Name), Address: strings.TrimSpace(address.Address), City: strings.TrimSpace(address.City),
		PostalCode: strings.TrimSpace(address.Zip), AvailableDates: []string{}, AcceptedPasses: []string{}}, nil
}

func boxofficeMovieIDs(index map[string][]string) ([]string, string) {
	ids := make([]string, 0, len(index))
	through := ""
	for id, days := range index {
		if len(days) > 0 {
			ids = append(ids, id)
			through = max(through, days[len(days)-1])
		}
	}
	sort.Strings(ids)
	return ids, through
}

func addBoxofficeMovies(body []byte, ids []string, movies map[string]schedule.MovieRecord) error {
	var items []boxoffice.Movie
	if boxoffice.DecodeJSON(body, &items) != nil || len(items) != len(ids) {
		return fmt.Errorf("%w: movie catalog", errShape)
	}
	for _, item := range items {
		raw := string(item.ID)
		if !slices.Contains(ids, raw) || movies[raw].ProviderID != "" || strings.TrimSpace(item.Title) == "" {
			return fmt.Errorf("%w: movie metadata", errShape)
		}
		id := raw
		if raw[0] == 'c' {
			// Keep opaque, case-sensitive Boxoffice IDs distinct from legacy
			// IDs within Ociné's lowercase public identity grammar.
			id = fmt.Sprintf("boxoffice-%x", sha256.Sum256([]byte(raw)))
		}
		if !schedule.ValidOcineIdentity("movie", id) {
			return fmt.Errorf("%w: movie identity", errShape)
		}
		runtime := 0
		if item.Runtime != nil && *item.Runtime != 0 {
			runtime = *item.Runtime / 60
			if _, ok := schedule.RuntimeDuration(runtime); runtime <= 0 || !ok {
				return fmt.Errorf("%w: movie runtime", errShape)
			}
		}
		poster, ok := schedule.GrandEcranSourcePosterURL(item.Poster)
		if !ok {
			return fmt.Errorf("%w: movie poster", errShape)
		}
		if !schedule.ValidOcinePosterURL(poster) {
			poster = ""
		}
		movies[raw] = schedule.MovieRecord{Provider: schedule.ProviderOcine, ProviderID: id, Slug: "ocine-film-" + id,
			Title: strings.TrimSpace(item.Title), RuntimeMinutes: runtime, PosterURL: poster}
	}
	return nil
}

func parseBoxofficeSchedule(body []byte, id, sourceID, first, from, through string, index map[string][]string, movies map[string]schedule.MovieRecord, loc *time.Location) ([]schedule.ShowtimeRecord, error) {
	var response boxoffice.Schedule
	if boxoffice.DecodeJSON(body, &response) != nil || len(response) != 1 || response[sourceID].Schedule == nil {
		return nil, fmt.Errorf("%w: schedule container", errShape)
	}
	programme := response[sourceID].Schedule
	for movie, days := range programme {
		for day, sessions := range days {
			if len(sessions) > 0 && (day < first || day > through || !slices.Contains(index[movie], day)) {
				return nil, boxoffice.ErrSnapshotChanged
			}
		}
	}
	var result []schedule.ShowtimeRecord
	for movie, days := range index {
		for _, day := range days {
			sessions := programme[movie][day]
			if len(sessions) == 0 && day > from {
				return nil, boxoffice.ErrSnapshotChanged
			}
			for _, session := range sessions {
				row, err := parseBoxofficeShowing(session, id, day, movies[movie], loc)
				if err != nil {
					return nil, err
				}
				if row.ServiceDate >= from {
					result = append(result, row)
				}
			}
		}
	}
	return result, nil
}

func parseBoxofficeShowing(session boxoffice.Showtime, id, day string, movie schedule.MovieRecord, loc *time.Location) (schedule.ShowtimeRecord, error) {
	start, err := boxoffice.ParseStart(session.StartsAt, loc)
	if err != nil || start.Nanosecond() != 0 || boxoffice.ServiceDate(start) != day || strings.TrimSpace(session.ID) == "" || movie.ProviderID == "" {
		return schedule.ShowtimeRecord{}, fmt.Errorf("%w: schedule session", errShape)
	}
	language, version, err := boxoffice.NormalizeVersion(session.Tags)
	if err != nil {
		return schedule.ShowtimeRecord{}, err
	}
	booking, suffix := "", "past"
	for _, ticket := range session.Data.Ticketing {
		if ticket.Provider != "default" || ticket.Type != "DESKTOP" {
			continue
		}
		if booking != "" || len(ticket.URLs) != 1 {
			return schedule.ShowtimeRecord{}, fmt.Errorf("%w: ambiguous booking", errShape)
		}
		booking = ticket.URLs[0]
		native, ok := schedule.OcineEntryID(booking, id)
		if !ok {
			return schedule.ShowtimeRecord{}, fmt.Errorf("%w: booking route", errShape)
		}
		suffix = "r" + native
	}
	if booking == "" && session.Data.Ticketing == nil {
		return schedule.ShowtimeRecord{}, fmt.Errorf("%w: missing ticketing", errShape)
	}
	room := ""
	if session.Screen != nil {
		room = strings.TrimSpace(session.Screen.Name)
	}
	showing := id + "-" + movie.ProviderID + "-" + strconv.FormatInt(start.Unix(), 10) + "-" + suffix
	return schedule.ShowtimeRecord{Provider: schedule.ProviderOcine, ID: "ocine-showing-" + showing, ProviderShowingID: showing,
		TheaterID: "ocine-" + id, Movie: movie, ServiceDate: start.Format(time.DateOnly), StartTime: start, EndTime: start,
		Language: language, ProviderVersion: version, Format: boxoffice.NormalizeFormat(session.Tags), Room: room, BookingURL: booking}, nil
}

type checkoutBindingKey struct{}
type checkoutBinding struct {
	id, source string
	row        schedule.ShowtimeRecord
	canonical  string
}

func (c *Client) CheckoutBoxoffice(ctx context.Context, row schedule.ShowtimeRecord, id string) ([]byte, string, error) {
	if _, ok := schedule.OcineEntryID(row.BookingURL, id); !ok ||
		!schedule.OcineShowingMatches(row.ProviderShowingID, id, row.Movie.ProviderID, row.StartTime) ||
		!schedule.ValidOcineBookingURL(row.BookingURL, id, row.ProviderShowingID) {
		return nil, "", syncproxy.InvalidRequest(sourceName, operationPage)
	}
	binding := &checkoutBinding{id: id, source: row.BookingURL, row: row}
	body, err := c.fetch(context.WithValue(ctx, checkoutBindingKey{}, binding), row.BookingURL, []string{"text/html", "application/xhtml+xml"}, true)
	return body, binding.canonical, err
}

func canonicalCheckoutParts(raw, id string) (film, epoch, version, hall string, ok bool) {
	legacy, valid := strings.CutPrefix(raw, "https://achat.ocine.fr/")
	if !valid {
		return "", "", "", "", false
	}
	return schedule.OcineBookingParts("https://www.ocine.fr/"+legacy, id)
}

func checkoutRedirect(next *http.Request, via []*http.Request) error {
	return syncproxy.NewRedirectChecker(func(u *url.URL) bool {
		if len(via) != 1 || via[0] == nil || via[0].Method != http.MethodGet || u == nil || next.Method != http.MethodGet ||
			next.Response == nil || next.Response.StatusCode != http.StatusTemporaryRedirect ||
			len(next.Response.Header.Values("Location")) != 1 || next.Response.Header.Get("Location") != u.String() {
			return false
		}
		b, ok := via[0].Context().Value(checkoutBindingKey{}).(*checkoutBinding)
		if !ok || via[0].URL.String() != b.source {
			return false
		}
		_, epoch, version, _, ok := canonicalCheckoutParts(u.String(), b.id)
		// The booking engine uses its own film token, distinct from the
		// Boxoffice movie ID. The parser binds the checkout body to that token.
		if !ok || epoch != strconv.FormatInt(b.row.StartTime.Unix(), 10) || (version == "VO") != (b.row.Language == schedule.LanguageVOSTFR) {
			return false
		}
		b.canonical = u.String()
		return true
	})(next, via)
}
