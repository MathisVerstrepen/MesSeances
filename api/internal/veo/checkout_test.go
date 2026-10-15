package veo

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestEnrichCheckoutsBindsSourceSessionStart(t *testing.T) {
	for _, epoch := range []string{"1792065000", "1792065001"} {
		t.Run(epoch, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					r := fixtureResponse(307, "", "text/html")
					r.Header.Set("Location", "https://achat.veocinemas.fr/andernos/reserver/F604912/D"+epoch+"/VF/271793/")
					return r, nil
				}
				return fixtureResponse(200, `<div id=s_fin>fin prévue à 15h45</div>{"auditorium_showtime":"salle-2"}`, "text/html"), nil
			})
			loc, _ := time.LoadLocation(schedule.Timezone)
			start := time.Unix(1792065000, 0).In(loc)
			rows := []schedule.ShowtimeRecord{{TheaterID: "veo-W3351", StartTime: start, EndTime: start, BookingURL: "https://achat.veocinemas.fr/andernos/r/55880", Room: "API room"}}
			summary := SyncSummary{}
			err := enrichCheckouts(t.Context(), c, rows, &summary, start.Add(-time.Minute))
			if epoch == "1792065000" {
				if err != nil || !rows[0].EndTime.After(start) || rows[0].Room != "API room" || calls != 2 || summary.Checkouts != 1 {
					t.Fatal("bound enrichment failed", err, calls)
				}
			} else if err == nil || !rows[0].EndTime.Equal(start) || summary.Checkouts != 0 || calls != 1 {
				t.Fatal("wrong session destination was used", err, calls)
			}
		})
	}
}

func TestCheckoutScopedClockCharsetAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid, end bool
		room       string
	}{
		{"scoped", `<aside>fin prévue à 22h22</aside><div class="s_fin">fin prévue à <b>00h15</b></div><script>{"auditorium_showtime":"salle-12"}</script>`, true, true, "Salle 12"},
		{"latin1", "<meta charset=iso-8859-1><div id=s_fin>fin pr\xe9vue \xe0 00h15</div><script>{\"auditorium_showtime\":\"salle-2\"}</script>", true, true, "Salle 2"},
		{"unknown", `<div>fin prévue à 22h22</div>{"auditorium_showtime":null}`, true, false, ""},
		{"missing identity", `<html>maintenance</html>`, false, false, ""},
		{"malformed clock", `<div id=s_fin>fin prévue à 29h22</div>{"auditorium_showtime":null}`, false, false, ""},
		{"conflicting", `<div id=s_fin>fin prévue à 21h00</div><div class=s_fin>fin prévue à 22h00</div>{"auditorium_showtime":null}`, false, false, ""},
		{"malformed room", `{"auditorium_showtime":"salle-0"}`, false, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parseCheckout([]byte(tc.body))
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
			if tc.valid && (f.hasEnd != tc.end || f.room != tc.room) {
				t.Fatalf("%+v", f)
			}
		})
	}
	if _, err := parseCheckout([]byte(strings.Repeat("x", MaxHTMLBytes+1))); err == nil {
		t.Fatal("oversized html")
	}
}
func TestExactEndMidnightAndDST(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	for _, tc := range []struct {
		start        string
		hour, minute int
		end          string
		valid        bool
	}{
		{"2026-10-15T23:30:00+02:00", 1, 15, "2026-10-16T01:15:00+02:00", true},
		{"2026-03-28T23:30:00+01:00", 3, 30, "2026-03-29T03:30:00+02:00", true},
		{"2026-03-28T23:30:00+01:00", 2, 30, "", false},
		{"2026-10-24T23:30:00+02:00", 2, 30, "", false},
	} {
		start, _ := time.Parse(time.RFC3339, tc.start)
		end, err := exactEnd(start.In(loc), checkoutFacts{hasEnd: true, hour: tc.hour, minute: tc.minute})
		if (err == nil) != tc.valid || tc.valid && end.Format(time.RFC3339) != tc.end {
			t.Fatal(tc, end, err)
		}
		unknown, err := exactEnd(start.In(loc), checkoutFacts{})
		if err != nil || !unknown.Equal(start) {
			t.Fatal(unknown, err)
		}
	}
	for _, raw := range []string{"2026-03-29T02:30:00", "2026-10-25T02:30:00", "2026-10-15T20:00:00Z"} {
		if _, err := parseStart(raw, loc); err == nil {
			t.Fatal(raw)
		}
	}
}
