package ugc

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"movieflow/api/internal/schedule"
)

type Cinema struct {
	ProviderID      string
	Name            string
	Address         string
	City            string
	PostalCode      string
	AdvertisedDates []string
}

const MaxShowingsPerResponse = 4096
const MaxFilmBlocksPerResponse = 512

func ParseSitemap(r io.Reader) ([]string, error) {
	decoder := xml.NewDecoder(io.LimitReader(r, 8<<20))
	seen := map[string]bool{}
	ids := []string{}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse sitemap: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "loc" {
			continue
		}
		var raw string
		if err := decoder.DecodeElement(&raw, &start); err != nil {
			return nil, fmt.Errorf("parse sitemap location: %w", err)
		}
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		if parsed.Scheme != "https" || strings.ToLower(parsed.Host) != "www.ugc.fr" || parsed.User != nil || parsed.Fragment != "" || parsed.Path != "/cinema.html" {
			continue
		}
		values := parsed.Query()
		id := values.Get("id")
		if len(values) != 1 || len(values["id"]) != 1 || id == "" {
			continue
		}
		number, err := strconv.ParseUint(id, 10, 64)
		if err != nil || number == 0 {
			continue
		}
		if !seen[id] {
			if len(ids) >= schedule.MaxTheaters {
				return nil, fmt.Errorf("sitemap cinema limit exceeded")
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { a, _ := strconv.Atoi(ids[i]); b, _ := strconv.Atoi(ids[j]); return a < b })
	if len(ids) == 0 {
		return nil, fmt.Errorf("sitemap contains no cinemas")
	}
	return ids, nil
}

var localityPattern = regexp.MustCompile(`(?i)cin[ée]ma\s+[àa]\s+(.+?)\s*\(([0-9]{5})\)`)
var addressLocalityPattern = regexp.MustCompile(`(?:^| )([0-9]{5}) (\p{L}[\p{L}\p{M}\p{N} .,'’()/-]*)$`)
var postalCodePattern = regexp.MustCompile(`^[0-9]{5}$`)
var dateIDPattern = regexp.MustCompile(`^nav_date_(\d{4}-\d{2}-\d{2})$`)

type locality struct {
	city       string
	postalCode string
}

func titleLocality(title string) (locality, bool) {
	match := localityPattern.FindStringSubmatch(title)
	if len(match) != 3 {
		return locality{}, false
	}
	return locality{city: collapse(match[1]), postalCode: match[2]}, true
}

func addressLocality(address string) (locality, bool, error) {
	address = collapse(address)
	postalCodes := 0
	for _, field := range strings.Fields(address) {
		if postalCodePattern.MatchString(field) {
			postalCodes++
		}
	}
	if postalCodes == 0 {
		return locality{}, false, nil
	}
	if postalCodes != 1 {
		return locality{}, false, fmt.Errorf("cinema locality malformed")
	}
	match := addressLocalityPattern.FindStringSubmatch(address)
	if len(match) != 3 {
		return locality{}, false, fmt.Errorf("cinema locality malformed")
	}
	return locality{city: collapse(match[2]), postalCode: match[1]}, true, nil
}

func ParseCinema(r io.Reader, expectedID string) (Cinema, error) {
	root, err := html.Parse(io.LimitReader(r, 8<<20))
	if err != nil {
		return Cinema{}, fmt.Errorf("parse cinema page: %w", err)
	}
	var ids, names, addresses []string
	dates := map[string]bool{}
	title := ""
	walk(root, func(node *html.Node) {
		if node.Type != html.ElementNode {
			return
		}
		if node.Data == "title" {
			title = collapse(text(node))
		}
		if node.Data == "input" && strings.EqualFold(attr(node, "name"), "cinemaId") {
			ids = append(ids, strings.TrimSpace(attr(node, "value")))
		}
		if match := dateIDPattern.FindStringSubmatch(attr(node, "id")); len(match) == 2 {
			if !dates[match[1]] && len(dates) >= schedule.MaxAdvertisedDatesPerTheater {
				err = fmt.Errorf("cinema advertised date limit exceeded")
				return
			}
			dates[match[1]] = true
		}
		if node.Data == "h1" && hasAncestorID(node, "cinema-heading") {
			names = append(names, collapse(text(node)))
		}
		if node.Data == "p" && hasClass(node, "address") && hasAncestorID(node, "cinema-heading") {
			addresses = append(addresses, collapse(text(node)))
		}
	})
	if err != nil {
		return Cinema{}, err
	}
	if !singleEqual(ids, expectedID) {
		return Cinema{}, fmt.Errorf("cinema identity missing or conflicting")
	}
	name, ok := singleNonempty(names)
	if !ok {
		return Cinema{}, fmt.Errorf("cinema name missing or conflicting")
	}
	address, ok := singleNonempty(addresses)
	if !ok {
		return Cinema{}, fmt.Errorf("cinema address missing or conflicting")
	}
	titleLocality, hasTitleLocality := titleLocality(title)
	addressLocality, hasAddressLocality, err := addressLocality(address)
	if err != nil {
		return Cinema{}, err
	}
	if !hasTitleLocality && !hasAddressLocality {
		return Cinema{}, fmt.Errorf("cinema locality missing")
	}
	parsedLocality := addressLocality
	if !hasAddressLocality {
		parsedLocality = titleLocality
	} else if hasTitleLocality && titleLocality.postalCode != addressLocality.postalCode {
		return Cinema{}, fmt.Errorf("cinema locality conflicting")
	}
	result := Cinema{ProviderID: expectedID, Name: name, Address: address, City: parsedLocality.city, PostalCode: parsedLocality.postalCode}
	for date := range dates {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return Cinema{}, fmt.Errorf("invalid advertised date")
		}
		result.AdvertisedDates = append(result.AdvertisedDates, date)
	}
	sort.Strings(result.AdvertisedDates)
	if len(result.AdvertisedDates) == 0 {
		return Cinema{}, fmt.Errorf("cinema advertised dates missing")
	}
	return result, nil
}

