package veo

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

func saintAffriqueTestSource(start time.Time) string {
	q := url.Values{"code_theater": {"P0793"}, "gid_entity": {"synthetic_123.opaque"}, "tags": {"Localization.Language.French,Format.Projection.Digital"}, "perfd": {start.Format("20060102")}, "perft": {start.Format("15:04")}, "code": {"VF"}}
	return "https://relay.mvtx.us/ticketing/dbz?" + q.Encode()
}

func saintAffriqueTestRoot(start time.Time) string {
	return fmt.Sprintf("https://relay.mvtx.us/?house_id=12&movie_id=34&perfd=%s&perft=%s&code=VF", start.Format("20060102"), start.Format("15:04"))
}

func TestSaintAffriqueRelayUnavailablePreservesSessions(t *testing.T) {
	start := gueretTestStart()
	source, root := saintAffriqueTestSource(start), saintAffriqueTestRoot(start)
	noweb := strings.Replace(root, "https://relay.mvtx.us/", "/noweb", 1)
	calls := 0
	var bodies []*closeUnreadBody
	c := testClient(t, func(req *http.Request) (*http.Response, error) {
		expected := []string{source, root}
		if calls >= len(expected) || req.URL.String() != expected[calls] || req.Method != http.MethodGet || req.Header.Get("Cookie") != "" {
			t.Fatal("unapproved request")
		}
		target := strings.TrimPrefix(root, "https://relay.mvtx.us")
		if calls == 1 {
			target = noweb
		}
		calls++
		r := fixtureResponse(302, "", "text/html")
		body := &closeUnreadBody{}
		bodies = append(bodies, body)
		r.Body = body
		r.Header.Set("Location", target)
		r.Header.Add("Set-Cookie", "ignored=synthetic; Path=/")
		return r, nil
	})
	row := retryTestRow(1)
	row.TheaterID = "veo-P0793"
	row.ProviderShowingID = schedule.VeoShowingID("P0793", "synthetic")
	row.ID = "veo-showing-" + row.ProviderShowingID
	row.StartTime = start
	row.EndTime = start
	row.BookingURL = source
	row.Room = "API room"
	rows := []schedule.ShowtimeRecord{row}
	s := SyncSummary{}
	if err := enrichCheckouts(t.Context(), c, rows, &s, start.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || s.Checkouts != 1 || s.ExactEnds != 0 || !reflect.DeepEqual(rows[0], row) {
		t.Fatal("unavailable changed session or facts", calls, s)
	}
	for _, b := range bodies[1:] {
		if b.read || !b.closed {
			t.Fatal("relay body read or not closed")
		}
	}
}

type closeUnreadBody struct{ read, closed bool }

func (b *closeUnreadBody) Read([]byte) (int, error) {
	b.read = true
	return 0, errors.New("body must remain unread")
}
func (b *closeUnreadBody) Close() error { b.closed = true; return nil }

func TestSaintAffriqueRelayRejectsUnboundResponses(t *testing.T) {
	start := gueretTestStart()
	source, root := saintAffriqueTestSource(start), saintAffriqueTestRoot(start)
	noweb := strings.Replace(root, "https://relay.mvtx.us/", "/noweb", 1)
	for _, tc := range []struct {
		name        string
		hop, status int
		target      string
		duplicate   bool
	}{
		{"source page", 0, 200, "", false}, {"source status", 0, 307, strings.TrimPrefix(root, "https://relay.mvtx.us"), false},
		{"wrong day", 0, 302, strings.TrimPrefix(strings.Replace(root, "20261016", "20261017", 1), "https://relay.mvtx.us"), false},
		{"wrong code", 0, 302, strings.TrimPrefix(strings.Replace(root, "code=VF", "code=VO", 1), "https://relay.mvtx.us"), false},
		{"duplicate root key", 0, 302, strings.TrimPrefix(root, "https://relay.mvtx.us") + "&code=VF", false},
		{"root page", 1, 200, "", false}, {"root missing", 1, 404, "", false},
		{"root status", 1, 307, noweb, false}, {"absolute noweb", 1, 302, "https://relay.mvtx.us" + noweb, false},
		{"wrong house", 1, 302, strings.Replace(noweb, "house_id=12", "house_id=13", 1), false},
		{"wrong movie", 1, 302, strings.Replace(noweb, "movie_id=34", "movie_id=35", 1), false},
		{"wrong clock", 1, 302, strings.Replace(noweb, "15:00", "15:01", 1), false},
		{"wrong version", 1, 302, strings.Replace(noweb, "code=VF", "code=VO", 1), false},
		{"fragment", 1, 302, noweb + "#", false}, {"extra key", 1, 302, noweb + "&x=1", false},
		{"duplicate location", 1, 302, noweb, true}, {"external redirect", 1, 302, "https://example.com/", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(req *http.Request) (*http.Response, error) {
				hop := calls
				calls++
				if hop > tc.hop {
					t.Fatal("unapproved target sent")
				}
				status, target := 302, strings.TrimPrefix(root, "https://relay.mvtx.us")
				if hop == tc.hop {
					status, target = tc.status, tc.target
				}
				r := fixtureResponse(status, "<html>untrusted</html>", "text/html")
				if target != "" {
					r.Header.Set("Location", target)
				}
				if tc.duplicate && hop == tc.hop {
					r.Header.Add("Location", target)
				}
				return r, nil
			})
			result, err := c.Checkout(t.Context(), source, start)
			var requestErr *RequestError
			if err == nil || !errors.As(err, &requestErr) || result.unavailableEnrichment || calls != tc.hop+1 {
				t.Fatal("unsafe outcome accepted", calls, err)
			}
		})
	}
	for _, badStart := range []time.Time{start.Add(time.Minute), start.Add(time.Second), start.Add(time.Nanosecond)} {
		c := testClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("misbound source sent"); return nil, nil })
		if _, err := c.Checkout(t.Context(), source, badStart); err == nil {
			t.Fatal("misbound source accepted")
		}
	}
	// Blocking and transport errors keep their shared classification.
	for _, status := range []int{403, 429, 503} {
		c := testClient(t, func(*http.Request) (*http.Response, error) { return fixtureResponse(status, "", "text/html"), nil })
		_, err := c.Checkout(t.Context(), source, start)
		var failure *RequestError
		want := syncproxy.FailureChallenge
		if status == 503 {
			want = syncproxy.FailureServer
		}
		if !errors.As(err, &failure) || failure.Kind != want {
			t.Fatal("failure classification changed", err)
		}
	}
}

