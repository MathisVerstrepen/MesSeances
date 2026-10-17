package cinemovida

import (
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"messeances/api/internal/parallel"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

var endClock = regexp.MustCompile(`(?i)fin\s+prévue\s+à\s+([0-9]{1,2})h([0-5][0-9])\b`)

// Only a single unambiguous source s_fin marker supplies a canonical clock.
func parseEnd(body []byte, start time.Time) (time.Time, bool) {
	if len(body) == 0 || len(body) > MaxHTMLBytes {
		return time.Time{}, false
	}
	if !utf8.Valid(body) {
		r, err := charset.NewReader(bytes.NewReader(body), "")
		if err != nil {
			return time.Time{}, false
		}
		body, err = io.ReadAll(io.LimitReader(r, MaxHTMLBytes+1))
		if err != nil || len(body) > MaxHTMLBytes {
			return time.Time{}, false
		}
	}
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return time.Time{}, false
	}
	var text func(*html.Node) string
	text = func(n *html.Node) string {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "template") {
			return ""
		}
		if n.Type == html.TextNode {
			return n.Data
		}
		var b strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			b.WriteString(text(c))
			b.WriteByte(' ')
		}
		return b.String()
	}
	var blocks []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "script" || n.Data == "style" || n.Data == "template" {
				return
			}
			marker := false
			for _, a := range n.Attr {
				if a.Key == "id" && a.Val == "s_fin" {
					marker = true
				}
				if a.Key == "class" {
					for _, v := range strings.Fields(a.Val) {
						if v == "s_fin" {
							marker = true
						}
					}
				}
			}
			if marker {
				blocks = append(blocks, strings.Join(strings.Fields(text(n)), " "))
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if len(blocks) != 1 {
		return time.Time{}, false
	}
	matches := endClock.FindAllStringSubmatch(blocks[0], -1)
	if len(matches) != 1 {
		return time.Time{}, false
	}
	h, _ := strconv.Atoi(matches[0][1])
	m, _ := strconv.Atoi(matches[0][2])
	if h > 23 {
		return time.Time{}, false
	}
	return exactEnd(start, h, m)
}

// Enumerate actual instants, not time.Date's normalization of nonexistent or
// repeated wall clocks. Reject ambiguity and roll over only to the next date.
func exactEnd(start time.Time, hour, minute int) (time.Time, bool) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return time.Time{}, false
	}
	start = start.In(location)
	for day := 0; day < 2; day++ {
		date := start.AddDate(0, 0, day).Format(time.DateOnly)
		var candidates []time.Time
		base, _ := time.Parse(time.DateOnly, date)
		for t := base.Add(-2 * time.Hour); t.Before(base.Add(27 * time.Hour)); t = t.Add(time.Minute) {
			local := t.In(location)
			if local.Format(time.DateOnly) == date && local.Hour() == hour && local.Minute() == minute && local.After(start) {
				candidates = append(candidates, local)
			}
		}
		if len(candidates) == 1 {
			return candidates[0], true
		}
		if len(candidates) > 1 {
			return time.Time{}, false
		}
		// A gap on the same calendar day is not evidence for tomorrow's clock.
		if day == 0 && (hour > start.Hour() || hour == start.Hour() && minute > start.Minute()) {
			return time.Time{}, false
		}
	}
	return time.Time{}, false
}

func enrichEnds(ctx context.Context, g Getter, records []schedule.ShowtimeRecord, summary *SyncSummary, budget time.Duration, limit int) error {
	work, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	indices := make([]int, 0, min(limit, len(records)))
	for i := range records {
		if len(indices) == limit {
			break
		}
		indices = append(indices, i)
	}
	type result struct {
		end       time.Time
		recovered bool
		attempted bool
	}
	// Each worker owns a distinct record index; MapOrdered joins all workers
	// before these outcomes are read, including on budget expiration.
	results := make([]result, len(indices))
	_, err := parallel.MapOrdered(work, indices, parallel.Options{Workers: 4}, func(ctx context.Context, i int) (struct{}, error) {
		if err := ctx.Err(); err != nil {
			return struct{}{}, err
		}
		results[i].attempted = true
		r := records[i]
		body, err := g.Checkout(ctx, r.BookingURL, r.StartTime)
		if err != nil {
			var requestErr *RequestError
			if errors.As(err, &requestErr) && requestErr.Kind == syncproxy.FailureChallenge {
				return struct{}{}, err
			}
			if ctx.Err() != nil {
				return struct{}{}, ctx.Err()
			}
			return struct{}{}, nil
		}
		end, ok := parseEnd(body, r.StartTime)
		results[i].end, results[i].recovered = end, ok
		return struct{}{}, nil
	})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	// Retain completed lookups on budget expiry. Missing source ends remain the
	// sentinel; parent cancellation and provider challenge stay fatal.
	for i, r := range results {
		if r.attempted {
			summary.EndsAttempted++
		}
		if r.recovered {
			records[indices[i]].EndTime = r.end
			summary.EndsRecovered++
		}
	}
	summary.EndsBudgetSkipped = len(records) - summary.EndsAttempted
	summary.EndsUnresolved = summary.EndsAttempted - summary.EndsRecovered
	return nil
}
