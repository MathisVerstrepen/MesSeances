package veo

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func collisionSession(t *testing.T, id, start, room, booking string) showtimeResponse {
	t.Helper()
	var s showtimeResponse
	if err := json.Unmarshal([]byte(fixtureSession(id, start, room)), &s); err != nil {
		t.Fatal(err)
	}
	s.Data.Ticketing[0].URLs = []string{"https://achat.veocinemas.fr/saint-chamond/r/" + booking}
	return s
}

func parseCollisionSchedule(t *testing.T, days map[string]map[string][]showtimeResponse) ([]schedule.ShowtimeRecord, error) {
	t.Helper()
	p := map[string][]string{}
	movies := map[string]schedule.MovieRecord{}
	for id, dates := range days {
		movies[id] = schedule.MovieRecord{Provider: schedule.ProviderVeo, ProviderID: id, Title: "Synthetic movie", Slug: "veo-film-" + id}
		for date := range dates {
			p[id] = append(p[id], date)
		}
	}
	body, err := json.Marshal(scheduleResponse{"P0833": {Schedule: days}})
	if err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err := parseSchedule(body, cinema{ID: "P0833"}, p, movies, loc, "", "2026-10-17", "2026-12-31")
	return rows, err
}

func TestScheduleRoomCollisionsPreserveStableIdentities(t *testing.T) {
	days := map[string][]showtimeResponse{}
	want := map[string]string{}
	for _, pair := range []struct{ date, roomA, roomB, bookingA, bookingB string }{
		{"2026-11-20", "Salle 3", "Salle 5", "77172", "77173"},
		{"2026-12-01", "Salle 3", "Salle 5", "77183", "77184"},
		{"2026-12-08", "Salle 4", "Salle 3", "77192", "77191"},
	} {
		id := "synthetic-room-blind-" + pair.date
		a := collisionSession(t, id, pair.date+"T20:00:00", pair.roomA, pair.bookingA)
		b := collisionSession(t, id, pair.date+"T20:00:00", pair.roomB, pair.bookingB)
		days[pair.date] = []showtimeResponse{a, b, a, b}
		for _, s := range []showtimeResponse{a, b} {
			booking := s.Data.Ticketing[0].URLs[0]
			want[schedule.VeoShowingID("P0833", id+"\x00"+booking)] = s.Screen.Name + "\x00" + booking
		}
	}
	ordinary := collisionSession(t, "ordinary", "2026-11-21T20:00:00", "Salle 1", "77174")
	days["2026-11-21"] = []showtimeResponse{ordinary, ordinary}
	want[schedule.VeoShowingID("P0833", "ordinary")] = "Salle 1\x00" + ordinary.Data.Ticketing[0].URLs[0]
	for range 2 {
		rows, err := parseCollisionSchedule(t, map[string]map[string][]showtimeResponse{"300279": days})
		if err != nil || len(rows) != 7 {
			t.Fatal("collision members dropped or duplicates retained", len(rows), err)
		}
		got := map[string]string{}
		for _, row := range rows {
			if row.ID != "veo-showing-"+row.ProviderShowingID || !schedule.ValidVeoBookingURL(row.BookingURL, "P0833", row.ProviderShowingID) || !row.EndTime.Equal(row.StartTime) {
				t.Fatal("identity or unknown end invalid")
			}
			got[row.ProviderShowingID] = row.Room + "\x00" + row.BookingURL
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal("identities, rooms or bookings changed with order")
		}
		for _, sessions := range days {
			slices.Reverse(sessions)
		}
	}
}

func TestScheduleRoomCollisionsFailClosed(t *testing.T) {
	for _, kind := range []string{"same room", "same booking", "missing room", "time", "date", "movie", "version", "format", "invalid booking"} {
		t.Run(kind, func(t *testing.T) {
			a := collisionSession(t, "same-id", "2026-11-20T20:00:00", "Salle 3", "77172")
			b := collisionSession(t, "same-id", "2026-11-20T20:00:00", "Salle 5", "77173")
			days := map[string]map[string][]showtimeResponse{"300279": {"2026-11-20": {a}}}
			date, movie := "2026-11-20", "300279"
			switch kind {
			case "same room":
				b.Screen.Name = a.Screen.Name
			case "same booking":
				b.Data.Ticketing[0].URLs = a.Data.Ticketing[0].URLs
			case "missing room":
				b.Screen = nil
			case "time":
				b.StartsAt = "2026-11-20T21:00:00"
			case "date":
				date, b.StartsAt = "2026-11-21", "2026-11-21T20:00:00"
			case "movie":
				movie = "300280"
				days[movie] = map[string][]showtimeResponse{}
			case "version":
				b.Tags = []string{"Localization.Language.French", "Format.Projection.3D"}
			case "format":
				b.Tags = []string{"Localization.Version.Original"}
			case "invalid booking":
				b.Data.Ticketing[0].URLs[0] = strings.ReplaceAll(b.Data.Ticketing[0].URLs[0], "https:", "http:")
			}
			days[movie][date] = append(days[movie][date], b)
			if rows, err := parseCollisionSchedule(t, days); !errors.Is(err, errShape) || rows != nil {
				t.Fatal("malformed collision admitted", err)
			}
		})
	}
}
