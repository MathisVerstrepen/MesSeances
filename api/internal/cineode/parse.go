package cineode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	stdhtml "html"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/text/encoding/charmap"

	"messeances/api/internal/schedule"
)

var errShape = errors.New("invalid Cineode programme structure")

type programme struct {
	theater   schedule.TheaterRecord
	movies    []schedule.MovieRecord
	showtimes []schedule.ShowtimeRecord
	through   string
}

var theaterIDPattern = regexp.MustCompile(`"theaterID"\s*:\s*("(?:[^"\\]|\\.)*")`)
var theaterNamePattern = regexp.MustCompile(`"theaterName"\s*:\s*("(?:[^"\\]|\\.)*")`)
var headerPattern = regexp.MustCompile(`^(lun\.|mar\.|mer\.|jeu\.|ven\.|sam\.|dim\.)\s*([0-9]{1,2})$`)
var clockPattern = regexp.MustCompile(`^([0-9]{1,2})h([0-9]{2})$`)
var runtimePattern = regexp.MustCompile(`^Durée\s*:\s*([0-9]+)\s*h\s*([0-9]{0,2})(\s*-.*)?$`)
var resizePattern = regexp.MustCompile(`^(https://fr\.web\.img[0-9]*\.acsta\.net)/r_[0-9]+_[0-9]+/`)
var addressPattern = regexp.MustCompile(`^(.*?)[,\-]?\s*([0-9]{2}\s?[0-9]{3})\s+(.+)$`)
var popupPattern = regexp.MustCompile(`Adresse\s*:\s*</strong>\s*<span>([^<]*)</span>\s*(?:<br\s*/?>\s*<span>([^<]*)</span>)?`)
var contactPattern = regexp.MustCompile(`(?i)([0-9]{1,4}(?:\s?(?:bis|ter))?\s+[a-zà-ÿ'’\- ]{3,60}?)\s+([0-9]{5})\s+([a-zà-ÿ'’\- ]{2,40})`)
var contactStop = regexp.MustCompile(`(?i)\s+(?:Tél|Tel|Fax|Email|Mail|Site|Accueil|Contact|Horaires|Tarifs)\b`)
var tagPattern = regexp.MustCompile(`tag_([a-z0-9_]+)\.png(?:$|[?#])`)

func document(body []byte) (*html.Node, string, error) {
	if len(body) == 0 || len(body) > MaxResponseBytes {
		return nil, "", errShape
	}
	decoded, err := charmap.ISO8859_1.NewDecoder().Bytes(body)
	if err != nil {
		return nil, "", errShape
	}
	text := string(decoded)
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "</body>") || !strings.Contains(lower, "</html>") {
		return nil, "", errShape
	}
	n, err := html.Parse(bytes.NewReader(decoded))
	if err != nil {
		return nil, "", errShape
	}
	return n, text, nil
}

