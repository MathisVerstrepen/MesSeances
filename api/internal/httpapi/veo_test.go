package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestVeoPublicReadAPIsPreserveExactAndUnknownEnds(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 10, 15, 19, 0, 0, 0, loc)
	for _, exact := range []bool{false, true} {
		id := schedule.VeoShowingID("P1028", "opaque")
		r := schedule.ShowtimeRecord{Provider: schedule.ProviderVeo, ID: "veo-showing-" + id, ProviderShowingID: id, TheaterID: "veo-P1028", ServiceDate: "2026-10-15", Movie: schedule.MovieRecord{Provider: schedule.ProviderVeo, ProviderID: "cEvent_1", Slug: "veo-film-cEvent_1", Title: "Event", PublicMovieID: 1, RuntimeMinutes: 120}, StartTime: start, EndTime: start, Language: schedule.LanguageVOSTFR, ProviderVersion: "VOSTF", Format: schedule.Format3D, BookingURL: "https://achat.veocinemas.fr/fixture/r/123"}
		if exact {
			r.EndTime = start.Add(80 * time.Minute)
		}
		h := newProviderReadHandler(t, schedule.ProviderCombined, r, []schedule.TheaterRecord{{Provider: schedule.ProviderVeo, ID: r.TheaterID, ProviderID: "P1028", Slug: r.TheaterID, Name: "Véo", Address: "1 rue", City: "Paris", PostalCode: "75001", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}, 120, 0)
		response := performRequest(t, h, "/api/v1/theaters?chain=veo")
		var theaters []schedule.TheaterCatalogItem
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &theaters) != nil || len(theaters) != 1 || theaters[0].Provider != schedule.ProviderVeo {
			t.Fatal(response.Body.String())
		}
		for _, slug := range []string{"film-1", r.Movie.Slug} {
			response := performRequest(t, h, "/api/v1/movies/"+slug+"/showtimes?date="+r.ServiceDate)
			var detail schedule.MovieSchedule
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &detail) != nil || len(detail.Theaters) != 1 || len(detail.Theaters[0].Showtimes) != 1 {
				t.Fatal(response.Body.String())
			}
			got := detail.Theaters[0].Showtimes[0]
			if got.Provider != schedule.ProviderVeo || !got.EndTime.Equal(r.EndTime) || got.Room != "" || got.BookingURL == nil || *got.BookingURL != r.BookingURL {
				t.Fatal(got)
			}
		}
		timeline := performRequest(t, h, "/api/v1/timeline?date="+r.ServiceDate+"&theaters=veo-P1028")
		if timeline.Code != http.StatusOK || !strings.Contains(timeline.Body.String(), `"provider":"veo"`) {
			t.Fatal(timeline.Body.String())
		}
	}
}