var blockPattern = regexp.MustCompile(`^bloc-showing-film-([1-9][0-9]*)$`)
var runtimePattern = regexp.MustCompile(`\(([0-9]+)h([0-9]{1,2})?\)`)
var sluggedFilmPathPattern = regexp.MustCompile(`^film_(.+)_([1-9][0-9]*)\.html$`)

func ParseShowings(r io.Reader, cinema Cinema, serviceDate string) ([]schedule.ShowtimeRecord, error) {
	root, err := html.Parse(io.LimitReader(r, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("parse showings: %w", err)
	}
	if err := validateShowingOwnership(root); err != nil {
		return nil, err
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return nil, err
	}
	date, err := time.ParseInLocation("2006-01-02", serviceDate, location)
	if err != nil {
		return nil, fmt.Errorf("invalid service date")
	}
	records := []schedule.ShowtimeRecord{}
	byID := map[string]schedule.ShowtimeRecord{}
	walk(root, func(block *html.Node) {
		if err != nil || block.Type != html.ElementNode {
			return
		}
		match := blockPattern.FindStringSubmatch(attr(block, "id"))
		if len(match) != 2 {
			return
		}
		filmID := match[1]
		buttons, malformedStructure := showingCandidates(block)
		if len(buttons) > MaxShowingsPerResponse || len(records) > MaxShowingsPerResponse-len(buttons) {
			err = fmt.Errorf("showings response limit exceeded")
			return
		}
		if malformedStructure {
			err = fmt.Errorf("showing required attribute missing or conflicting")
			return
		}
		if len(buttons) == 0 {
			return
		}
		title, runtime, poster, metaErr := parseMovieBlock(block, filmID, cinema.ProviderID)
		if metaErr != nil {
			err = metaErr
			return
		}
		for _, button := range buttons {
			record, buttonErr := parseShowingButton(button, cinema.ProviderID, filmID, serviceDate, date, location, title, runtime, poster)
			if buttonErr != nil {
				err = buttonErr
				return
			}
			if previous, exists := byID[record.ID]; exists {
				if fmt.Sprintf("%#v", previous) != fmt.Sprintf("%#v", record) {
					err = fmt.Errorf("conflicting duplicate showing")
				}
				continue
			}
			byID[record.ID] = record
			records = append(records, record)
		}
	})
	if err != nil {
		return nil, err
	}
	if len(records) == 0 && !hasEmptyScheduleMarker(root) {
		nextSessionOnly, validationErr := validateNextSessionOnly(root, cinema.ProviderID, date, location)
		if validationErr != nil {
			return nil, validationErr
		}
		if !nextSessionOnly {
			return nil, fmt.Errorf("unrecognized showings document")
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if !records[i].StartTime.Equal(records[j].StartTime) {
			return records[i].StartTime.Before(records[j].StartTime)
		}
		return records[i].ID < records[j].ID
	})
	return records, nil
}

func parseMovieBlock(block *html.Node, filmID, cinemaID string) (string, int, string, error) {
	var canonicalTitles, legacyTitles []string
	poster := ""
	for _, node := range descendants(block, func(n *html.Node) bool { return n.Type == html.ElementNode }) {
		if node.Data == "a" {
			if isCanonicalFilmHeading(node) {
				if err := validateCanonicalFilmHref(attr(node, "href"), filmID, cinemaID); err != nil {
					return "", 0, "", err
				}
				if dataFilm := attrAny(node, "data-film", "data-film-id"); dataFilm != "" && dataFilm != filmID {
					return "", 0, "", fmt.Errorf("film identity conflict")
				}
				visibleTitle := collapse(text(node))
				attributeTitle := collapse(attr(node, "title"))
				if visibleTitle != "" && attributeTitle != "" && visibleTitle != attributeTitle {
					return "", 0, "", fmt.Errorf("film title conflicting")
				}
				candidate := visibleTitle
				if candidate == "" {
					candidate = attributeTitle
				}
				canonicalTitles = append(canonicalTitles, candidate)
				continue
			}
			dataFilm := attrAny(node, "data-film", "data-film-id")
			candidate := collapse(attr(node, "title"))
			if node.Parent == block && dataFilm != "" && candidate != "" {
				if dataFilm != filmID {
					return "", 0, "", fmt.Errorf("film identity conflict")
				}
				legacyTitles = append(legacyTitles, candidate)
			}
		}
		if node.Data == "img" && poster == "" {
			poster = strings.TrimSpace(attrAny(node, "data-src", "src"))
		}
	}
	var title string
	if len(canonicalTitles) > 0 {
		hasTitle := false
		for _, candidate := range canonicalTitles {
			if collapse(candidate) != "" {
				hasTitle = true
				break
			}
		}
		if !hasTitle {
			return "", 0, "", fmt.Errorf("film title missing")
		}
		var ok bool
		title, ok = singleNonempty(canonicalTitles)
		if !ok {
			return "", 0, "", fmt.Errorf("film title conflicting")
		}
	} else {
		if len(legacyTitles) == 0 {
			return "", 0, "", fmt.Errorf("film title missing")
		}
		if len(legacyTitles) != 1 {
			return "", 0, "", fmt.Errorf("film title conflicting")
		}
		title = legacyTitles[0]
	}
	match := runtimePattern.FindStringSubmatch(collapse(text(block)))
	if len(match) == 0 {
		return "", 0, "", fmt.Errorf("film runtime missing")
	}
	hours, err := strconv.Atoi(match[1])
	if err != nil || hours < 0 || hours > schedule.MaxRuntimeMinutes/60 {
		return "", 0, "", fmt.Errorf("invalid film runtime")
	}
	minutes := 0
	if match[2] != "" {
		minutes, err = strconv.Atoi(match[2])
		if err != nil {
			return "", 0, "", fmt.Errorf("invalid film runtime")
		}
	}
	if minutes < 0 || minutes >= 60 {
		return "", 0, "", fmt.Errorf("invalid film runtime")
	}
	runtime := hours*60 + minutes
	if _, ok := schedule.RuntimeDuration(runtime); !ok {
		return "", 0, "", fmt.Errorf("invalid film runtime")
	}
	return title, runtime, poster, nil
}

func showingCandidates(block *html.Node) ([]*html.Node, bool) {
	seen := map[*html.Node]bool{}
	candidates := []*html.Node{}
	add := func(button *html.Node) {
		if !seen[button] {
			seen[button] = true
			candidates = append(candidates, button)
		}
	}
	malformedStructure := false
	for _, node := range descendants(block, func(n *html.Node) bool { return n.Type == html.ElementNode }) {
		if node.Data == "button" && hasShowingAttribute(node) {
			add(node)
		}
		if !isShowingStructure(node) {
			continue
		}
		buttons := structuralShowingButtons(node, block)
		if len(buttons) == 0 {
			malformedStructure = true
			continue
		}
		for _, button := range buttons {
			add(button)
		}
	}
	return candidates, malformedStructure
}

func validateShowingOwnership(root *html.Node) error {
	for _, candidate := range documentShowingCandidates(root) {
		owners := 0
		for ancestor := candidate.Parent; ancestor != nil; ancestor = ancestor.Parent {
			if blockPattern.MatchString(attr(ancestor, "id")) {
				owners++
			}
		}
		if owners != 1 {
			return fmt.Errorf("unrecognized showing ownership")
		}
	}
	return nil
}

func documentShowingCandidates(root *html.Node) []*html.Node {
	seen := map[*html.Node]bool{}
	candidates := []*html.Node{}
	add := func(node *html.Node) {
		if !seen[node] {
			seen[node] = true
			candidates = append(candidates, node)
		}
	}
	walk(root, func(node *html.Node) {
		if node.Type != html.ElementNode {
			return
		}
		if node.Data == "button" && hasShowingAttribute(node) {
			add(node)
		}
		if !isShowingStructure(node) {
			return
		}
		add(node)
		if hasClass(node, "session") {
			for _, button := range descendants(node, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "button"
			}) {
				add(button)
			}
			return
		}
		if node.Parent != nil {
			for _, button := range descendants(node.Parent, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "button"
			}) {
				add(button)
			}
		}
	})
	return candidates
}

