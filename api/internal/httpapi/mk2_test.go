package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestMK2PublicCatalogAliasTimelineSlotAndSilentDTO(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 8, 15, 19, 0, 0, 0, loc)
	r := schedule.ShowtimeRecord{Provider: schedule.ProviderMK2, ID: "mk2-showing-0004-140350", ProviderShowingID: "0004-140350", TheaterID: "mk2-0004", ServiceDate: "2026-08-15", Movie: schedule.MovieRecord{Provider: schedule.ProviderMK2, ProviderID: "HO00006568", Slug: "mk2-film-HO00006568", Title: "Silent", PublicMovieID: 1, PosterURL: schedule.MK2PosterPrefix + "HO00006568"}, StartTime: start, EndTime: start, ProviderVersion: "Muet", Format: schedule.Format2D, BookingURL: schedule.MK2BookingPrefix + "0004&sessionId=140350"}
	d := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderCombined, Scope: schedule.ScopeAll, Timezone: schedule.Timezone, GeneratedAt: start.UTC(), Window: schedule.Window{From: r.ServiceDate, Through: r.ServiceDate}, Showtimes: []schedule.ShowtimeRecord{r}, Theaters: []schedule.TheaterRecord{
		{Provider: schedule.ProviderMK2, ID: r.TheaterID, ProviderID: "0004", Slug: r.TheaterID, Name: "MK2 Bibliothèque", Address: "128 avenue de France", City: "Paris", PostalCode: "75013", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}},
		{Provider: schedule.ProviderUGC, ID: "ugc-25", ProviderID: "25", Slug: "ugc-25", Name: "UGC", Address: "1 rue", City: "Paris", PostalCode: "75001", AvailableDates: []string{}, AcceptedPasses: []string{"UGC_ILLIMITE"}},
	}, PublicMovies: []schedule.PublicMovieRecord{{ID: 1, IdentityAnchorProvider: schedule.ProviderMK2, IdentityAnchorSourceID: r.Movie.ProviderID, Title: r.Movie.Title, RuntimeMinutes: 123, TMDBID: 42, TMDBRuntimeMinutes: 123, PosterURL: r.Movie.PosterURL, UpdatedAt: start.UTC()}}, MovieSources: []schedule.PublicMovieSourceRecord{{Provider: schedule.ProviderMK2, SourceMovieID: r.Movie.ProviderID, SourceSlug: r.Movie.Slug, PublicMovieID: 1, Title: r.Movie.Title, PosterURL: r.Movie.PosterURL}}, MovieAliases: []schedule.MovieSlugAliasRecord{{Slug: r.Movie.Slug, PublicMovieID: 1, Kind: "source", Provider: schedule.ProviderMK2, SourceMovieID: r.Movie.ProviderID}}}
	if err := schedule.ValidateDataset(d, true); err != nil {
		t.Fatal(err)
	}
	service, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(d)}, schedule.ServiceOptions{Now: func() time.Time { return start.Add(-time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(service, "http://localhost:3000")
	response := performRequest(t, handler, "/api/v1/theaters?chain=mk2")
	var theaters []schedule.TheaterCatalogItem
	if json.Unmarshal(response.Body.Bytes(), &theaters) != nil || response.Code != http.StatusOK || len(theaters) != 1 || theaters[0].Provider != schedule.ProviderMK2 {
		t.Fatalf("chain=%s", response.Body.String())
	}
	combined := performRequest(t, handler, "/api/v1/theaters")
	if !strings.Contains(combined.Body.String(), `"provider":"ugc"`) || !strings.Contains(combined.Body.String(), `"provider":"mk2"`) {
		t.Fatal("combined providers")
	}
	for _, slug := range []string{"film-1", r.Movie.Slug} {
		response := performRequest(t, handler, "/api/v1/movies/"+slug+"/showtimes?date=2026-08-15")
		var detail schedule.MovieSchedule
		if json.Unmarshal(response.Body.Bytes(), &detail) != nil || response.Code != http.StatusOK || len(detail.Theaters) != 1 || len(detail.Theaters[0].Showtimes) != 1 {
			t.Fatalf("alias=%s", response.Body.String())
		}
		got := detail.Theaters[0].Showtimes[0]
		if got.Provider != schedule.ProviderMK2 || got.ID != r.ID || got.Language != "" || got.Room != "" || !got.EndTime.Equal(got.StartTime) || got.Movie.RuntimeMinutes != 123 || got.BookingURL == nil || *got.BookingURL != r.BookingURL || got.EstimatedEndTime == nil || !got.EstimatedEndTime.Equal(start.Add(138*time.Minute)) {
			t.Fatalf("DTO=%+v", got)
		}
		if !strings.Contains(response.Body.String(), `"language":""`) {
			t.Fatal("silent DTO omitted language")
		}
	}
	timeline := performRequest(t, handler, "/api/v1/timeline?date=2026-08-15&theaters=mk2-0004")
	if timeline.Code != http.StatusOK || !strings.Contains(timeline.Body.String(), `"duration_minutes":138`) || !strings.Contains(timeline.Body.String(), `"provider":"mk2"`) {
		t.Fatalf("timeline=%s", timeline.Body.String())
	}
	slots := performRequest(t, handler, "/api/v1/search/slot?theaters=mk2-0004&date=2026-08-15&start_after=18:00&finish_before=23:00&buffer_ads=0")
	var results []schedule.SlotResult
	if slots.Code != http.StatusOK || json.Unmarshal(slots.Body.Bytes(), &results) != nil || len(results) != 1 || !results[0].EffectiveEndTime.Equal(start.Add(123*time.Minute)) {
		t.Fatalf("slots=%s", slots.Body.String())
	}
	filtered := performRequest(t, handler, "/api/v1/search/slot?theaters=mk2-0004&date=2026-08-15&start_after=18:00&finish_before=23:00&language=VF")
	if filtered.Code != http.StatusOK || json.Unmarshal(filtered.Body.Bytes(), &results) != nil || len(results) != 0 {
		t.Fatalf("language filter=%s", filtered.Body.String())
	}
}
