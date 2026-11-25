package cotecine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/text/encoding/charmap"

	"messeances/api/internal/schedule"
)

var (
	// ErrShape reports a page that does not match the Cinegroup programme.
	ErrShape          = errors.New("invalid Cotecine programme structure")
	errEmptyProgramme = fmt.Errorf("%w: empty programme", ErrShape)
)

// maxDays bounds the day columns of a film block.
const maxDays = 21

var (
	theaterIDPattern   = regexp.MustCompile(`"theaterID"\s*:\s*("(?:[^"\\]|\\.)*")`)
	theaterNamePattern = regexp.MustCompile(`"theaterName"\s*:\s*("(?:[^"\\]|\\.)*")`)
	headerPattern      = regexp.MustCompile(`^(lun\.|mar\.|mer\.|jeu\.|ven\.|sam\.|dim\.)\s*([0-9]{1,2})$`)
	clockPattern       = regexp.MustCompile(`^([0-9]{1,2})h([0-9]{2})$`)
	runtimePattern     = regexp.MustCompile(`^Durée\s*:\s*([0-9]{1,2})\s*h\s*([0-9]{2})\b`)
	tagPattern         = regexp.MustCompile(`/tag_([a-z0-9_]+)\.png$`)
	// bookingPattern is a session route below the site: film, start instant,
	// version and auditorium.
	bookingPattern = regexp.MustCompile(`^reserver/F([1-9][0-9]{0,9})/D([1-9][0-9]{0,11})/[A-Z]{2,5}/[1-9][0-9]{0,9}/$`)
	// posterResize removes the Allociné CDN resize directive.
	posterResize = regexp.MustCompile(`^(https://[a-z0-9.]+\.acsta\.net)/(?:c|r)_[0-9]+_[0-9]+(?:_[0-9]+_[0-9]+)?/`)
)

// session is one parsed session before the film metadata is merged.
type session struct {
	movie, date string
	start       time.Time
	language    schedule.Language
	version     string
	format      schedule.Format
	booking     string
}

// programme is everything parsed from one site's programme page.
type programme struct {
	name     string
	movies   []schedule.MovieRecord
	sessions []session
	// skipped counts sessions without a usable version or with a booking
	// route contradicting the displayed time.
	skipped int
	through string
}

// decode returns the page text: UTF-8 when the body is valid multi-byte
// UTF-8, otherwise ISO-8859-1, which Cinegroup sites serve.
func decode(body []byte) (string, error) {
	if utf8.Valid(body) {
		return string(body), nil
	}
	decoded, err := charmap.ISO8859_1.NewDecoder().Bytes(body)
	if err != nil {
		return "", ErrShape
	}
	return string(decoded), nil
}