func isShowingStructure(node *html.Node) bool {
	return hasClass(node, "session") || hasClass(node, "screening-room") || hasClass(node, "screening-2D3D")
}

func structuralShowingButtons(node, block *html.Node) []*html.Node {
	for container := node; container != nil; container = container.Parent {
		buttons := descendants(container, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "button"
		})
		if container.Type == html.ElementNode && container.Data == "button" {
			buttons = append([]*html.Node{container}, buttons...)
		}
		if len(buttons) > 0 || container == block {
			return buttons
		}
	}
	return nil
}

func hasShowingAttribute(node *html.Node) bool {
	for _, attribute := range node.Attr {
		name := strings.ToLower(attribute.Key)
		switch name {
		case "data-filmid", "data-film-id", "data-cinemaid", "data-cinema-id", "data-version", "data-seancedate", "data-seance-date", "data-seancehour", "data-seance-hour":
			return true
		case "data-film", "data-cinema":
			if _, ok := positiveNumericID(strings.TrimSpace(attribute.Val)); ok {
				return true
			}
		}
		if strings.HasPrefix(name, "data-showing") {
			return true
		}
	}
	return false
}

func isCanonicalFilmHeading(node *html.Node) bool {
	return node.Type == html.ElementNode && node.Data == "a" && hasClass(node, "color--dark-blue") && node.Parent != nil && node.Parent.Type == html.ElementNode && node.Parent.Data == "div" && hasClass(node.Parent, "block--title") && hasClass(node.Parent, "text-uppercase")
}

