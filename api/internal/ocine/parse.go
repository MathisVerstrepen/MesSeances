package ocine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"messeances/api/internal/schedule"
)

var errShape = errors.New("invalid Ocine source structure")
var headerPattern = regexp.MustCompile(`^(lun\.|mar\.|mer\.|jeu\.|ven\.|sam\.|dim\.)\s*([0-9]{1,2})$`)
var clockPattern = regexp.MustCompile(`^([0-9]{1,2})h([0-9]{2})$`)
var runtimePattern = regexp.MustCompile(`^Durée\s*:\s*([0-9]+)\s*h\s*([0-9]{0,2})(\s*-.*)?$`)
var resizePattern = regexp.MustCompile(`^(https://fr\.web\.img[0-9]*\.acsta\.net)/r_[0-9]+_[0-9]+/`)
var popupPattern = regexp.MustCompile(`Adresse\s*:\s*</strong>\s*<span>([\s\S]*?)</span>\s*(?:<br\s*/?>\s*<span>([\s\S]*?)</span>)?`)
var addressPattern = regexp.MustCompile(`^(.*?)[,\-]?\s*([0-9]{2}\s?[0-9]{3})\s+(.+)$`)
var breakPattern = regexp.MustCompile(`(?i)<br\s*/?>`)
var tagPattern = regexp.MustCompile(`(?:tag_|sal_)([a-z0-9_]+)\.png(?:$|[?#])`)
var roomPattern = regexp.MustCompile(`(?i)^(?:sal|salle-)([0-9]+)$`)
var endPattern = regexp.MustCompile(`(?i)fin prévue\s+[àa]\s+([0-9]{1,2}h[0-9]{2})`)

type programme struct {
	theater   schedule.TheaterRecord
	movies    []schedule.MovieRecord
	showtimes []schedule.ShowtimeRecord
	through   string
}

func document(body []byte) (*html.Node, string, error) {
	if len(body) == 0 || len(body) > MaxResponseBytes {
		return nil, "", errShape
	}
	// HTML's declared charset wins. UTF-8 pages stay UTF-8; legacy source meta
	// declarations are decoded using the HTML encoding rules, not a fixed codec.
	r, err := charset.NewReader(bytes.NewReader(body), "")
	if err != nil {
		return nil, "", errShape
	}
	decoded, err := io.ReadAll(io.LimitReader(r, 4*MaxResponseBytes+1))
	if err != nil || len(decoded) > 4*MaxResponseBytes {
		return nil, "", errShape
	}
	text := string(decoded)
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "</body>") || !strings.Contains(lower, "</html>") {
		return nil, "", errShape
	}
	n, err := html.Parse(strings.NewReader(text))
	if err != nil {
		return nil, "", errShape
	}
	return n, text, nil
}

func jsonField(doc *html.Node, key string) (string, error) {
	pattern := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `"\s*:\s*("(?:[^"\\]|\\.)*")`)
	var all []string
	for _, n := range nodes(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "script" }) {
		for _, m := range pattern.FindAllStringSubmatch(nodeText(n), -1) {
			var s string
			if json.Unmarshal([]byte(m[1]), &s) != nil {
				return "", errShape
			}
			all = append(all, s)
		}
	}
	if len(all) != 1 {
		return "", errShape
	}
	return all[0], nil
}

func identity(doc *html.Node, id string) (string, error) {
	actual, e1 := jsonField(doc, "theaterID")
	name, e2 := jsonField(doc, "theaterName")
	expected := map[string]string{"309": "Ociné Maubeuge", "310": "Ociné Saint-Omer", "461": "Ociné Dunkerque"}[id]
	if e1 != nil || e2 != nil || actual != id || expected == "" || !strings.EqualFold(clean(name), expected) {
		return "", fmt.Errorf("%w: theater identity", errShape)
	}
	return clean(name), nil
}

