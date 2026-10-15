package veo

import (
	"bytes"
	"context"
	"html"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"messeances/api/internal/parallel"
	"messeances/api/internal/schedule"
)

var checkoutEnd = regexp.MustCompile(`(?i)fin\s+prévue\s+à\s+([0-2][0-9])h([0-5][0-9])`)
var checkoutRoom = regexp.MustCompile(`"auditorium_showtime"\s*:\s*(null|"salle-([1-9][0-9]{0,19})")`)

type checkoutFacts struct {
	hour, minute int
	hasEnd       bool
	room         string
}

func parseCheckout(body []byte) (checkoutFacts, error) {
	if len(body) == 0 || len(body) > MaxHTMLBytes {
		return checkoutFacts{}, errShape
	}
	if !utf8.Valid(body) {
		r, err := charset.NewReader(bytes.NewReader(body), "")
		if err != nil {
			return checkoutFacts{}, errShape
		}
		body, err = io.ReadAll(io.LimitReader(r, MaxHTMLBytes+1))
		if err != nil || len(body) > MaxHTMLBytes {
			return checkoutFacts{}, errShape
		}
	}
	root, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		return checkoutFacts{}, errShape
	}
	var blocks []string
	var text func(*xhtml.Node) string
	text = func(n *xhtml.Node) string {
		if n.Type == xhtml.TextNode {
			return n.Data
		}
		var b strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			b.WriteString(text(c))
			b.WriteByte(' ')
		}
		return b.String()
	}
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			for _, a := range n.Attr {
				if a.Key == "id" && a.Val == "s_fin" || a.Key == "class" && contains(strings.Fields(a.Val), "s_fin") {
					blocks = append(blocks, strings.Join(strings.Fields(text(n)), " "))
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	facts := checkoutFacts{}
	for _, block := range blocks {
		matches := checkoutEnd.FindAllStringSubmatch(block, -1)
		if len(matches) != 1 {
			return checkoutFacts{}, errShape
		}
		h, _ := strconv.Atoi(matches[0][1])
		m, _ := strconv.Atoi(matches[0][2])
		if h > 23 || facts.hasEnd && (h != facts.hour || m != facts.minute) {
			return checkoutFacts{}, errShape
		}
		facts.hour = h
		facts.minute = m
		facts.hasEnd = true
	}
	// A recognizable booking-page identity is required even when the source does
	// not publish an end. Generic error/login/maintenance pages are not absence.
	decoded := strings.ReplaceAll(html.UnescapeString(string(body)), `\"`, `"`)
	matches := checkoutRoom.FindAllStringSubmatch(decoded, -1)
	if len(matches) == 0 || len(matches) != strings.Count(decoded, `"auditorium_showtime"`) {
		return checkoutFacts{}, errShape
	}
	first := matches[0][1]
	for _, m := range matches {
		if m[1] != first {
			return checkoutFacts{}, errShape
		}
	}
	if matches[0][2] != "" {
		facts.room = "Salle " + matches[0][2]
	}
	return facts, nil
}

// Resolve a published local end clock against calendar dates, not 24-hour
// arithmetic. Ambiguous repeated clocks fail closed rather than guess an offset.
func exactEnd(start time.Time, f checkoutFacts) (time.Time, error) {
	if !f.hasEnd {
		return start, nil
	}
	loc := start.Location()
	for day := 0; day < 2; day++ {
		date := start.AddDate(0, 0, day)
		wall := time.Date(date.Year(), date.Month(), date.Day(), f.hour, f.minute, 0, 0, time.UTC).Format("2006-01-02T15:04")
		candidate, err := localTime("2006-01-02T15:04", wall, loc)
		if err != nil {
			return time.Time{}, err
		}
		if candidate.After(start) {
			return candidate, nil
		}
	}
	return time.Time{}, errShape
}
func enrichCheckouts(parent context.Context, g Getter, rows []schedule.ShowtimeRecord, summary *SyncSummary, now time.Time) error {
	if now.IsZero() {
		return errShape
	}
	indices := []int{}
	for i, r := range rows {
		if r.TheaterID != "veo-EMS1361" {
			// Started sessions remain part of their advertised date. Their portal
			// may no longer be bookable: retain API room and an unknown canonical
			// end, without confusing an error page with missing source metadata.
			if !r.StartTime.After(now) {
				rows[i].EndTime = r.StartTime
				continue
			}
			indices = append(indices, i)
		}
	}
	if len(indices) == 0 {
		return parent.Err()
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	facts, err := parallel.MapOrdered(ctx, indices, parallel.Options{Workers: min(WorkerCount, len(indices))}, func(ctx context.Context, index int) (checkoutFacts, error) {
		b, err := g.Checkout(ctx, rows[index].BookingURL, rows[index].StartTime)
		if err != nil {
			return checkoutFacts{}, err
		}
		return parseCheckout(b)
	})
	if err != nil {
		return err
	}
	for i, f := range facts {
		index := indices[i]
		r := &rows[index]
		end, err := exactEnd(r.StartTime, f)
		if err != nil {
			return err
		}
		r.EndTime = end
		if f.hasEnd {
			summary.ExactEnds++
		}
		if r.Room == "" {
			r.Room = f.room
		}
	}
	summary.Checkouts = len(indices)
	return ctx.Err()
}