func validateCanonicalFilmHref(raw, expectedFilmID, expectedCinemaID string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.TrimSpace(raw) == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("invalid film detail link")
	}
	if parsed.Host != "" {
		if parsed.Scheme != "https" || strings.ToLower(parsed.Host) != "www.ugc.fr" {
			return fmt.Errorf("invalid film detail link")
		}
	} else if parsed.Scheme != "" {
		return fmt.Errorf("invalid film detail link")
	}
	match := sluggedFilmPathPattern.FindStringSubmatch(path.Base(parsed.Path))
	if len(match) != 3 {
		return fmt.Errorf("invalid film detail link")
	}
	if match[2] != expectedFilmID {
		return fmt.Errorf("film identity conflict")
	}
	values := parsed.Query()
	cinemaIDs := values["cinemaId"]
	if len(cinemaIDs) != 1 {
		return fmt.Errorf("invalid film detail link")
	}
	number, err := strconv.ParseUint(cinemaIDs[0], 10, 64)
	if err != nil || number == 0 {
		return fmt.Errorf("invalid film detail link")
	}
	if cinemaIDs[0] != expectedCinemaID {
		return fmt.Errorf("film identity conflict")
	}
	return nil
}

var nextSessionPattern = regexp.MustCompile(`(?i)^prochaine séance le(?: ([\p{L}\p{M}]+))? ([0-9]{1,2}) ([\p{L}\p{M}]+\.?) ([0-9]{4})$`)
var nextSessionPrefixPattern = regexp.MustCompile(`(?i)^prochaine séance(?:\s|$)`)

