package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/synccontrol"
	"messeances/api/internal/syncschedule"
)

func TestCinemovidaPublicReadsCanonicalEstimatedEndsAndFinishBefore(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	for _, runtime := range []int{0, 120} {
		for _, exact := range []bool{false, true} {
			t.Run(fmt.Sprintf("runtime=%d/exact=%t", runtime, exact), func(t *testing.T) {
				start := time.Date(2026, 10, 17, 19, 0, 0, 0, loc)
				key := schedule.CinemovidaShowingID("P0176", "opaque")
				r := schedule.ShowtimeRecord{Provider: schedule.ProviderCinemovida, ID: "cinemovida-showing-" + key, ProviderShowingID: key, TheaterID: "cinemovida-P0176", ServiceDate: "2026-10-17", Movie: schedule.MovieRecord{Provider: schedule.ProviderCinemovida, ProviderID: "cEvent_1", Slug: "cinemovida-film-cEvent_1", Title: "Événement", PublicMovieID: 1}, StartTime: start, EndTime: start, Language: schedule.LanguageVFSTF, ProviderVersion: "VFSTF", Format: schedule.Format4DX, Room: "Salle 10 PREMIUM MX4D", BookingURL: "https://achat.cinema-castillet.com/reserver/r/123"}
				if exact {
					r.EndTime = start.Add(45 * time.Minute)
				}
				h := newProviderReadHandler(t, schedule.ProviderCombined, r, []schedule.TheaterRecord{{Provider: schedule.ProviderCinemovida, ID: r.TheaterID, ProviderID: "P0176", Slug: r.TheaterID, Name: "Castillet", Address: "1 rue", City: "Perpignan", PostalCode: "66000", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}, runtime, 0)
				response := performRequest(t, h, "/api/v1/theaters?chain=cinemovida")
				var theaters []schedule.TheaterCatalogItem
				if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &theaters) != nil || len(theaters) != 1 || theaters[0].Provider != schedule.ProviderCinemovida || theaters[0].Address != "1 rue" || theaters[0].PostalCode != "66000" {
					t.Fatal("directory metadata", response.Body.String())
				}
				for _, slug := range []string{"film-1", r.Movie.Slug} {
					response := performRequest(t, h, "/api/v1/movies/"+slug+"/showtimes?date="+r.ServiceDate)
					var detail schedule.MovieSchedule
					if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &detail) != nil || len(detail.Theaters) != 1 || len(detail.Theaters[0].Showtimes) != 1 {
						t.Fatal("source alias/calendar", response.Body.String())
					}
					got := detail.Theaters[0].Showtimes[0]
					if got.Provider != schedule.ProviderCinemovida || got.ID != r.ID || !got.EndTime.Equal(r.EndTime) || got.Room != r.Room || got.BookingURL == nil || *got.BookingURL != r.BookingURL || got.Format != r.Format || got.Language != r.Language {
						t.Fatal("source fields lost", got)
					}
					if (got.EstimatedEndTime != nil) != (!exact && runtime > 0) {
						t.Fatal("canonical/estimated end conflated", got)
					}
					if got.EstimatedEndTime != nil && (!got.EstimatedEndTime.Equal(start.Add(135*time.Minute)) || got.EstimatedEndAdsMinutes == nil || *got.EstimatedEndAdsMinutes != 15) {
						t.Fatal("response-only estimate incorrect")
					}
				}
				query := "?date=" + r.ServiceDate + "&theaters=" + r.TheaterID
				response = performRequest(t, h, "/api/v1/timeline"+query)
				if response.Code != 200 || !strings.Contains(response.Body.String(), `"provider":"cinemovida"`) {
					t.Fatal("timeline", response.Body.String())
				}
				end := start.Add(time.Duration(runtime) * time.Minute)
				if exact {
					end = r.EndTime
				}
				for _, early := range []bool{false, true} {
					finish := end
					if early {
						finish = finish.Add(-time.Minute)
					}
					response := performRequest(t, h, "/api/v1/search/slot"+query+"&start_after=18:00&buffer_ads=0&include_ads=true&finish_before="+finish.Format("15:04"))
					var slots []schedule.SlotResult
					if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &slots) != nil {
						t.Fatal("finish filter", response.Body.String())
					}
					want := 0
					if !early && (exact || runtime > 0) {
						want = 1
					}
					if len(slots) != want || want == 1 && !slots[0].EffectiveEndTime.Equal(end) {
						t.Fatal("finish-before ignored canonical/unknown/estimate", slots)
					}
				}
			})
		}
	}
}

func TestCinemovidaAdminManualAndScheduleDispatch(t *testing.T) {
	c := &fakeSyncController{status: synccontrol.Status{ID: "1", Target: synccontrol.TargetCinemovida, State: synccontrol.StateRunning}}
	h := syncAdminHandler(t, c)
	path := "/api/v1/admin/syncs/cinemovida"
	origin := "http://localhost:3000"
	assertAPIError(t, adminRequest(h, http.MethodPost, path, "", origin, nil), 401, "unauthorized", "Authentification requise.")
	cookie := loginAdmin(t, h, "password")
	assertAPIError(t, adminRequest(h, http.MethodPost, path, "", "https://evil.example", cookie), 403, "origin_forbidden", "Origine non autorisée.")
	if r := adminRequest(h, http.MethodPost, path, "", origin, cookie); r.Code != 202 || len(c.started) != 1 || c.started[0] != synccontrol.TargetCinemovida || !strings.Contains(r.Body.String(), `"target":"cinemovida"`) {
		t.Fatal("manual target", r.Body.String())
	}
	s := &fakeSyncScheduleController{now: time.Now(), available: []syncschedule.Target{syncschedule.TargetCineode, syncschedule.TargetCinemovida, syncschedule.TargetMetadataRefresh}}
	h = syncScheduleAdminHandler(t, s)
	cookie = loginAdmin(t, h, "password")
	path = "/api/v1/admin/sync-schedules/cinemovida"
	body := `{"enabled":false,"schedule":{"kind":"daily","time":"10:00"}}`
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{{"POST", path, body, 201}, {"PUT", path + "/41", body, 200}, {"DELETE", path + "/41", "", 204}} {
		r := adminRequest(h, tc.method, tc.path, tc.body, origin, cookie)
		if r.Code != tc.status || s.lastTarget != syncschedule.TargetCinemovida {
			t.Fatal("schedule dispatch", r.Code, s.lastTarget)
		}
	}
}