func parseProgramme(body []byte, id, from string, now time.Time, loc *time.Location) (programme, error) {
	p := programme{through: from}
	doc, _, err := document(body)
	if err != nil {
		return p, err
	}
	name, err := identity(doc, id)
	if err != nil {
		return p, err
	}
	p.theater = schedule.TheaterRecord{Provider: schedule.ProviderOcine, ProviderID: id, ID: "ocine-" + id, Slug: "ocine-" + id, Name: name, AvailableDates: []string{}, AcceptedPasses: []string{}}
	prints := byClass(doc, "seances_print")
	if len(prints) != 1 || len(nodes(doc, func(n *html.Node) bool { return attr(n, "id") == "footer_new" })) != 1 || len(nodes(doc, func(n *html.Node) bool { return attr(n, "id") == "mod_ccweb_cine_jeu_large" })) != 1 {
		return p, fmt.Errorf("%w: programme container", errShape)
	}
	container := prints[0].Parent
	if container == nil || container.Parent == nil || !hasClass(container.Parent, "cadre-in") {
		return p, errShape
	}
	blocks := byClass(container, "hr_film")
	if len(blocks) != len(byClass(doc, "hr_film")) {
		return p, errShape
	}
	if len(blocks) == 0 {
		if clean(nodeText(container)) != "" || len(nodes(container, func(n *html.Node) bool { return n != container && n.Type == html.ElementNode })) != 1 || len(byClass(doc, "hor")) != 0 || len(byClass(doc, "nom_jour")) != 0 {
			return p, fmt.Errorf("%w: empty programme", errShape)
		}
		return p, nil
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
			dates, err = resolveDates(headers, now, loc)
			if err != nil {
				return p, err
			}
			if last := dates[len(dates)-1].Format(time.DateOnly); last > p.through {
				p.through = last
			}
		} else if strings.Join(headers, "|") != strings.Join(current, "|") {
			return p, fmt.Errorf("%w: inconsistent headers", errShape)
		}
		movie, err := readMovie(block, id)
		if err != nil {
			return p, err
		}
		p.movies = append(p.movies, movie)
		cells := byClass(block, "tab_seances")
		if len(cells) != len(dates) {
			return p, fmt.Errorf("%w: day cells", errShape)
		}
		seen := map[int]bool{}
		clocks := 0
		anchors := 0
		for _, cell := range cells {
			day := 0
			for _, class := range strings.Fields(attr(cell, "class")) {
				if strings.HasPrefix(class, "jour") {
					n, e := strconv.Atoi(strings.TrimPrefix(class, "jour"))
					if e != nil || day != 0 || class != "jour"+strconv.Itoa(n) {
						return p, errShape
					}
					day = n
				}
			}
			if day < 1 || day > len(dates) || seen[day] {
				return p, fmt.Errorf("%w: day index", errShape)
			}
			seen[day] = true
			rows := byClass(cell, "frow")
			if len(rows) == 0 {
				return p, fmt.Errorf("%w: missing rows", errShape)
			}
			for _, row := range rows {
				tagCells := byClass(row, "celtags")
				if len(tagCells) != 1 {
					return p, errShape
				}
				dayTags := tags(tagCells[0])
				sessions := byClass(row, "hr_seance")
				if len(sessions) == 0 && (len(byClass(row, "aucune")) != 1 || len(byClass(row, "hor")) != 0) {
					return p, fmt.Errorf("%w: empty row", errShape)
				}
				if len(sessions) > 0 && len(byClass(row, "aucune")) != 0 {
					return p, errShape
				}
				for _, session := range sessions {
					clockNodes := byClass(session, "hor")
					if len(clockNodes) != 1 {
						return p, fmt.Errorf("%w: session clock", errShape)
					}
					clocks++
					s, err := readSession(session, clean(nodeText(clockNodes[0])), dates[day-1], movie, id, dayTags, loc)
					if err != nil {
						return p, err
					}
					if s.BookingURL != "" {
						anchors++
					}
					if s.ServiceDate >= from {
						p.showtimes = append(p.showtimes, s)
					}
				}
			}
		}
		if clocks != len(byClass(block, "hor")) {
			return p, fmt.Errorf("%w: orphan clock", errShape)
		}
		links := nodes(block, func(n *html.Node) bool { return n.Data == "a" && strings.Contains(attr(n, "href"), "/reserver/") })
		if anchors != len(links) {
			return p, fmt.Errorf("%w: orphan booking", errShape)
		}
	}
	return p, nil
}