var frenchMonths = map[string]time.Month{
	"janvier":   time.January,
	"janv":      time.January,
	"janv.":     time.January,
	"février":   time.February,
	"févr":      time.February,
	"févr.":     time.February,
	"mars":      time.March,
	"avril":     time.April,
	"avr":       time.April,
	"avr.":      time.April,
	"mai":       time.May,
	"juin":      time.June,
	"juillet":   time.July,
	"juil":      time.July,
	"juil.":     time.July,
	"août":      time.August,
	"septembre": time.September,
	"sept":      time.September,
	"sept.":     time.September,
	"octobre":   time.October,
	"oct":       time.October,
	"oct.":      time.October,
	"novembre":  time.November,
	"nov":       time.November,
	"nov.":      time.November,
	"décembre":  time.December,
	"déc":       time.December,
	"déc.":      time.December,
}

var frenchWeekdays = map[string]time.Weekday{
	"dimanche": time.Sunday,
	"lundi":    time.Monday,
	"mardi":    time.Tuesday,
	"mercredi": time.Wednesday,
	"jeudi":    time.Thursday,
	"vendredi": time.Friday,
	"samedi":   time.Saturday,
}

func validateNextSessionOnly(root *html.Node, cinemaID string, serviceDate time.Time, location *time.Location) (bool, error) {
	if len(documentShowingCandidates(root)) != 0 {
		return false, nil
	}
	blocks := []*html.Node{}
	placeholders := []*html.Node{}
	malformedBlock := false
	walk(root, func(node *html.Node) {
		if node.Type != html.ElementNode {
			return
		}
		id := attr(node, "id")
		if !strings.HasPrefix(id, "bloc-showing-film-") {
			return
		}
		if id == "bloc-showing-film-" {
			placeholders = append(placeholders, node)
			return
		}
		match := blockPattern.FindStringSubmatch(id)
		if len(match) != 2 {
			malformedBlock = true
			return
		}
		if _, ok := positiveNumericID(match[1]); !ok {
			malformedBlock = true
			return
		}
		blocks = append(blocks, node)
	})
	if malformedBlock || len(blocks) == 0 {
		return false, nil
	}
	if len(blocks)+len(placeholders) > MaxFilmBlocksPerResponse {
		return false, fmt.Errorf("film block limit exceeded")
	}
	allBlocks := append(append([]*html.Node{}, blocks...), placeholders...)
	if !recognizedNextSessionRoot(root, allBlocks) {
		return false, nil
	}
	documentMarkerDates, err := nextSessionMarkerDates(root, location)
	if err != nil {
		return false, nil
	}
	validMarkerCount := 0
	seenFilmIDs := map[string]bool{}
	for _, block := range blocks {
		filmID := blockPattern.FindStringSubmatch(attr(block, "id"))[1]
		if seenFilmIDs[filmID] {
			return false, nil
		}
		seenFilmIDs[filmID] = true
		headings := descendants(block, isCanonicalFilmHeading)
		if len(headings) != 1 || validateCanonicalFilmHref(attr(headings[0], "href"), filmID, cinemaID) != nil {
			return false, nil
		}
		for _, attribute := range []string{"data-film", "data-film-id", "data-filmid"} {
			if value := strings.TrimSpace(attr(headings[0], attribute)); value != "" && value != filmID {
				return false, nil
			}
		}
		markerDates, err := nextSessionMarkerDates(block, location)
		if err != nil || len(markerDates) > 1 {
			return false, nil
		}
		if len(markerDates) == 1 {
			if !markerDates[0].After(serviceDate) {
				return false, nil
			}
			validMarkerCount++
		}
	}
	for _, placeholder := range placeholders {
		if placeholderCarriesIdentity(placeholder) || placeholderCarriesShowingEvidence(placeholder) || len(documentShowingCandidates(placeholder)) != 0 {
			return false, nil
		}
		markerDates, err := nextSessionMarkerDates(placeholder, location)
		if err != nil || len(markerDates) != 1 || !markerDates[0].After(serviceDate) {
			return false, nil
		}
		validMarkerCount++
	}
	if validMarkerCount == 0 || len(documentMarkerDates) != validMarkerCount {
		return false, nil
	}
	return true, nil
}

