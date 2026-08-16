package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"movieflow/api/internal/schedule"
)

type fixtureSource struct {
	data schedule.Dataset
}

func (s fixtureSource) Snapshot() schedule.Dataset { return s.data }

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	showtime := func(id, theaterID, movieID, title, poster, clock string) schedule.ShowtimeRecord {
		start, err := time.ParseInLocation("2006-01-02 15:04", "2026-08-15 "+clock, location)
		if err != nil {
			t.Fatal(err)
		}
		return schedule.ShowtimeRecord{
			ID:                "ugc-showing-" + id,
			ProviderShowingID: id,
			ServiceDate:       "2026-08-15",
			TheaterID:         theaterID,
			Movie: schedule.MovieRecord{
				ProviderID:     movieID,
				Slug:           "ugc-film-" + movieID,
				Title:          title,
				RuntimeMinutes: 100,
				PosterURL:      poster,
			},
			StartTime:       start,
			EndTime:         start.Add(100 * time.Minute),
			Language:        schedule.LanguageVOSTFR,
			ProviderVersion: schedule.LanguageVOSTFR,
			Format:          "2D",
			Room:            "Salle 1",
			BookingURL:      "https://www.ugc.fr/reservationSeances.html?id=" + id,
		}
	}
	data := schedule.Dataset{
		SchemaVersion: schedule.SchemaVersion,
		Provider:      schedule.ProviderUGC,
		Scope:         schedule.ScopeAll,
		GeneratedAt:   time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC),
		Timezone:      schedule.Timezone,
		Window:        schedule.Window{From: "2026-08-15", Through: "2026-08-15"},
		Theaters: []schedule.TheaterRecord{
			{ID: "ugc-25", ProviderID: "25", Slug: "ugc-lille", Name: "UGC Lille", Address: "1 rue de Lille", City: "Lille", PostalCode: "59000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}},
			{ID: "ugc-26", ProviderID: "26", Slug: "ugc-villeneuve", Name: "UGC Villeneuve", Address: "2 rue de Villeneuve", City: "Villeneuve d'Ascq", PostalCode: "59650", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}},
			{ID: "ugc-99", ProviderID: "99", Slug: "ugc-lyon", Name: "UGC Lyon", Address: "3 rue de Lyon", City: "Lyon", PostalCode: "69000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}},
		},
		Showtimes: []schedule.ShowtimeRecord{
			showtime("100", "ugc-25", "200", "Film A", "https://static.ugc.fr/posters/200.jpg", "12:00"),
			showtime("101", "ugc-26", "200", "Film A", "https://static.ugc.fr/posters/200.jpg", "18:00"),
			showtime("102", "ugc-99", "201", "Film B", "", "12:30"),
		},
	}
	service, err := schedule.NewService(fixtureSource{data: data}, schedule.ServiceOptions{
		DefaultCity: "Lille",
		CityAliases: map[string][]string{"Lille": {"Lille", "Villeneuve d'Ascq"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewHandler(service, "http://localhost:3000")
}

func performRequest(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type=%q", got)
	}
	return response
}

func TestTheatersTransport(t *testing.T) {
	handler := testHandler(t)
	response := performRequest(t, handler, "/api/v1/theaters?city=lille")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var theaters []schedule.Theater
	if err := json.Unmarshal(response.Body.Bytes(), &theaters); err != nil {
		t.Fatal(err)
	}
	if len(theaters) != 2 || theaters[0].ID != "ugc-25" || theaters[1].ID != "ugc-26" || theaters[0].PostalCode != "59000" {
		t.Fatalf("theaters=%+v", theaters)
	}

	empty := performRequest(t, handler, "/api/v1/theaters?chain=pathe")
	if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != "[]" {
		t.Fatalf("non-UGC status=%d body=%s", empty.Code, empty.Body.String())
	}
}

func TestMoviesTransport(t *testing.T) {
	handler := testHandler(t)
	response := performRequest(t, handler, "/api/v1/movies?search=film&page=1&page_size=1")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var catalog schedule.MovieCatalog
	if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.Page != 1 || catalog.PageSize != 1 || catalog.Total != 2 || len(catalog.Items) != 1 || catalog.Items[0].Slug != "ugc-film-200" || catalog.Items[0].PosterURL == nil {
		t.Fatalf("catalog=%+v", catalog)
	}

	empty := performRequest(t, handler, "/api/v1/movies?currently_screened=false")
	if empty.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", empty.Code, empty.Body.String())
	}
	if err := json.Unmarshal(empty.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.Total != 0 || catalog.Page != 1 || catalog.PageSize != 24 || len(catalog.Items) != 0 {
		t.Fatalf("empty catalog=%+v", catalog)
	}
}

func TestMovieShowtimesTransport(t *testing.T) {
	handler := testHandler(t)
	response := performRequest(t, handler, "/api/v1/movies/ugc-film-200/showtimes?date=2026-08-15&theaters=ugc-26%20,%20ugc-25")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result schedule.MovieSchedule
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Movie.Slug != "ugc-film-200" || result.Date != "2026-08-15" || len(result.Theaters) != 2 || result.Theaters[0].ID != "ugc-25" || result.Theaters[0].Showtimes[0].StartTime.Location() != time.UTC {
		t.Fatalf("schedule=%+v", result)
	}

	notFound := performRequest(t, handler, "/api/v1/movies/inconnu/showtimes?date=2026-08-15")
	assertAPIError(t, notFound, http.StatusNotFound, "not_found", "Film introuvable.")
}

func TestSearchSlotExactTheatersTransport(t *testing.T) {
	handler := testHandler(t)
	response := performRequest(t, handler, "/api/v1/search/slot?theaters=ugc-99&date=2026-08-15&start_after=12:00&finish_before=15:00")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var results []schedule.SlotResult
	if err := json.Unmarshal(response.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Theater.ID != "ugc-99" || results[0].BufferAdsMinutes != 20 {
		t.Fatalf("results=%+v", results)
	}
}

func TestInvalidQueriesTransport(t *testing.T) {
	handler := testHandler(t)
	tests := []struct {
		name    string
		target  string
		message string
	}{
		{"page zero", "/api/v1/movies?page=0", "Le paramètre page doit être un entier supérieur ou égal à 1."},
		{"page omitted value", "/api/v1/movies?page=", "Le paramètre page doit être un entier supérieur ou égal à 1."},
		{"page text", "/api/v1/movies?page=un", "Le paramètre page doit être un entier supérieur ou égal à 1."},
		{"page size zero", "/api/v1/movies?page_size=0", "Le paramètre page_size doit être un entier compris entre 1 et 100."},
		{"page size capped", "/api/v1/movies?page_size=101", "Le paramètre page_size doit être un entier compris entre 1 et 100."},
		{"screened boolean", "/api/v1/movies?currently_screened=non", "Le paramètre currently_screened doit être true ou false."},
		{"screened numeric boolean", "/api/v1/movies?currently_screened=1", "Le paramètre currently_screened doit être true ou false."},
		{"showtimes date required", "/api/v1/movies/ugc-film-200/showtimes", "Le paramètre date est requis."},
		{"showtimes scopes", "/api/v1/movies/ugc-film-200/showtimes?date=2026-08-15&city=Lille&theaters=ugc-25", "Les paramètres city et theaters sont mutuellement exclusifs."},
		{"showtimes unknown theater", "/api/v1/movies/ugc-film-200/showtimes?date=2026-08-15&theaters=inconnu", "Le paramètre theaters contient un identifiant de cinéma inconnu."},
		{"slot missing scope", "/api/v1/search/slot?date=2026-08-15&start_after=12:00&finish_before=15:00", "Le paramètre city ou theaters est requis."},
		{"slot duplicate scopes", "/api/v1/search/slot?city=Lille&theaters=ugc-25&date=2026-08-15&start_after=12:00&finish_before=15:00", "Les paramètres city et theaters sont mutuellement exclusifs."},
		{"slot empty theater", "/api/v1/search/slot?theaters=&date=2026-08-15&start_after=12:00&finish_before=15:00", "Le paramètre theaters contient un identifiant de cinéma inconnu."},
		{"slot unknown theater", "/api/v1/search/slot?theaters=inconnu&date=2026-08-15&start_after=12:00&finish_before=15:00", "Le paramètre theaters contient un identifiant de cinéma inconnu."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performRequest(t, handler, test.target)
			assertAPIError(t, response, http.StatusBadRequest, "invalid_query", test.message)
		})
	}
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d want=%d body=%s", response.Code, status, response.Body.String())
	}
	var result errorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Error.Code != code || result.Error.Message != message {
		t.Fatalf("error=%+v", result.Error)
	}
}