func readHeaders(block *html.Node) ([]string, error) {
	ns := byClass(block, "nom_jour")
	if len(ns) < 1 {
		return nil, errShape
	}
	result := make([]string, 0, len(ns))
	for _, n := range ns {
		if n.Parent == nil || !hasClass(n.Parent, "hr_jour") {
			return nil, errShape
		}
		v := strings.ToLower(clean(nodeText(n.Parent)))
		if !headerPattern.MatchString(v) {
			return nil, fmt.Errorf("%w: header text", errShape)
		}
		result = append(result, v)
	}
	return result, nil
}

func resolveDates(headers []string, now time.Time, loc *time.Location) ([]time.Time, error) {
	today := now.In(loc)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
	weekdays := map[string]time.Weekday{"lun.": time.Monday, "mar.": time.Tuesday, "mer.": time.Wednesday, "jeu.": time.Thursday, "ven.": time.Friday, "sam.": time.Saturday, "dim.": time.Sunday}
	var result []time.Time
	for delta := -31; delta <= 31; delta++ {
		candidate := today.AddDate(0, 0, delta)
		dates := make([]time.Time, 0, len(headers))
		valid := true
		for i, h := range headers {
			m := headerPattern.FindStringSubmatch(h)
			if len(m) != 3 {
				return nil, errShape
			}
			day, _ := strconv.Atoi(m[2])
			d := candidate.AddDate(0, 0, i)
			if d.Day() != day || d.Weekday() != weekdays[m[1]] {
				valid = false
				break
			}
			dates = append(dates, d)
		}
		if valid {
			if result != nil {
				return nil, errShape
			}
			result = dates
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%w: date sequence", errShape)
	}
	return result, nil
}

func clockInstants(date time.Time, clock string, loc *time.Location) []time.Time {
	m := clockPattern.FindStringSubmatch(clock)
	if len(m) != 3 {
		return nil
	}
	h, _ := strconv.Atoi(m[1])
	minute, _ := strconv.Atoi(m[2])
	if h > 23 || minute > 59 {
		return nil
	}
	guess := time.Date(date.Year(), date.Month(), date.Day(), h, minute, 0, 0, loc)
	var result []time.Time
	for delta := -3 * time.Hour; delta <= 3*time.Hour; delta += 30 * time.Minute {
		t := guess.Add(delta).In(loc)
		if t.Format(time.DateOnly) == date.Format(time.DateOnly) && t.Hour() == h && t.Minute() == minute {
			result = append(result, t)
		}
	}
	return result
}

func readMovie(block *html.Node, id string) (schedule.MovieRecord, error) {
	var movie schedule.MovieRecord
	h2 := nodes(block, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "h2" })
	if len(h2) != 1 {
		return movie, errShape
	}
	links := nodes(h2[0], func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "a" })
	if len(links) != 1 {
		return movie, errShape
	}
	prefix := "https://www.ocine.fr/" + schedule.OcineSlug(id) + "/film/"
	href := attr(links[0], "href")
	fid := strings.TrimSuffix(strings.TrimPrefix(href, prefix), "/")
	title := attr(links[0], "title")
	if !schedule.ValidOcineIdentity("movie", fid) || href != prefix+fid+"/" || !strings.HasPrefix(title, "Voir la fiche du film ") || clean(strings.TrimPrefix(title, "Voir la fiche du film ")) == "" {
		return movie, fmt.Errorf("%w: film identity", errShape)
	}
	movie = schedule.MovieRecord{Provider: schedule.ProviderOcine, ProviderID: fid, Slug: "ocine-film-" + fid, Title: clean(strings.TrimPrefix(title, "Voir la fiche du film "))}
	durations := byClass(block, "hr_dur")
	if len(durations) > 1 {
		return movie, errShape
	}
	if len(durations) == 1 && strings.Contains(nodeText(durations[0]), "Durée") {
		m := runtimePattern.FindStringSubmatch(clean(nodeText(durations[0])))
		if len(m) == 0 {
			return movie, fmt.Errorf("%w: runtime", errShape)
		}
		h, e1 := strconv.Atoi(m[1])
		minutes, e2 := strconv.Atoi("0" + m[2])
		if e1 != nil || e2 != nil || h > 1000000 || minutes > 59 || h*60+minutes <= 0 {
			return movie, errShape
		}
		movie.RuntimeMinutes = h*60 + minutes
	}
	posters := byClass(block, "hr_aff")
	if len(posters) > 1 {
		return movie, errShape
	}
	if len(posters) == 1 {
		poster := resizePattern.ReplaceAllString(attr(posters[0], "src"), "${1}/")
		if poster != "" && !schedule.ValidOcinePosterURL(poster) {
			return movie, fmt.Errorf("%w: poster", errShape)
		}
		movie.PosterURL = poster
	}
	return movie, nil
}