func recognizedNextSessionRoot(root *html.Node, blocks []*html.Node) bool {
	if len(blocks) == 0 || blocks[0].Parent == nil {
		return false
	}
	parent := blocks[0].Parent
	for _, block := range blocks[1:] {
		if block.Parent != parent {
			return false
		}
	}
	if parent.Type != html.ElementNode {
		return false
	}
	if parent.Data == "body" {
		return true
	}
	if attr(parent, "id") != "showings" {
		return false
	}
	showingsRoots := 0
	walk(root, func(node *html.Node) {
		if node.Type == html.ElementNode && attr(node, "id") == "showings" {
			showingsRoots++
		}
	})
	return showingsRoots == 1
}

func placeholderCarriesIdentity(block *html.Node) bool {
	found := false
	walk(block, func(node *html.Node) {
		if found || node.Type != html.ElementNode {
			return
		}
		if isCanonicalFilmHeading(node) || node.Data == "a" && strings.TrimSpace(attr(node, "href")) != "" {
			found = true
			return
		}
		for _, attribute := range node.Attr {
			switch strings.ToLower(attribute.Key) {
			case "data-film", "data-filmid", "data-film-id", "data-cinema", "data-cinemaid", "data-cinema-id":
				if strings.TrimSpace(attribute.Val) != "" {
					found = true
					return
				}
			}
		}
	})
	return found
}

func placeholderCarriesShowingEvidence(block *html.Node) bool {
	found := false
	walk(block, func(node *html.Node) {
		if found || node.Type != html.ElementNode {
			return
		}
		found = hasShowingAttribute(node) || isShowingStructure(node)
	})
	return found
}

func nextSessionMarkerDates(block *html.Node, location *time.Location) ([]time.Time, error) {
	dates := []time.Time{}
	for _, node := range descendants(block, func(n *html.Node) bool { return n.Type == html.ElementNode }) {
		value := collapse(text(node))
		if !nextSessionPrefixPattern.MatchString(value) {
			continue
		}
		hasMatchingDescendant := false
		for _, child := range descendants(node, func(n *html.Node) bool { return n.Type == html.ElementNode }) {
			if nextSessionPattern.MatchString(collapse(text(child))) {
				hasMatchingDescendant = true
				break
			}
		}
		if hasMatchingDescendant {
			continue
		}
		date, err := parseNextSessionDate(value, location)
		if err != nil {
			return nil, err
		}
		dates = append(dates, date)
	}
	return dates, nil
}

func parseNextSessionDate(value string, location *time.Location) (time.Time, error) {
	match := nextSessionPattern.FindStringSubmatch(collapse(value))
	if len(match) != 5 {
		return time.Time{}, fmt.Errorf("invalid next session date")
	}
	day, dayErr := strconv.Atoi(match[2])
	year, yearErr := strconv.Atoi(match[4])
	month, monthOK := frenchMonths[strings.ToLower(match[3])]
	if dayErr != nil || yearErr != nil || !monthOK {
		return time.Time{}, fmt.Errorf("invalid next session date")
	}
	date := time.Date(year, month, day, 0, 0, 0, 0, location)
	if date.Year() != year || date.Month() != month || date.Day() != day {
		return time.Time{}, fmt.Errorf("invalid next session date")
	}
	if match[1] != "" {
		weekday, ok := frenchWeekdays[strings.ToLower(match[1])]
		if !ok || date.Weekday() != weekday {
			return time.Time{}, fmt.Errorf("invalid next session date")
		}
	}
	return date, nil
}

