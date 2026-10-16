package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestCinewestPublishedEndPublicAPI(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 9, 14, 19, 0, 0, 123456000, loc)
	id, _ := schedule.CinewestShowingID("cineoffice-royanlelido", "1")
	r := schedule.ShowtimeRecord{Provider: schedule.ProviderCinewest, ID: "cinewest-showing-" + id, ProviderShowingID: id, TheaterID: "cinewest-cineoffice-royanlelido", ServiceDate: "2026-09-14", Movie: schedule.MovieRecord{Provider: schedule.ProviderCinewest, ProviderID: "cineoffice-1", Slug: "cinewest-film-cineoffice-1", Title: "Silent", PublicMovieID: 1, RuntimeMinutes: 90}, StartTime: start, EndTime: start.Add(187 * time.Minute), ProviderVersion: "VERSION_MUET", Format: schedule.FormatScreenX, Room: "Salle 1", BookingURL: "https://www.cine-royan.com/"}
	handler := newProviderReadHandler(t, schedule.ProviderCinewest, r, []schedule.TheaterRecord{{Provider: schedule.ProviderCinewest, ID: r.TheaterID, ProviderID: "cineoffice-royanlelido", Slug: r.TheaterID, Name: "Le Lido", Address: "Place de la Gare", City: "Royan", PostalCode: "17200", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}}}, 123, 42)
	response := performRequest(t, handler, "/api/v1/theaters?chain=cinewest")
	var theaters []schedule.TheaterCatalogItem
	if json.Unmarshal(response.Body.Bytes(), &theaters) != nil || response.Code != http.StatusOK || len(theaters) != 1 || theaters[0].Provider != schedule.ProviderCinewest {
		t.Fatal("Cinewest chain filter")
	}
	for _, slug := range []string{"film-1", r.Movie.Slug} {
		response := performRequest(t, handler, "/api/v1/movies/"+slug+"/showtimes?date=2026-09-14")
		var detail schedule.MovieSchedule
		if json.Unmarshal(response.Body.Bytes(), &detail) != nil || response.Code != http.StatusOK || len(detail.Theaters) != 1 || len(detail.Theaters[0].Showtimes) != 1 {
			t.Fatal("Cinewest durable alias")
		}
		got := detail.Theaters[0].Showtimes[0]
		if got.Provider != schedule.ProviderCinewest || got.Language != "" || !got.EndTime.Equal(r.EndTime) || got.EstimatedEndTime != nil || got.Movie.RuntimeMinutes != 123 || got.BookingURL == nil || *got.BookingURL != r.BookingURL {
			t.Fatal("published end changed by API enrichment")
		}
	}
	timeline := performRequest(t, handler, "/api/v1/timeline?date=2026-09-14&theaters="+r.TheaterID)
	if timeline.Code != http.StatusOK || !strings.Contains(timeline.Body.String(), `"duration_minutes":187`) {
		t.Fatal("planning ignores published end")
	}
	for _, query := range []string{"&finish_before=22:00", "&finish_before=23:00&language=VF", "&finish_before=23:00&format=3D"} {
		response := performRequest(t, handler, "/api/v1/search/slot?theaters="+r.TheaterID+"&date=2026-09-14&start_after=18:00"+query)
		var results []schedule.SlotResult
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &results) != nil || len(results) != 0 {
			t.Fatal("end/language/format filter", query)
		}
	}
}
