package noecinemas

import (
	"context"
	"html"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"messeances/api/internal/schedule"
)

const roomBudget = 20 * time.Minute
const roomLimit = 4096
const roomWorkers = 4

var roomMarker = regexp.MustCompile(`"auditorium_showtime"\s*:\s*"salle-([1-9][0-9]*)"`)

func parseRoom(body []byte) string {
	if len(body) > MaxHTMLBytes {
		return ""
	}
	// Only ASCII quote escapes/entities are interpreted, never JavaScript.
	text := strings.ReplaceAll(html.UnescapeString(string(body)), `\"`, `"`)
	matches := roomMarker.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 || len(matches) != strings.Count(text, `"auditorium_showtime"`) {
		return ""
	}
	room := matches[0][1]
	if len(room) > 20 {
		return ""
	}
	for _, m := range matches {
		if m[1] != room {
			return ""
		}
	}
	return "Salle " + room
}

// Optional page failures and room-budget expiry preserve unknown rooms. Parent
// cancellation or a provider challenge still fails the complete acquisition.
func enrichRooms(parent context.Context, g Getter, records []schedule.ShowtimeRecord, summary *SyncSummary, budget time.Duration, limit int) error {
	indices := map[string][]int{}
	for i, r := range records {
		if r.Room == "" && r.TheaterID == "noecinemas-P8088" && schedule.ValidNoeCinemasBookingURL(r.BookingURL, "P8088") {
			indices[r.BookingURL] = append(indices[r.BookingURL], i)
		}
	}
	keys := make([]string, 0, len(indices))
	for key := range indices {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	summary.RoomsUnresolved = len(keys)
	if len(keys) == 0 {
		return parent.Err()
	}
	if len(keys) > limit {
		summary.RoomsBudgetSkipped = len(keys) - limit
		keys = keys[:limit]
	}
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	var mu sync.Mutex
	var fatal error
	next := 0
	var workers sync.WaitGroup
	for i := 0; i < min(roomWorkers, len(keys)); i++ {
		workers.Go(func() {
			for {
				mu.Lock()
				if ctx.Err() != nil || next == len(keys) {
					mu.Unlock()
					return
				}
				key := keys[next]
				next++
				summary.RoomsAttempted++
				mu.Unlock()
				body, err := g.Get(ctx, OperationRoom, key)
				if fatalRequest(err) {
					mu.Lock()
					if fatal == nil {
						fatal = err
					}
					mu.Unlock()
					cancel()
					return
				}
				if err != nil {
					continue
				}
				room := parseRoom(body)
				if room == "" {
					continue
				}
				mu.Lock()
				for _, index := range indices[key] {
					records[index].Room = room
				}
				summary.RoomsRecovered++
				summary.RoomsUnresolved--
				mu.Unlock()
			}
		})
	}
	workers.Wait()
	summary.RoomsBudgetSkipped += len(keys) - next
	if fatal != nil {
		return fatal
	}
	return parent.Err()
}