func parseShowingButton(button *html.Node, cinemaID, filmID, serviceDate string, date time.Time, location *time.Location, title string, runtime int, poster string) (schedule.ShowtimeRecord, error) {
	showingID := attrAny(button, "data-showing", "data-showing-id")
	buttonFilm, filmIDOK := providerNumericID(button, []string{"data-filmid", "data-film-id"}, "data-film")
	buttonCinema, cinemaIDOK := providerNumericID(button, []string{"data-cinemaid", "data-cinema-id"}, "data-cinema")
	version := strings.ToUpper(collapse(attr(button, "data-version")))
	rawDate := attrAny(button, "data-seancedate", "data-seance-date")
	rawHour := attrAny(button, "data-seancehour", "data-seance-hour")
	if showingID == "" || !filmIDOK || buttonFilm != filmID || !cinemaIDOK || buttonCinema != cinemaID || version == "" || rawDate == "" || rawHour == "" {
		return schedule.ShowtimeRecord{}, fmt.Errorf("showing required attribute missing or conflicting")
	}
	language, err := normalizeLanguage(version)
	if err != nil {
		return schedule.ShowtimeRecord{}, err
	}
	clock, err := time.Parse("15:04", rawHour)
	if err != nil {
		return schedule.ShowtimeRecord{}, fmt.Errorf("invalid showing hour")
	}
	hour, minute := clock.Hour(), clock.Minute()
	if hour > 2 && hour < 8 || hour == 2 && minute > 0 {
		return schedule.ShowtimeRecord{}, fmt.Errorf("showing outside cinema day")
	}
	startDate := date
	if hour < 8 {
		startDate = date.AddDate(0, 0, 1)
	}
	start := time.Date(startDate.Year(), startDate.Month(), startDate.Day(), hour, minute, 0, 0, location)
	parsedAttrDate, err := parseProviderDate(rawDate, location)
	if err != nil || (!sameDay(parsedAttrDate, date) && !sameDay(parsedAttrDate, startDate)) {
		return schedule.ShowtimeRecord{}, fmt.Errorf("invalid showing date")
	}
	format := showingFormat(button)
	if !validShowingFormat(format) {
		return schedule.ShowtimeRecord{}, fmt.Errorf("unknown showing format")
	}
	room := ""
	for ancestor := button.Parent; ancestor != nil; ancestor = ancestor.Parent {
		for _, node := range descendants(ancestor, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, "screening-room") }) {
			room = collapse(text(node))
			break
		}
		if room != "" || blockPattern.MatchString(attr(ancestor, "id")) {
			break
		}
	}
	duration, ok := schedule.RuntimeDuration(runtime)
	if !ok {
		return schedule.ShowtimeRecord{}, fmt.Errorf("invalid film runtime")
	}
	return schedule.ShowtimeRecord{ID: "ugc-showing-" + showingID, ProviderShowingID: showingID, ServiceDate: serviceDate, TheaterID: "ugc-" + cinemaID, Movie: schedule.MovieRecord{ProviderID: filmID, Slug: "ugc-film-" + filmID, Title: title, RuntimeMinutes: runtime, PosterURL: poster}, StartTime: start, EndTime: start.Add(duration), Language: language, ProviderVersion: version, Format: format, Room: room, BookingURL: "https://www.ugc.fr/reservationSeances.html?id=" + url.QueryEscape(showingID)}, nil
}

func providerNumericID(node *html.Node, preferred []string, legacy string) (string, bool) {
	selected := ""
	for _, name := range preferred {
		if value := strings.TrimSpace(attr(node, name)); value != "" {
			if _, ok := positiveNumericID(value); !ok || selected != "" && selected != value {
				return "", false
			}
			selected = value
		}
	}
	if selected != "" {
		return selected, true
	}
	return positiveNumericID(strings.TrimSpace(attr(node, legacy)))
}

func positiveNumericID(value string) (string, bool) {
	number, err := strconv.ParseUint(value, 10, 64)
	return value, err == nil && number > 0
}