// parseProgramme reads a Cinegroup "seances/" page. Every film block must
// resolve; a session whose version tags are unknown is skipped and counted.
func parseProgramme(body []byte, src Source, from string, now time.Time, location *time.Location) (programme, error) {
	var p programme
	if len(body) == 0 || len(body) > MaxResponseBytes {
		return p, ErrShape
	}
	text, err := decode(body)
	if err != nil {
		return p, err
	}
	if lower := strings.ToLower(text); !strings.Contains(lower, "</body>") || !strings.Contains(lower, "</html>") {
		return p, fmt.Errorf("%w: truncated page", ErrShape)
	}
	doc, err := html.Parse(strings.NewReader(text))
	if err != nil {
		return p, ErrShape
	}
	var script strings.Builder
	for _, n := range nodes(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "script" }) {
		script.WriteString(nodeText(n))
		script.WriteByte('\n')
	}
	id, ok := uniqueJSONString(theaterIDPattern, script.String())
	name, okName := uniqueJSONString(theaterNamePattern, script.String())
	if !ok || !okName || id != src.TheaterID || clean(name) == "" {
		return p, fmt.Errorf("%w: identity", ErrShape)
	}
	p.name = clean(name)
	printBlocks := byClass(doc, "seances_print")
	if len(printBlocks) != 1 || printBlocks[0].Parent == nil {
		return p, fmt.Errorf("%w: programme container", ErrShape)
	}
	container := printBlocks[0].Parent
	blocks := byClass(container, "hr_film")
	if len(blocks) != len(byClass(doc, "hr_film")) {
		return p, fmt.Errorf("%w: orphan film", ErrShape)
	}
	p.through = from
	if len(blocks) == 0 {
		if len(byClass(doc, "hor")) != 0 {
			return p, fmt.Errorf("%w: orphan clock", ErrShape)
		}
		return p, errEmptyProgramme
	}
	clocks := 0
	for _, block := range blocks {
		clocks += len(byClass(block, "hor"))
	}
	if clocks != len(byClass(doc, "hor")) {
		return p, fmt.Errorf("%w: orphan clock", ErrShape)
	}
	var headers []string
	var dates []time.Time
	for _, block := range blocks {
		current, err := readHeaders(block)
		if err != nil {
			return p, err
		}
		if headers == nil {
			headers = current
			if dates, err = resolveDates(headers, now, location); err != nil {
				return p, err
			}
			p.through = max(p.through, dates[len(dates)-1].Format(time.DateOnly))
		} else if strings.Join(headers, "|") != strings.Join(current, "|") {
			return p, fmt.Errorf("%w: inconsistent day headers", ErrShape)
		}
		movie, err := readMovie(block, src)
		if err != nil {
			return p, err
		}
		p.movies = append(p.movies, movie)
		if err := readSessions(&p, block, src.Website, movie.ProviderID, dates, from, location); err != nil {
			return p, err
		}
	}
	return p, nil
}

func uniqueJSONString(pattern *regexp.Regexp, text string) (string, bool) {
	value := ""
	for _, m := range pattern.FindAllStringSubmatch(text, -1) {
		var decoded string
		if json.Unmarshal([]byte(m[1]), &decoded) != nil || value != "" && decoded != value {
			return "", false
		}
		value = decoded
	}
	return value, value != ""
}

func readHeaders(block *html.Node) ([]string, error) {
	ns := byClass(block, "nom_jour")
	if len(ns) < 1 || len(ns) > maxDays {
		return nil, fmt.Errorf("%w: day headers", ErrShape)
	}
	result := make([]string, 0, len(ns))
	for _, n := range ns {
		if n.Parent == nil || !hasClass(n.Parent, "hr_jour") {
			return nil, fmt.Errorf("%w: day header parent", ErrShape)
		}
		value := strings.ToLower(clean(nodeText(n.Parent)))
		if !headerPattern.MatchString(value) {
			return nil, fmt.Errorf("%w: day header text", ErrShape)
		}
		result = append(result, value)
	}
	return result, nil
}

// resolveDates finds the single date sequence, starting within three days of
// now, whose weekdays and days of month match the headers.
func resolveDates(headers []string, now time.Time, location *time.Location) ([]time.Time, error) {
	today := now.In(location)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, location)
	weekdays := map[string]time.Weekday{"lun.": time.Monday, "mar.": time.Tuesday, "mer.": time.Wednesday, "jeu.": time.Thursday, "ven.": time.Friday, "sam.": time.Saturday, "dim.": time.Sunday}
	var result []time.Time
	for delta := -3; delta <= 3; delta++ {
		candidate := today.AddDate(0, 0, delta)
		dates := make([]time.Time, 0, len(headers))
		for i, header := range headers {
			parts := headerPattern.FindStringSubmatch(header)
			day, _ := strconv.Atoi(parts[2])
			date := candidate.AddDate(0, 0, i)
			if date.Weekday() != weekdays[parts[1]] || date.Day() != day {
				dates = nil
				break
			}
			dates = append(dates, date)
		}
		if dates != nil {
			if result != nil {
				return nil, fmt.Errorf("%w: ambiguous date sequence", ErrShape)
			}
			result = dates
		}
	}
	if result == nil {
		return nil, fmt.Errorf("%w: date sequence", ErrShape)
	}
	return result, nil
}