func parseProgramme(body []byte, id, from string, now time.Time, location *time.Location) (programme, error) {
	var p programme
	doc, _, err := document(body)
	if err != nil {
		return p, err
	}
	scripts := nodes(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "script" })
	var script strings.Builder
	for _, n := range scripts {
		script.WriteString(nodeText(n))
		script.WriteByte('\n')
	}
	ids, names := theaterIDPattern.FindAllStringSubmatch(script.String(), -1), theaterNamePattern.FindAllStringSubmatch(script.String(), -1)
	var actual, name string
	if len(ids) != 1 || len(names) != 1 || json.Unmarshal([]byte(ids[0][1]), &actual) != nil || json.Unmarshal([]byte(names[0][1]), &name) != nil || actual != id || !schedule.ValidCineodeIdentity("theater", id) || clean(name) == "" {
		return p, fmt.Errorf("%w: identity", errShape)
	}
	p.theater = schedule.TheaterRecord{Provider: schedule.ProviderCineode, ProviderID: id, ID: "cineode-" + id, Slug: "cineode-" + id, Name: clean(name), AvailableDates: []string{}, AcceptedPasses: []string{}}
	setAddress(&p.theater, programmeAddress(doc, id))
	containers := byClass(doc, "seances_print")
	if len(containers) != 1 || len(nodes(doc, func(n *html.Node) bool { return attr(n, "id") == "footer_new" })) != 1 {
		return p, fmt.Errorf("%w: programme container", errShape)
	}
	print := containers[0]
	container := print.Parent
	if container == nil || container.Parent == nil || !hasClass(container.Parent, "cadre-in") || len(nodes(doc, func(n *html.Node) bool { return attr(n, "id") == "mod_ccweb_cine_jeu_large" })) != 1 {
		return p, fmt.Errorf("%w: programme hierarchy", errShape)
	}
	blocks := byClass(container, "hr_film")
	if len(blocks) != len(byClass(doc, "hr_film")) {
		return p, fmt.Errorf("%w: orphan film", errShape)
	}
	p.through = from
	if len(blocks) == 0 {
		if len(nodes(container, func(n *html.Node) bool { return n != container && n.Type == html.ElementNode })) != 1 || clean(nodeText(container)) != "" || len(byClass(doc, "nom_jour")) != 0 || len(byClass(doc, "hor")) != 0 {
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
			dates, err = resolveDates(headers, now, location)
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
			return p, fmt.Errorf("film block: %w", err)
		}
		p.movies = append(p.movies, movie)
		cells := byClass(block, "tab_seances")
		if len(cells) != len(dates) {
			return p, fmt.Errorf("%w: day cells", errShape)
		}
		seen := make(map[int]bool, len(cells))
		totalTimes := 0
		for _, cell := range cells {
			day := 0
			for _, class := range strings.Fields(attr(cell, "class")) {
				if strings.HasPrefix(class, "jour") {
					n, err := strconv.Atoi(strings.TrimPrefix(class, "jour"))
					if err != nil || day != 0 || class != "jour"+strconv.Itoa(n) {
						return p, fmt.Errorf("%w: day class", errShape)
					}
					day = n
				}
			}
			if day < 1 || day > len(dates) || seen[day] {
				return p, fmt.Errorf("%w: repeated or absent day", errShape)
			}
			seen[day] = true
			rows := byClass(cell, "frow")
			if len(rows) == 0 {
				return p, fmt.Errorf("%w: missing version rows", errShape)
			}
			for _, row := range rows {
				times := byClass(row, "hor")
				totalTimes += len(times)
				language, version, format, err := attributes(row)
				if err != nil {
					return p, fmt.Errorf("session tags: %w", err)
				}
				if len(times) == 0 {
					if len(byClass(row, "aucune")) != 1 {
						return p, fmt.Errorf("%w: unrecognized empty cell", errShape)
					}
					continue
				}
				if len(byClass(row, "aucune")) != 0 {
					return p, fmt.Errorf("%w: contradictory empty cell", errShape)
				}
				for _, clock := range times {
					start, err := resolveClock(dates[day-1], clean(nodeText(clock)), location)
					if err != nil {
						return p, err
					}
					date := dates[day-1].Format(time.DateOnly)
					if date < from {
						continue
					}
					key := id + "-" + movie.ProviderID + "-" + strconv.FormatInt(start.Unix(), 10)
					if !schedule.CineodeShowingMatches(key, id, movie.ProviderID, start) {
						return p, fmt.Errorf("%w: showing identity", errShape)
					}
					p.showtimes = append(p.showtimes, schedule.ShowtimeRecord{Provider: schedule.ProviderCineode, ID: "cineode-showing-" + key, ProviderShowingID: key, TheaterID: p.theater.ID, ServiceDate: date, Movie: movie, StartTime: start, EndTime: start, Language: language, ProviderVersion: version, Format: format, BookingURL: schedule.CineodeWebsite(id)})
				}
			}
		}
		if totalTimes != len(byClass(block, "hor")) {
			return p, fmt.Errorf("%w: orphan clock", errShape)
		}
	}
	return p, nil
}

func readHeaders(block *html.Node) ([]string, error) {
	ns := byClass(block, "nom_jour")
	if len(ns) < 1 || len(ns) > 14 {
		return nil, fmt.Errorf("%w: headers", errShape)
	}
	result := make([]string, 0, len(ns))
	for _, n := range ns {
		if n.Parent == nil || !hasClass(n.Parent, "hr_jour") {
			return nil, fmt.Errorf("%w: header parent", errShape)
		}
		value := strings.ToLower(clean(nodeText(n.Parent)))
		if !headerPattern.MatchString(value) {
			return nil, fmt.Errorf("%w: header text", errShape)
		}
		result = append(result, value)
	}
	return result, nil
}