func TestSaintAffriqueSourceBindsSessionAndKeepsOriginalBooking(t *testing.T) {
	start := gueretTestStart()
	loc := start.Location()
	source := saintAffriqueTestSource(start)
	s := collisionSession(t, "synthetic", start.Format("2006-01-02T15:04:05"), "API room", "1")
	s.Tags = []string{"Localization.Language.French", "Format.Projection.Digital"}
	s.Data.Ticketing[0].Provider = "relay"
	s.Data.Ticketing[0].URLs = []string{source}
	c := cinema{ID: "P0793"}
	m := schedule.MovieRecord{Provider: schedule.ProviderVeo, ProviderID: "123", Title: "Synthetic"}
	row, err := parseShowtime(s, c, m, start.Format(time.DateOnly), loc)
	if err != nil || row.BookingURL != source || row.TheaterID != "veo-P0793" || row.Room != "API room" || !row.EndTime.Equal(start) {
		t.Fatal("original showing lost", err)
	}
	for _, bad := range []string{strings.Replace(source, "20261016", "20261017", 1), strings.Replace(source, "15%3A00", "15%3A01", 1), strings.Replace(source, "code=VF", "code=VO", 1), strings.Replace(source, "P0793", "P1394", 1), strings.Replace(source, "Localization.Language.French", "Localization.Version.Original", 1)} {
		s.Data.Ticketing[0].URLs = []string{bad}
		if _, err := parseShowtime(s, c, m, start.Format(time.DateOnly), loc); !errors.Is(err, errShape) {
			t.Fatal("misbound relay accepted")
		}
	}
	s.Data.Ticketing[0].URLs = []string{source}
	c.ID = "P1028"
	if _, err := parseShowtime(s, c, m, start.Format(time.DateOnly), loc); err == nil {
		t.Fatal("cross-cinema relay accepted")
	}
}