func readSession(n *html.Node, clock string, date time.Time, movie schedule.MovieRecord, id string, dayTags map[string]bool, loc *time.Location) (schedule.ShowtimeRecord, error) {
	s := schedule.ShowtimeRecord{Provider: schedule.ProviderOcine, TheaterID: "ocine-" + id, ServiceDate: date.Format(time.DateOnly), Movie: movie, Format: schedule.Format2D}
	st := tags(n)
	language, version, err := tagVersion(st)
	if err != nil {
		return s, err
	}
	s.Language, s.ProviderVersion = language, version
	s.Format = formatTags(dayTags, st)
	s.Room, err = roomTags(st)
	if err != nil {
		return s, err
	}
	hall := "past"
	if n.Data == "a" {
		if hasClass(n, "past") {
			return s, errShape
		}
		s.BookingURL = attr(n, "href")
		f, epoch, v, h, ok := schedule.OcineBookingParts(s.BookingURL, id)
		if !ok || movie.ProviderID[0] >= '0' && movie.ProviderID[0] <= '9' && movie.ProviderID != f {
			return s, fmt.Errorf("%w: booking binding", errShape)
		}
		seconds, _ := strconv.ParseInt(epoch, 10, 64)
		s.StartTime = time.Unix(seconds, 0).In(loc)
		found := false
		for _, t := range clockInstants(date, clock, loc) {
			// The grid publishes minutes, while D is the actual second-precision instant.
			found = found || t.Equal(s.StartTime.Truncate(time.Minute))
		}
		if !found {
			return s, fmt.Errorf("%w: grid epoch mismatch", errShape)
		}
		if v == "VF" {
			s.Language, s.ProviderVersion = schedule.LanguageVF, "VF"
		} else {
			s.Language, s.ProviderVersion = schedule.LanguageVOSTFR, "VOSTF"
		}
		if version != "" && version != s.ProviderVersion {
			return s, fmt.Errorf("%w: conflicting session version", errShape)
		}
		hall = h
	} else {
		if n.Data != "div" || !hasClass(n, "past") || len(nodes(n, func(n *html.Node) bool { return n.Data == "a" })) != 0 {
			return s, fmt.Errorf("%w: anchorless session", errShape)
		}
		instants := clockInstants(date, clock, loc)
		if len(instants) != 1 {
			return s, fmt.Errorf("%w: ambiguous fallback instant", errShape)
		}
		s.StartTime = instants[0]
		// Day tags mix VF and VO. Never assign them to an anchorless row.
	}
	s.EndTime = s.StartTime
	s.ProviderShowingID = id + "-" + movie.ProviderID + "-" + strconv.FormatInt(s.StartTime.Unix(), 10) + "-" + hall
	s.ID = "ocine-showing-" + s.ProviderShowingID
	if !schedule.OcineShowingMatches(s.ProviderShowingID, id, movie.ProviderID, s.StartTime) {
		return s, errShape
	}
	return s, nil
}