// resolveClock places a displayed clock on its service day; starts before
// 03:00 belong to the next calendar day.
func resolveClock(day time.Time, clock string, location *time.Location) (time.Time, error) {
	m := clockPattern.FindStringSubmatch(clock)
	if len(m) != 3 {
		return time.Time{}, fmt.Errorf("%w: clock", ErrShape)
	}
	h, _ := strconv.Atoi(m[1])
	minute, _ := strconv.Atoi(m[2])
	if h > 23 || minute > 59 {
		return time.Time{}, fmt.Errorf("%w: clock", ErrShape)
	}
	date := day
	if h < 3 {
		date = day.AddDate(0, 0, 1)
	}
	start := time.Date(date.Year(), date.Month(), date.Day(), h, minute, 0, 0, location)
	if start.Hour() != h || start.Minute() != minute {
		return time.Time{}, fmt.Errorf("%w: nonexistent local time", ErrShape)
	}
	return start, nil
}

// filmID is a Cotecine film id; filmSlug a repertory film listed by slug.
var (
	filmID   = regexp.MustCompile(`^[1-9][0-9]{0,9}$`)
	filmSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

func readMovie(block *html.Node, src Source) (schedule.MovieRecord, error) {
	var movie schedule.MovieRecord
	h2 := nodes(block, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "h2" })
	if len(h2) != 1 {
		return movie, fmt.Errorf("%w: film heading", ErrShape)
	}
	links := nodes(h2[0], func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" })
	if len(links) != 1 {
		return movie, fmt.Errorf("%w: film link", ErrShape)
	}
	href, title := attr(links[0], "href"), attr(links[0], "title")
	id, ok := strings.CutPrefix(href, src.Website+"film/")
	id, okSuffix := strings.CutSuffix(id, "/")
	if !filmID.MatchString(id) && filmSlug.MatchString(id) {
		// Slugs are site-local, so the film is scoped to its theater.
		id = src.TheaterID + "-" + id
	}
	title, okTitle := strings.CutPrefix(title, "Voir la fiche du film ")
	if !ok || !okSuffix || !okTitle || !schedule.ValidCotecineIdentity("movie", id) || clean(title) == "" {
		return movie, fmt.Errorf("%w: film identity", ErrShape)
	}
	movie = schedule.MovieRecord{Provider: schedule.ProviderCotecine, ProviderID: id, Slug: string(schedule.ProviderCotecine) + "-film-" + id, Title: clean(title)}
	if durations := byClass(block, "hr_dur"); len(durations) == 1 {
		if m := runtimePattern.FindStringSubmatch(clean(nodeText(durations[0]))); m != nil {
			h, _ := strconv.Atoi(m[1])
			minutes, _ := strconv.Atoi(m[2])
			if minutes < 60 && h*60+minutes > 0 {
				movie.RuntimeMinutes = h*60 + minutes
			}
		}
	}
	if posters := byClass(block, "hr_aff"); len(posters) == 1 {
		poster := posterResize.ReplaceAllString(attr(posters[0], "src"), "${1}/")
		if schedule.ValidCotecinePosterURL(poster) {
			movie.PosterURL = poster
		}
	}
	return movie, nil
}

func readSessions(p *programme, block *html.Node, website, movie string, dates []time.Time, from string, location *time.Location) error {
	cells := byClass(block, "tab_seances")
	if len(cells) != len(dates) {
		return fmt.Errorf("%w: day cells", ErrShape)
	}
	seen := make(map[int]bool, len(cells))
	clocks := 0
	for _, cell := range cells {
		day := 0
		for _, class := range strings.Fields(attr(cell, "class")) {
			if n, ok := strings.CutPrefix(class, "jour"); ok {
				value, err := strconv.Atoi(n)
				if err != nil || day != 0 || class != "jour"+strconv.Itoa(value) {
					return fmt.Errorf("%w: day class", ErrShape)
				}
				day = value
			}
		}
		if day < 1 || day > len(dates) || seen[day] {
			return fmt.Errorf("%w: repeated or absent day", ErrShape)
		}
		seen[day] = true
		date := dates[day-1].Format(time.DateOnly)
		for _, row := range byClass(cell, "frow") {
			items := byClass(row, "hr_seance")
			language, version, known := versionTags(row)
			for _, item := range items {
				hor := byClass(item, "hor")
				if len(hor) != 1 {
					return fmt.Errorf("%w: session clock", ErrShape)
				}
				clocks++
				start, err := resolveClock(dates[day-1], clean(nodeText(hor[0])), location)
				if err != nil {
					return err
				}
				if date < from {
					continue
				}
				booking, consistent := bookingURL(item, website, movie, start)
				if !known || !consistent {
					p.skipped++
					continue
				}
				p.sessions = append(p.sessions, session{movie: movie, date: date, start: start, language: language, version: version, format: formatTags(item), booking: booking})
			}
		}
	}
	if clocks != len(byClass(block, "hor")) {
		return fmt.Errorf("%w: orphan clock", ErrShape)
	}
	return nil
}

// versionTags reads the version tag of a session row.
func versionTags(row *html.Node) (schedule.Language, string, bool) {
	tags := byClass(row, "celtags")
	if len(tags) != 1 {
		return "", "", false
	}
	found := imageTags(tags[0])
	count := 0
	language, version := schedule.Language(""), ""
	for tag, value := range map[string]struct {
		language schedule.Language
		version  string
	}{"vf_new": {schedule.LanguageVF, "VF"}, "vost_new": {schedule.LanguageVOSTFR, "VOSTF"}, "vfst_new": {schedule.LanguageVFSTF, "VFSTF"}} {
		if found[tag] {
			count++
			language, version = value.language, value.version
		}
	}
	return language, version, count == 1
}

// formatTags maps a session's projection tags; unknown tags mean 2D.
func formatTags(item *html.Node) schedule.Format {
	tags := imageTags(item)
	switch {
	case tags["3d_new"] || tags["3d"]:
		return schedule.Format3D
	case tags["imax_new"] || tags["imax"]:
		return schedule.FormatIMAX
	case tags["4dx_new"] || tags["4dx"]:
		return schedule.Format4DX
	case tags["dolby_new"] || tags["atmos"]:
		return schedule.FormatDolby
	default:
		return schedule.Format2D
	}
}

func imageTags(n *html.Node) map[string]bool {
	tags := map[string]bool{}
	for _, img := range nodes(n, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "img" }) {
		if m := tagPattern.FindStringSubmatch(attr(img, "src")); m != nil {
			tags[m[1]] = true
		}
	}
	return tags
}

// bookingURL returns the session's booking route when it has one. A route
// naming another film or start instant makes the session inconsistent.
func bookingURL(item *html.Node, website, movie string, start time.Time) (string, bool) {
	if item.Data != "a" {
		return "", true
	}
	raw := attr(item, "href")
	route, ok := strings.CutPrefix(raw, website)
	m := bookingPattern.FindStringSubmatch(route)
	if !ok || m == nil || !schedule.ValidPublicHTTPSURL(raw) {
		return "", true
	}
	// A scoped repertory film books under a Cotecine id the page does not
	// otherwise expose; only the start instant can be checked.
	return raw, (m[1] == movie || schedule.CotecineScopedMovie(movie)) && m[2] == strconv.FormatInt(start.Unix(), 10)
}

func clean(value string) string { return strings.Join(strings.Fields(value), " ") }

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func byClass(n *html.Node, class string) []*html.Node {
	return nodes(n, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, class) })
}

func nodes(n *html.Node, match func(*html.Node) bool) []*html.Node {
	var result []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if match(n) {
			result = append(result, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return result
}

func nodeText(n *html.Node) string {
	var b bytes.Buffer
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}