func resolveDates(headers []string, now time.Time, location *time.Location) ([]time.Time, error) {
	today := now.In(location)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, location)
	weekdays := map[string]time.Weekday{"lun.": time.Monday, "mar.": time.Tuesday, "mer.": time.Wednesday, "jeu.": time.Thursday, "ven.": time.Friday, "sam.": time.Saturday, "dim.": time.Sunday}
	var result []time.Time
	for delta := -3; delta <= 3; delta++ {
		candidate := today.AddDate(0, 0, delta)
		dates := make([]time.Time, 0, len(headers))
		valid := true
		for i, header := range headers {
			parts := headerPattern.FindStringSubmatch(header)
			if len(parts) != 3 {
				return nil, errShape
			}
			day, _ := strconv.Atoi(parts[2])
			date := candidate.AddDate(0, 0, i)
			if date.Weekday() != weekdays[parts[1]] || date.Day() != day {
				valid = false
				break
			}
			dates = append(dates, date)
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

func resolveClock(date time.Time, clock string, location *time.Location) (time.Time, error) {
	m := clockPattern.FindStringSubmatch(clock)
	if len(m) != 3 {
		return time.Time{}, fmt.Errorf("%w: clock", errShape)
	}
	h, _ := strconv.Atoi(m[1])
	minute, _ := strconv.Atoi(m[2])
	if h > 23 || minute > 59 {
		return time.Time{}, errShape
	}
	guess := time.Date(date.Year(), date.Month(), date.Day(), h, minute, 0, 0, location)
	var result time.Time
	count := 0
	for delta := -3 * time.Hour; delta <= 3*time.Hour; delta += 30 * time.Minute {
		t := guess.Add(delta).In(location)
		if t.Year() == date.Year() && t.Month() == date.Month() && t.Day() == date.Day() && t.Hour() == h && t.Minute() == minute {
			result = t
			count++
		}
	}
	if count != 1 {
		return time.Time{}, fmt.Errorf("%w: ambiguous or nonexistent instant", errShape)
	}
	return result, nil
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
	prefix := "https://www.cineode.fr/" + schedule.CineodeSlug(id) + "/film/"
	href := attr(links[0], "href")
	fid := strings.TrimSuffix(strings.TrimPrefix(href, prefix), "/")
	title := attr(links[0], "title")
	if !schedule.ValidCineodeIdentity("movie", fid) || href != prefix+fid+"/" || !strings.HasPrefix(title, "Voir la fiche du film ") || clean(strings.TrimPrefix(title, "Voir la fiche du film ")) == "" {
		return movie, fmt.Errorf("%w: film", errShape)
	}
	movie = schedule.MovieRecord{Provider: schedule.ProviderCineode, ProviderID: fid, Slug: "cineode-film-" + fid, Title: clean(strings.TrimPrefix(title, "Voir la fiche du film "))}
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
		raw := attr(posters[0], "src")
		// Removing only the resize directive cannot change authority or escaping.
		poster := resizePattern.ReplaceAllString(raw, "${1}/")
		if raw == "" || !schedule.ValidCineodePosterURL(poster) {
			return movie, fmt.Errorf("%w: poster", errShape)
		}
		movie.PosterURL = poster
	}
	return movie, nil
}

func attributes(cell *html.Node) (schedule.Language, string, schedule.Format, error) {
	tagCells := byClass(cell, "celtags")
	if len(tagCells) != 1 {
		return "", "", "", errShape
	}
	tags := map[string]bool{}
	for _, n := range nodes(tagCells[0], func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "img" }) {
		if m := tagPattern.FindStringSubmatch(attr(n, "src")); len(m) == 2 {
			tags[m[1]] = true
		}
	}
	if tags["vf_new"] == tags["vost_new"] {
		return "", "", "", fmt.Errorf("%w: version", errShape)
	}
	language, version := schedule.LanguageVF, "VF"
	if tags["vost_new"] {
		language, version = schedule.LanguageVOSTFR, "VOSTF"
	}
	format := schedule.Format2D
	switch {
	case tags["3d_new"] || tags["3d"]:
		format = schedule.Format3D
	case tags["imax_new"] || tags["imax"]:
		format = schedule.FormatIMAX
	case tags["dolby_new"] || tags["atmos"]:
		format = schedule.FormatDolby
	case tags["motion_new"] || tags["4dx"]:
		format = schedule.Format4DX
	}
	return language, version, format, nil
}

func programmeAddress(doc *html.Node, id string) string {
	for _, n := range byClass(doc, "soustitre") {
		if address := safeAddress(nodeText(n), id); address != "" {
			return address
		}
	}
	for _, n := range nodes(doc, func(n *html.Node) bool { return attr(n, "id") == "le_cine" }) {
		for _, p := range nodes(n, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "p" }) {
			if address := safeAddress(nodeText(p), id); address != "" {
				return address
			}
		}
	}
	return ""
}

func metadataAddress(body []byte, id, route string) (string, error) {
	if len(body) == 0 {
		return "", nil
	}
	doc, raw, err := document(body)
	if err != nil {
		return "", err
	}
	if route == "infos-cine" {
		if m := popupPattern.FindStringSubmatch(raw); len(m) == 3 {
			return safeAddress(stdhtml.UnescapeString(strings.Trim(strings.Join(m[1:], ", "), ", ")), id), nil
		}
		return "", nil
	}
	for _, n := range byClass(doc, "page") {
		text := clean(nodeText(n))
		for _, m := range contactPattern.FindAllStringSubmatch(text, -1) {
			city := contactStop.Split(m[3], 2)[0]
			if address := safeAddress(m[1]+", "+m[2]+" "+city, id); address != "" {
				return address, nil
			}
		}
	}
	return "", nil
}

func safeAddress(value, id string) string {
	value = clean(value)
	if id != "592" && (strings.Contains(strings.ToLower(value), "brinon") || strings.Contains(value, "02300")) {
		return ""
	}
	return value
}
func setAddress(t *schedule.TheaterRecord, value string) {
	t.Address = clean(value)
	if m := addressPattern.FindStringSubmatch(t.Address); len(m) == 4 {
		t.Address = strings.Trim(m[1], ", -")
		t.PostalCode = strings.ReplaceAll(m[2], " ", "")
		t.City = clean(m[3])
	}
	if t.City == "" {
		switch t.ProviderID {
		case "991":
			t.City = "Montmirail"
		case "4386":
			t.City = "Plateau d'Hauteville"
		}
	}
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
