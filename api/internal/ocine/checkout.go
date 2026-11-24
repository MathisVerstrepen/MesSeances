package ocine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"

	"messeances/api/internal/parallel"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

const checkoutBudget = 5 * time.Minute

type checkoutFacts struct {
	end       time.Time
	room      string
	transient bool
}

func enrichCheckouts(parent context.Context, g Getter, rows []schedule.ShowtimeRecord, summary *SyncSummary, clock func() time.Time, loc *time.Location) error {
	indices := make([]int, 0, len(rows))
	for i, row := range rows {
		if row.BookingURL != "" {
			indices = append(indices, i)
		}
	}
	// Use the available checkout time on the soonest screenings first.
	sort.Slice(indices, func(i, j int) bool {
		a, b := rows[indices[i]], rows[indices[j]]
		if !a.StartTime.Equal(b.StartTime) {
			return a.StartTime.Before(b.StartTime)
		}
		return a.ID < b.ID
	})
	work, cancel := context.WithTimeout(parent, checkoutBudget)
	defer cancel()
	type outcome struct {
		facts     checkoutFacts
		attempted bool
		completed bool
	}
	results := make([]outcome, len(rows))
	source, _ := g.(boxofficeGetter)
	// MapOrdered joins all workers on expiry. Each worker owns its row index,
	// so completed facts survive even when the remaining jobs are canceled.
	_, err := parallel.MapOrdered(work, indices, parallel.Options{Workers: 2}, func(ctx context.Context, i int) (struct{}, error) {
		facts, err := fetchCheckoutFacts(ctx, g, source, rows[i], clock, loc)
		results[i] = outcome{facts: facts, attempted: true, completed: err == nil}
		// A timed-out lookup must not mask a sibling's rejected response.
		if errors.Is(work.Err(), context.DeadlineExceeded) && errors.Is(err, context.DeadlineExceeded) {
			return struct{}{}, nil
		}
		return struct{}{}, err
	})
	if err := parent.Err(); err != nil {
		return err
	}
	if err != nil && (!errors.Is(err, context.DeadlineExceeded) || !errors.Is(work.Err(), context.DeadlineExceeded)) {
		return err
	}
	for _, i := range indices {
		result := results[i]
		if result.attempted {
			summary.EndsAttempted++
		}
		if !result.completed {
			summary.CheckoutBudgetSkipped++
			continue
		}
		facts := result.facts
		if facts.transient {
			summary.CheckoutTransientFailures++
		}
		row := &rows[i]
		row.EndTime = facts.end
		if facts.end.After(row.StartTime) {
			summary.EndsRecovered++
		}
		if row.Room == "" {
			row.Room = facts.room
		}
	}
	for _, row := range rows {
		if row.Room == "" {
			summary.RoomsMissing++
		}
	}
	return nil
}

func fetchCheckoutFacts(ctx context.Context, g Getter, source boxofficeGetter, row schedule.ShowtimeRecord, clock func() time.Time, loc *time.Location) (checkoutFacts, error) {
	id := strings.TrimPrefix(row.TheaterID, "ocine-")
	var body []byte
	var canonical string
	var err error
	if source != nil {
		body, canonical, err = source.CheckoutBoxoffice(ctx, row, id)
	} else {
		body, err = g.Checkout(ctx, row.BookingURL)
	}
	if err != nil {
		var requestErr *RequestError
		if errors.As(err, &requestErr) {
			switch requestErr.Kind {
			case syncproxy.FailureStatus:
				if requestErr.StatusCode == http.StatusNotFound && !row.StartTime.After(clock().Add(2*time.Minute)) {
					// Booking can close shortly before start while the grid keeps its link.
					return checkoutFacts{end: row.StartTime}, nil
				}
			case syncproxy.FailureTransport, syncproxy.FailureResponseRead, syncproxy.FailureServer:
				return checkoutFacts{end: row.StartTime, transient: true}, nil
			}
		}
		return checkoutFacts{}, err
	}
	var end time.Time
	var room string
	if source != nil {
		film, epoch, _, _, ok := canonicalCheckoutParts(canonical, id)
		if !ok {
			return checkoutFacts{}, fmt.Errorf("%w: checkout destination", errShape)
		}
		end, room, err = parseCheckoutFacts(body, row, id, film, epoch, loc)
	} else {
		end, room, err = parseCheckout(body, row, id, loc)
	}
	return checkoutFacts{end: end, room: room}, err
}

func parseCheckout(body []byte, s schedule.ShowtimeRecord, id string, loc *time.Location) (time.Time, string, error) {
	film, epoch, _, _, ok := schedule.OcineBookingParts(s.BookingURL, id)
	if !ok {
		return time.Time{}, "", errShape
	}
	return parseCheckoutFacts(body, s, id, film, epoch, loc)
}

func parseCheckoutFacts(body []byte, s schedule.ShowtimeRecord, id, film, epoch string, loc *time.Location) (time.Time, string, error) {
	doc, _, err := document(body)
	if err != nil {
		return time.Time{}, "", err
	}
	if _, err = identity(doc, id); err != nil {
		return time.Time{}, "", err
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