func tags(n *html.Node) map[string]bool {
	result := map[string]bool{}
	for _, img := range nodes(n, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "img" }) {
		for _, class := range strings.Fields(attr(img, "class")) {
			if strings.HasPrefix(class, "tag-") {
				result[strings.ToLower(strings.TrimPrefix(class, "tag-"))] = true
			}
		}
		if m := tagPattern.FindStringSubmatch(attr(img, "src")); len(m) == 2 {
			t := strings.TrimSuffix(m[1], "_new")
			if strings.Contains(attr(img, "src"), "/sal_") {
				t = "sal" + t
			}
			result[t] = true
		}
	}
	return result
}
func tagVersion(tags map[string]bool) (schedule.Language, string, error) {
	vf, vo := tags["vf"], tags["vost"] || tags["vostf"] || tags["vo"]
	if vf && vo {
		return "", "", fmt.Errorf("%w: contradictory version tags", errShape)
	}
	if vf {
		return schedule.LanguageVF, "VF", nil
	}
	if vo {
		return schedule.LanguageVOSTFR, "VOSTF", nil
	}
	return "", "", nil
}
func formatTags(day, session map[string]bool) schedule.Format {
	for _, tags := range []map[string]bool{session, day} {
		switch {
		case tags["4dx"] || tags["motion"]:
			return schedule.Format4DX
		case tags["imax"]:
			return schedule.FormatIMAX
		case tags["dolby"] || tags["atmos"]:
			return schedule.FormatDolby
		case tags["3d"]:
			return schedule.Format3D
		case tags["2d"]:
			return schedule.Format2D
		}
	}
	return schedule.Format2D
}
func roomTags(tags map[string]bool) (string, error) {
	room := ""
	for tag := range tags {
		if m := roomPattern.FindStringSubmatch(tag); len(m) == 2 {
			n, e := strconv.Atoi(m[1])
			if e != nil || n <= 0 {
				return "", errShape
			}
			v := "Salle " + strconv.Itoa(n)
			if room != "" && room != v {
				return "", errShape
			}
			room = v
		}
	}
	return room, nil
}

func parseAddress(body []byte, id string) (string, string, string, error) {
	doc, raw, err := document(body)
	if err != nil {
		return "", "", "", err
	}
	name, err := identity(doc, id)
	if err != nil {
		return "", "", "", err
	}
	m := popupPattern.FindStringSubmatch(raw)
	if len(m) != 3 {
		return "", "", "", fmt.Errorf("%w: address popup", errShape)
	}
	parts := make([]string, 0, 2)
	for _, part := range m[1:] {
		fragment, err := html.Parse(strings.NewReader(breakPattern.ReplaceAllString(stdhtml.UnescapeString(part), ", ")))
		if err != nil {
			return "", "", "", errShape
		}
		parts = append(parts, strings.Trim(clean(strings.ReplaceAll(nodeText(fragment), `\'`, "'")), ", "))
	}
	joined := strings.Trim(strings.Join(parts, ", "), ", ")
	if joined == "" {
		_, city, _ := strings.Cut(name, " ")
		return "", city, "", nil
	}
	address := addressPattern.FindStringSubmatch(joined)
	if len(address) != 4 {
		return "", "", "", fmt.Errorf("%w: address fields", errShape)
	}
	return strings.Trim(address[1], ", -"), clean(address[3]), strings.ReplaceAll(address[2], " ", ""), nil
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
	var b strings.Builder
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
