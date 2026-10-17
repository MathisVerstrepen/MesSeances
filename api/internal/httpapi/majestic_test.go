package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/synccontrol"
	"messeances/api/internal/syncschedule"
)

func TestMajesticPublicReadPreservesCalendarSourceAndFirstPart(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 10, 17, 0, 30, 0, 0, loc)
	r := schedule.ShowtimeRecord{Provider: schedule.ProviderMajestic, ID: "majestic-showing-emsx070000000001", ProviderShowingID: "emsx070000000001", TheaterID: "majestic-EMS0700", ServiceDate: "2026-10-17", Movie: schedule.MovieRecord{Provider: schedule.ProviderMajestic, ProviderID: "ABCDE", Slug: "majestic-film-ABCDE", Title: "Film", PublicMovieID: 1, RuntimeMinutes: 100}, StartTime: start, EndTime: start.Add(110 * time.Minute), FirstPartDurationMinutes: 10, Language: schedule.LanguageVOSTFR, ProviderVersion: "VOSTF", Format: schedule.FormatDolby, Room: "Salle source", BookingURL: "https://www.ticketingcine.com/EMS0700/FRABCDE#showsession?id=emsx070000000001&ps=ticketingcine"}
	h := newProviderReadHandler(t, schedule.ProviderCombined, r, []schedule.TheaterRecord{{Provider: schedule.ProviderMajestic, ID: r.TheaterID, ProviderID: "EMS0700", Slug: r.TheaterID, Name: "Source Cinema", Address: "1 rue", City: "Ville", PostalCode: "60000", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}, 100, 0)
	response := performRequest(t, h, "/api/v1/theaters?chain=majestic")
	var theaters []schedule.TheaterCatalogItem
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &theaters) != nil || len(theaters) != 1 || theaters[0].Provider != schedule.ProviderMajestic {
		t.Fatal("directory", response.Body.String())
	}
	response = performRequest(t, h, "/api/v1/movies/"+r.Movie.Slug+"/showtimes?date="+r.ServiceDate)
	var detail schedule.MovieSchedule
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &detail) != nil || len(detail.Theaters) != 1 || len(detail.Theaters[0].Showtimes) != 1 {
		t.Fatal("calendar lost", response.Body.String())
	}
	got := detail.Theaters[0].Showtimes[0]
	if got.Provider != schedule.ProviderMajestic || !got.EndTime.Equal(r.EndTime) || got.Room != r.Room || got.BookingURL == nil || *got.BookingURL != r.BookingURL {
		t.Fatal("source/end changed", got)
	}
}

func TestMajesticAdminSyncAndScheduleDispatch(t *testing.T) {
	c := &fakeSyncController{status: synccontrol.Status{ID: "1", Target: synccontrol.TargetMajestic, State: synccontrol.StateRunning}}
	h := syncAdminHandler(t, c)
	path := "/api/v1/admin/syncs/majestic"
	origin := "http://localhost:3000"
	assertAPIError(t, adminRequest(h, http.MethodPost, path, "", origin, nil), http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	cookie := loginAdmin(t, h, "password")
	assertAPIError(t, adminRequest(h, http.MethodPost, path, "", "https://evil.example", cookie), http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")
	if r := adminRequest(h, http.MethodPost, path, "", origin, cookie); r.Code != 202 || len(c.started) != 1 || c.started[0] != synccontrol.TargetMajestic || !strings.Contains(r.Body.String(), `"target":"majestic"`) {
		t.Fatal("admin target", r.Body.String())
	}
	s := &fakeSyncScheduleController{now: time.Now(), available: []syncschedule.Target{syncschedule.TargetVeo, syncschedule.TargetMajestic, syncschedule.TargetMetadataRefresh}}
	h = syncScheduleAdminHandler(t, s)
	cookie = loginAdmin(t, h, "password")
	path = "/api/v1/admin/sync-schedules/majestic"
	body := `{"enabled":false,"schedule":{"kind":"daily","time":"10:00"}}`
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{{"POST", path, body, 201}, {"PUT", path + "/41", body, 200}, {"DELETE", path + "/41", "", 204}} {
		r := adminRequest(h, tc.method, tc.path, tc.body, origin, cookie)
		if r.Code != tc.status || s.lastTarget != syncschedule.TargetMajestic {
			t.Fatal("schedule target", r.Code, s.lastTarget)
		}
	}
}
