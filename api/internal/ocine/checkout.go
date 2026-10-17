package ocine

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/net/html"

	"messeances/api/internal/schedule"
)

func parseCheckout(body []byte, s schedule.ShowtimeRecord, id string, loc *time.Location) (time.Time, string, error) {
	doc, _, err := document(body)
	if err != nil {
		return time.Time{}, "", err
	}
	if _, err = identity(doc, id); err != nil {
		return time.Time{}, "", err
	}
	film, epoch, _, _, ok := schedule.OcineBookingParts(s.BookingURL, id)
	if !ok {
		return time.Time{}, "", errShape
	}
	actual, err := jsonField(doc, "movieID")
	if err != nil || actual != film {
		return time.Time{}, "", fmt.Errorf("%w: checkout film", errShape)
	}
	clock, err := jsonField(doc, "date_showtime")
	start, e := time.Parse(time.RFC3339, clock)
	if err != nil || e != nil || !start.Equal(s.StartTime) || epoch == "" {
		return time.Time{}, "", fmt.Errorf("%w: checkout instant", errShape)
	}
	// Missing auditorium is legitimate, but malformed/contradictory published
	// facts are not absence. Only inspect the named source field.
	room, err := optionalJSONField(doc, "auditorium_showtime")
	if err != nil {
		return time.Time{}, "", err
	}
	if m := roomPattern.FindStringSubmatch(room); len(m) == 2 {
		room, err = roomTags(map[string]bool{"sal" + m[1]: true})
		if err != nil {
			return time.Time{}, "", err
		}
	}
	if len(room) > 256 {
		return time.Time{}, "", errShape
	}
	var visible func(*html.Node) string
	visible = func(n *html.Node) string {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return ""
		}
		if n.Type == html.TextNode {
			return n.Data + " "
		}
		var text string
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			text += visible(c)
		}
		return text
	}
	text := clean(visible(doc))
	matches := endPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		if strings.Contains(strings.ToLower(text), "fin prévue") {
			return time.Time{}, "", fmt.Errorf("%w: malformed checkout end", errShape)
		}
		return s.StartTime, room, nil
	}
	if len(matches) != 1 {
		return time.Time{}, "", fmt.Errorf("%w: checkout end count", errShape)
	}
	var candidates []time.Time
	for day := 0; day <= 1; day++ {
		for _, end := range clockInstants(s.StartTime.In(loc).AddDate(0, 0, day), matches[0][1], loc) {
			if end.After(s.StartTime) && end.Sub(s.StartTime) < 24*time.Hour {
				candidates = append(candidates, end)
			}
		}
	}
	if len(candidates) != 1 {
		return time.Time{}, "", fmt.Errorf("%w: checkout end resolution", errShape)
	}
	return candidates[0], room, nil
}

func optionalJSONField(doc *html.Node, key string) (string, error) {
	// Count independently so a genuinely absent source field stays unknown.
	count := 0
	for _, n := range nodes(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "script" }) {
		count += strings.Count(nodeText(n), `"`+key+`"`)
	}
	if count == 0 {
		return "", nil
	}
	return jsonField(doc, key)
}