func normalizeLanguage(value string) (string, error) {
	switch value {
	case "VOSTF", "VOSTFR":
		return schedule.LanguageVOSTFR, nil
	case "VF":
		return schedule.LanguageVF, nil
	case "VFSTF":
		return schedule.LanguageVFSME, nil
	case "VO":
		return schedule.LanguageVO, nil
	default:
		return "", fmt.Errorf("unknown showing version")
	}
}
func showingFormat(button *html.Node) string {
	for ancestor := button.Parent; ancestor != nil; ancestor = ancestor.Parent {
		nodes := descendants(ancestor, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, "screening-2D3D") })
		if len(nodes) > 0 {
			value := strings.ToUpper(collapse(text(nodes[0])))
			switch {
			case strings.Contains(value, "IMAX"):
				return "IMAX"
			case strings.Contains(value, "DOLBY"):
				return "DOLBY"
			case strings.Contains(value, "4DX"):
				return "4DX"
			case strings.Contains(value, "3D"):
				return "3D"
			case value == "" || strings.Contains(value, "2D"):
				return "2D"
			default:
				return value
			}
		}
		if hasClass(ancestor, "session") || len(descendants(ancestor, func(n *html.Node) bool { return n.Type == html.ElementNode && hasClass(n, "screening-room") })) > 0 {
			return "2D"
		}
		if blockPattern.MatchString(attr(ancestor, "id")) {
			break
		}
	}
	return "2D"
}
func validShowingFormat(v string) bool {
	switch v {
	case "2D", "3D", "IMAX", "DOLBY", "4DX":
		return true
	}
	return false
}
func parseProviderDate(value string, location *time.Location) (time.Time, error) {
	for _, layout := range []string{"02/01/2006", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, value, location); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date")
}
func sameDay(a, b time.Time) bool { return a.Year() == b.Year() && a.YearDay() == b.YearDay() }
func walk(node *html.Node, fn func(*html.Node)) {
	fn(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walk(child, fn)
	}
}
func descendants(node *html.Node, predicate func(*html.Node) bool) []*html.Node {
	out := []*html.Node{}
	walk(node, func(n *html.Node) {
		if n != node && predicate(n) {
			out = append(out, n)
		}
	})
	return out
}
func attr(node *html.Node, name string) string {
	for _, a := range node.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}
func attrAny(node *html.Node, names ...string) string {
	for _, name := range names {
		if value := attr(node, name); value != "" {
			return value
		}
	}
	return ""
}
func hasClass(node *html.Node, class string) bool {
	for _, candidate := range strings.Fields(attr(node, "class")) {
		if candidate == class {
			return true
		}
	}
	return false
}
func hasAncestorID(node *html.Node, id string) bool {
	for parent := node.Parent; parent != nil; parent = parent.Parent {
		if attr(parent, "id") == id {
			return true
		}
	}
	return false
}
func text(node *html.Node) string {
	var builder strings.Builder
	var appendVisibleText func(*html.Node)
	appendVisibleText = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "template":
				return
			}
		}
		if n.Type == html.TextNode {
			builder.WriteString(n.Data)
			builder.WriteByte(' ')
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			appendVisibleText(child)
		}
	}
	appendVisibleText(node)
	return builder.String()
}
func collapse(value string) string { return strings.Join(strings.Fields(value), " ") }
func singleEqual(values []string, want string) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if value != want {
			return false
		}
	}
	return true
}
func singleNonempty(values []string) (string, bool) {
	seen := ""
	for _, value := range values {
		value = collapse(value)
		if value == "" {
			continue
		}
		if seen != "" && seen != value {
			return "", false
		}
		seen = value
	}
	return seen, seen != ""
}
func hasEmptyScheduleMarker(root *html.Node) bool {
	found := false
	walk(root, func(n *html.Node) {
		explicitMarker := hasClass(n, "no-showing") || hasClass(n, "no-result")
		inShowingsContainer := attr(n, "id") == "showings" || hasAncestorID(n, "showings")
		if n.Type == html.ElementNode && explicitMarker && inShowingsContainer {
			found = true
		}
	})
	return found
}
