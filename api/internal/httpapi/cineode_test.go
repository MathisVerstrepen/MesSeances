package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/synccontrol"
	"messeances/api/internal/syncschedule"
)

func TestCineodePublicCalendarWebsiteAndUnknownMetadata(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 10, 17, 0, 15, 0, 0, loc)
	key := "991-atelier-local-" + strconv.FormatInt(start.Unix(), 10)
	r := schedule.ShowtimeRecord{Provider: schedule.ProviderCineode, ID: "cineode-showing-" + key, ProviderShowingID: key, TheaterID: "cineode-991", ServiceDate: "2026-10-17", Movie: schedule.MovieRecord{Provider: schedule.ProviderCineode, ProviderID: "atelier-local", Slug: "cineode-film-atelier-local", Title: "L'été", PublicMovieID: 1}, StartTime: start, EndTime: start, Language: schedule.LanguageVOSTFR, ProviderVersion: "VOSTF", Format: schedule.Format2D, BookingURL: schedule.CineodeWebsite("991")}
	for _, runtime := range []int{0, 100} {
		h := newProviderReadHandler(t, schedule.ProviderCombined, r, []schedule.TheaterRecord{{Provider: schedule.ProviderCineode, ID: r.TheaterID, ProviderID: "991", Slug: r.TheaterID, Name: "Le Don Camillo", City: "Montmirail", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}, runtime, 42)
		response := performRequest(t, h, "/api/v1/theaters?chain=cineode")
		var theaters []schedule.TheaterCatalogItem
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &theaters) != nil || len(theaters) != 1 || theaters[0].Provider != schedule.ProviderCineode || theaters[0].Address != "" || theaters[0].PostalCode != "" {
			t.Fatal("directory metadata", response.Body.String())
		}
		response = performRequest(t, h, "/api/v1/movies/"+r.Movie.Slug+"/showtimes?date="+r.ServiceDate)
		var detail schedule.MovieSchedule
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &detail) != nil || len(detail.Theaters) != 1 || len(detail.Theaters[0].Showtimes) != 1 {
			t.Fatal("calendar lost", response.Body.String())
		}
		got := detail.Theaters[0].Showtimes[0]
		if got.Provider != schedule.ProviderCineode || got.ID != r.ID || !got.EndTime.Equal(start) || got.Room != "" || got.BookingURL == nil || *got.BookingURL != r.BookingURL || (runtime == 0) != (got.EstimatedEndTime == nil) {
			t.Fatal("source end/website/estimate changed", got)
		}
	}
}

func TestCineodeAdminSyncAndScheduleDispatch(t *testing.T) {
	c := &fakeSyncController{status: synccontrol.Status{ID: "1", Target: synccontrol.TargetCineode, State: synccontrol.StateRunning}}
	h := syncAdminHandler(t, c)
	path := "/api/v1/admin/syncs/cineode"
	origin := "http://localhost:3000"
	assertAPIError(t, adminRequest(h, http.MethodPost, path, "", origin, nil), http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	cookie := loginAdmin(t, h, "password")
	assertAPIError(t, adminRequest(h, http.MethodPost, path, "", "https://evil.example", cookie), http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")
	if r := adminRequest(h, http.MethodPost, path, "", origin, cookie); r.Code != 202 || len(c.started) != 1 || c.started[0] != synccontrol.TargetCineode || !strings.Contains(r.Body.String(), `"target":"cineode"`) {
		t.Fatal("admin target", r.Body.String())
	}
	s := &fakeSyncScheduleController{now: time.Now(), available: []syncschedule.Target{syncschedule.TargetMajestic, syncschedule.TargetCineode, syncschedule.TargetMetadataRefresh}}
	h = syncScheduleAdminHandler(t, s)
	cookie = loginAdmin(t, h, "password")
	path = "/api/v1/admin/sync-schedules/cineode"
	body := `{"enabled":false,"schedule":{"kind":"daily","time":"10:00"}}`
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{{"POST", path, body, 201}, {"PUT", path + "/41", body, 200}, {"DELETE", path + "/41", "", 204}} {
		r := adminRequest(h, tc.method, tc.path, tc.body, origin, cookie)
		if r.Code != tc.status || s.lastTarget != syncschedule.TargetCineode {
			t.Fatal("schedule dispatch", r.Code, s.lastTarget)
		}
	}
}
