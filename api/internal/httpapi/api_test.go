package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/observability"
	"messeances/api/internal/schedule"
)

type fixtureSource struct {
	view *schedule.SnapshotView
}

func (s fixtureSource) Snapshot() *schedule.SnapshotView { return s.view }

func testHandler(t *testing.T) http.Handler {
	return testHandlerWithAdmin(t, AdminOptions{})
}

func testHandlerWithAdmin(t *testing.T, options AdminOptions) http.Handler {
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
		record := schedule.ShowtimeRecord{
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
			ProviderVersion: string(schedule.LanguageVOSTFR),
			Format:          "2D",
			Room:            "Salle 1",
			BookingURL:      "https://www.ugc.fr/reservationSeances.html?id=" + id,
		}
		if movieID == "200" {
			record.Movie.Enrichment = &schedule.MovieEnrichment{TMDBID: 42, Overview: "Résumé", ReleaseDate: "2026-01-02", Genres: []string{"Drame"}, PosterURL: "https://image.tmdb.org/t/p/w500/a.jpg", BackdropURL: "https://image.tmdb.org/t/p/w780/a.jpg", TrailerYouTubeKey: "FRoff123456"}
		}
		switch id {
		case "100":
			record.Format = schedule.FormatScreenX
		case "101":
			record.Format = schedule.FormatLaserUltra
		case "102":
			record.Format = schedule.Format4DX
		}
		return record
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
	latitude, longitude := 50.6321, 3.0612
	data.Theaters[0].Latitude, data.Theaters[0].Longitude = &latitude, &longitude
	service, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(data)}, schedule.ServiceOptions{
		DefaultCity: "Lille",
		CityAliases: map[string][]string{"Lille": {"Lille", "Villeneuve d'Ascq"}},
		Now:         func() time.Time { return time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewHandlerWithAdmin(service, "http://localhost:3000", options)
}

func performRequest(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type=%q", got)
	}
	return response
}

func TestProbeContracts(t *testing.T) {
	handler := testHandler(t)
	for _, test := range []struct {
		name   string
		target string
		body   string
	}{
		{name: "liveness", target: "/healthz", body: "{\"status\":\"ok\"}\n"},
		{name: "readiness", target: "/readyz", body: "{\"status\":\"ready\"}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := performRequest(t, handler, test.target)
			if response.Code != http.StatusOK || response.Body.String() != test.body {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
		})
	}
}

func TestMetricsIsPublicAndGETOnly(t *testing.T) {
	handler := testHandler(t)
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	if get.Code != http.StatusOK || !strings.HasPrefix(get.Header().Get("Content-Type"), "text/plain") || !strings.Contains(get.Body.String(), "go_info") {
		t.Fatalf("GET status=%d content-type=%q body=%q", get.Code, get.Header().Get("Content-Type"), get.Body.String())
	}
	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodPut} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), method, "/metrics", nil))
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("method=%s status=%d body=%q", method, response.Code, response.Body.String())
		}
	}
}

func TestPanicRecoveryRedactsPanicRequestAndHeaders(t *testing.T) {
	secret := "synthetic-secret"
	var logs bytes.Buffer
	logger := observability.NewLogger(&logs)
	handler := recoverJSON(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(secret) }))
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/anything?token="+secret, nil)
	request.Header.Set("Authorization", "Bearer "+secret)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	combined := response.Body.String() + logs.String()
	if response.Code != http.StatusInternalServerError || strings.Contains(combined, secret) || !strings.Contains(logs.String(), `"error_code":"internal_failure"`) {
		t.Fatalf("status=%d body=%q logs=%q", response.Code, response.Body.String(), logs.String())
	}
}

func TestTheatersTransport(t *testing.T) {
	handler := testHandler(t)
	response := performRequest(t, handler, "/api/v1/theaters?city=lille")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var theaters []schedule.TheaterCatalogItem
	if err := json.Unmarshal(response.Body.Bytes(), &theaters); err != nil {
		t.Fatal(err)
	}
	if len(theaters) != 2 || theaters[0].ID != "ugc-25" || theaters[1].ID != "ugc-26" || theaters[0].PostalCode != "59000" || theaters[0].Provider != schedule.ProviderUGC || theaters[0].CitySlug != "lille" || theaters[1].CitySlug != "villeneuve-d-ascq" || theaters[0].Latitude == nil || *theaters[0].Latitude != 50.6321 || theaters[0].Longitude == nil || *theaters[0].Longitude != 3.0612 || theaters[1].Latitude != nil || theaters[1].Longitude != nil || !strings.Contains(response.Body.String(), `"latitude":null,"longitude":null`) {
		t.Fatalf("theaters=%+v", theaters)
	}

	empty := performRequest(t, handler, "/api/v1/theaters?chain=pathe")
	if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != "[]" {
		t.Fatalf("non-UGC status=%d body=%s", empty.Code, empty.Body.String())
	}
}

func TestCitiesTransportContracts(t *testing.T) {
	handler := testHandler(t)
	inventory := performRequest(t, handler, "/api/v1/cities")
	wantInventory := "{\"generated_at\":\"2026-08-14T12:00:00Z\",\"items\":[{\"name\":\"Lille\",\"slug\":\"lille\",\"theaters\":[{\"provider\":\"ugc\",\"id\":\"ugc-25\",\"slug\":\"ugc-lille\",\"name\":\"UGC Lille\"}]},{\"name\":\"Lyon\",\"slug\":\"lyon\",\"theaters\":[{\"provider\":\"ugc\",\"id\":\"ugc-99\",\"slug\":\"ugc-lyon\",\"name\":\"UGC Lyon\"}]},{\"name\":\"Villeneuve d'Ascq\",\"slug\":\"villeneuve-d-ascq\",\"theaters\":[{\"provider\":\"ugc\",\"id\":\"ugc-26\",\"slug\":\"ugc-villeneuve\",\"name\":\"UGC Villeneuve\"}]}]}\n"
	if inventory.Code != http.StatusOK || inventory.Body.String() != wantInventory {
		t.Fatalf("inventory status=%d body=%s", inventory.Code, inventory.Body.String())
	}

	detail := performRequest(t, handler, "/api/v1/cities/lille")
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(detail.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sortedKeys(payload), []string{"city", "generated_at", "movies", "theaters"}) || payload["generated_at"] != "2026-08-14T12:00:00Z" {
		t.Fatalf("detail root=%+v", payload)
	}
	city := payload["city"].(map[string]any)
	theaters := payload["theaters"].([]any)
	movies := payload["movies"].([]any)
	if !reflect.DeepEqual(city, map[string]any{"name": "Lille", "slug": "lille"}) || len(theaters) != 1 || theaters[0].(map[string]any)["city_slug"] != "lille" || len(movies) != 1 || movies[0].(map[string]any)["slug"] != "tmdb-film-42" {
		t.Fatalf("detail=%+v", payload)
	}
	if _, exists := theaters[0].(map[string]any)["latitude"]; exists {
		t.Fatal("city detail unexpectedly exposes theater coordinates")
	}
	assertAPIError(t, performRequest(t, handler, "/api/v1/cities/Lille"), http.StatusNotFound, "not_found", "Ville introuvable.")
	assertAPIError(t, performRequest(t, handler, "/api/v1/cities/inconnue"), http.StatusNotFound, "not_found", "Ville introuvable.")
}

func TestTheaterShowtimesTransportContracts(t *testing.T) {
	handler := testHandler(t)
	response := performRequest(t, handler, "/api/v1/theaters/ugc-lille/showtimes")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sortedKeys(payload), []string{"date", "generated_at", "showtimes", "theater", "timezone"}) || payload["generated_at"] != "2026-08-14T12:00:00Z" || payload["timezone"] != schedule.Timezone || payload["date"] != "2026-08-15" {
		t.Fatalf("payload=%+v", payload)
	}
	theater := payload["theater"].(map[string]any)
	showtimes := payload["showtimes"].([]any)
	if theater["slug"] != "ugc-lille" || theater["city_slug"] != "lille" || len(showtimes) != 1 || showtimes[0].(map[string]any)["id"] != "ugc-showing-100" {
		t.Fatalf("payload=%+v", payload)
	}
	if _, exists := theater["latitude"]; exists {
		t.Fatal("theater showtimes unexpectedly exposes coordinates")
	}
	empty := performRequest(t, handler, "/api/v1/theaters/ugc-lille/showtimes?date=2027-01-01")
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), `"date":"2027-01-01","showtimes":[]`) {
		t.Fatalf("empty status=%d body=%s", empty.Code, empty.Body.String())
	}
	assertAPIError(t, performRequest(t, handler, "/api/v1/theaters/ugc-lille/showtimes?date=15-08-2026"), http.StatusBadRequest, "invalid_query", "Le paramètre date doit respecter le format YYYY-MM-DD.")
	assertAPIError(t, performRequest(t, handler, "/api/v1/theaters/ugc-lille/showtimes?date="), http.StatusBadRequest, "invalid_query", "Le paramètre date est requis.")
	assertAPIError(t, performRequest(t, handler, "/api/v1/theaters/UGC-lille/showtimes"), http.StatusNotFound, "not_found", "Cinéma introuvable.")
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func TestTheatersKinepolisChainAndCombinedProviderDTOs(t *testing.T) {
	location, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 8, 15, 20, 0, 0, 0, location)
	ugc := schedule.TheaterRecord{Provider: schedule.ProviderUGC, ID: "ugc-25", ProviderID: "25", Slug: "ugc-25", Name: "UGC Lille", Address: "Lille", City: "Lille", PostalCode: "59000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}}
	kine := schedule.TheaterRecord{Provider: schedule.ProviderKinepolis, ID: "kinepolis-LOM", ProviderID: "LOM", Slug: "kinepolis-LOM", Name: "Kinepolis Lomme", City: "Lomme", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{}}
	data := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderCombined, Scope: schedule.ScopeAll, GeneratedAt: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC), Timezone: schedule.Timezone, Window: schedule.Window{From: "2026-08-15", Through: "2026-08-15"}, Theaters: []schedule.TheaterRecord{ugc, kine}, Showtimes: []schedule.ShowtimeRecord{{Provider: schedule.ProviderUGC, ID: "ugc-showing-1", ProviderShowingID: "1", ServiceDate: "2026-08-15", TheaterID: "ugc-25", Movie: schedule.MovieRecord{Provider: schedule.ProviderUGC, ProviderID: "1", Slug: "ugc-film-1", Title: "Film partagé", RuntimeMinutes: 90, Enrichment: &schedule.MovieEnrichment{TMDBID: 42}}, StartTime: start, EndTime: start.Add(90 * time.Minute), Language: schedule.LanguageVF, ProviderVersion: "VF", Format: "2D", BookingURL: "https://www.ugc.fr/reservationSeances.html?id=1"}, {Provider: schedule.ProviderKinepolis, ID: "kinepolis-showing-VS1", ProviderShowingID: "VS1", ServiceDate: "2026-08-15", TheaterID: "kinepolis-LOM", Movie: schedule.MovieRecord{Provider: schedule.ProviderKinepolis, ProviderID: "HO1", Slug: "kinepolis-film-HO1", Title: "Film partagé", RuntimeMinutes: 100, Enrichment: &schedule.MovieEnrichment{TMDBID: 42}}, StartTime: start, EndTime: start.Add(100 * time.Minute), Language: schedule.LanguageVF, ProviderVersion: "VF", Format: "IMAX", BookingURL: "https://kinepolis.fr/direct-vista-redirect/VS1/0/LOM/0"}}}
	service, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(data)}, schedule.ServiceOptions{Now: func() time.Time { return time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(service, "http://localhost:3000")
	response := performRequest(t, handler, "/api/v1/theaters?chain=kinepolis")
	var theaters []schedule.TheaterCatalogItem
	if err := json.Unmarshal(response.Body.Bytes(), &theaters); err != nil {
		t.Fatal(err)
	}
	if len(theaters) != 1 || theaters[0].Provider != schedule.ProviderKinepolis {
		t.Fatalf("theaters=%+v", theaters)
	}
	combined := performRequest(t, handler, "/api/v1/theaters")
	if !strings.Contains(combined.Body.String(), `"provider":"ugc"`) || !strings.Contains(combined.Body.String(), `"provider":"kinepolis"`) {
		t.Fatalf("combined=%s", combined.Body.String())
	}
	timeline := performRequest(t, handler, "/api/v1/timeline?date=2026-08-15&theaters=kinepolis-LOM")
	if !strings.Contains(timeline.Body.String(), `"provider":"kinepolis"`) || !strings.Contains(timeline.Body.String(), `"slug":"tmdb-film-42"`) {
		t.Fatalf("timeline=%s", timeline.Body.String())
	}
	catalog := performRequest(t, handler, "/api/v1/movies?page_size=1")
	var movies schedule.MovieCatalog
	if err := json.Unmarshal(catalog.Body.Bytes(), &movies); err != nil {
		t.Fatal(err)
	}
	if catalog.Code != http.StatusOK || movies.Total != 1 || len(movies.Items) != 1 || movies.Items[0].Slug != "tmdb-film-42" {
		t.Fatalf("catalog status=%d payload=%+v", catalog.Code, movies)
	}
	detail := performRequest(t, handler, "/api/v1/movies/tmdb-film-42/showtimes?date=2026-08-15")
	var movieSchedule schedule.MovieSchedule
	if err := json.Unmarshal(detail.Body.Bytes(), &movieSchedule); err != nil {
		t.Fatal(err)
	}
	if detail.Code != http.StatusOK || len(movieSchedule.Theaters) != 2 {
		t.Fatalf("detail status=%d payload=%+v", detail.Code, movieSchedule)
	}
	want := map[string]struct {
		provider schedule.Provider
		booking  string
	}{"ugc-showing-1": {schedule.ProviderUGC, "https://www.ugc.fr/reservationSeances.html?id=1"}, "kinepolis-showing-VS1": {schedule.ProviderKinepolis, "https://kinepolis.fr/direct-vista-redirect/VS1/0/LOM/0"}}
	for _, theater := range movieSchedule.Theaters {
		for _, showtime := range theater.Showtimes {
			expected, exists := want[showtime.ID]
			if !exists || showtime.Provider != expected.provider || showtime.Movie.Slug != "tmdb-film-42" || showtime.BookingURL == nil || *showtime.BookingURL != expected.booking {
				t.Fatalf("showtime=%+v", showtime)
			}
			delete(want, showtime.ID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing showtimes=%+v", want)
	}
	for _, slug := range []string{"ugc-film-1", "kinepolis-film-HO1"} {
		obsolete := performRequest(t, handler, "/api/v1/movies/"+slug+"/showtimes?date=2026-08-15")
		assertAPIError(t, obsolete, http.StatusNotFound, "not_found", "Film introuvable.")
	}
}

func TestTimelineMediaTransportAndContractIsolation(t *testing.T) {
	handler := testHandler(t)
	response := performRequest(t, handler, "/api/v1/timeline?date=2026-08-15&theaters=ugc-25,ugc-26,ugc-99")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	theaters := payload["theaters"].([]any)
	matched := theaters[0].(map[string]any)["showtimes"].([]any)[0].(map[string]any)
	repeated := theaters[1].(map[string]any)["showtimes"].([]any)[0].(map[string]any)
	unmatched := theaters[2].(map[string]any)["showtimes"].([]any)[0].(map[string]any)
	if matched["backdrop_url"] != "https://image.tmdb.org/t/p/w780/a.jpg" || unmatched["backdrop_url"] != nil {
		t.Fatalf("matched=%+v unmatched=%+v", matched, unmatched)
	}
	if matched["poster_url"] != "https://image.tmdb.org/t/p/w500/a.jpg" || repeated["poster_url"] != "https://image.tmdb.org/t/p/w500/a.jpg" || unmatched["poster_url"] != nil {
		t.Fatalf("matched=%+v repeated=%+v unmatched=%+v", matched, repeated, unmatched)
	}
	if _, exists := unmatched["poster_url"]; !exists {
		t.Fatalf("nullable poster field omitted: %+v", unmatched)
	}
	if movie := matched["movie"].(map[string]any); hasAnyKey(movie, "backdrop_url", "poster_url") {
		t.Fatalf("nested movie gained timeline media: %+v", movie)
	}
	if matched["start_offset_minutes"] != float64(240) || matched["duration_minutes"] != float64(100) {
		t.Fatalf("timeline timing changed: %+v", matched)
	}
	slotResponse := performRequest(t, handler, "/api/v1/search/slot?theaters=ugc-25,ugc-99&date=2026-08-15&start_after=12:00&finish_before=15:00&buffer_ads=0")
	if slotResponse.Code != http.StatusOK {
		t.Fatalf("slot status=%d body=%s", slotResponse.Code, slotResponse.Body.String())
	}
	var slots []map[string]any
	if err := json.Unmarshal(slotResponse.Body.Bytes(), &slots); err != nil {
		t.Fatal(err)
	}
	if len(slots) != 2 || slots[0]["poster_url"] != "https://image.tmdb.org/t/p/w500/a.jpg" || slots[0]["backdrop_url"] != "https://image.tmdb.org/t/p/w780/a.jpg" {
		t.Fatalf("matched slot media=%+v", slots)
	}
	if poster, exists := slots[1]["poster_url"]; !exists || poster != nil {
		t.Fatalf("missing slot poster must serialize as explicit null: %+v", slots[1])
	}
	if backdrop, exists := slots[1]["backdrop_url"]; !exists || backdrop != nil {
		t.Fatalf("missing slot backdrop must serialize as explicit null: %+v", slots[1])
	}
	if showtime := slots[0]["showtime"].(map[string]any); hasAnyKey(showtime, "poster_url", "backdrop_url") || hasAnyKey(showtime["movie"].(map[string]any), "poster_url", "backdrop_url") {
		t.Fatalf("slot media leaked into nested showtime: %+v", showtime)
	}
	movieResponse := performRequest(t, handler, "/api/v1/movies/tmdb-film-42/showtimes?date=2026-08-15")
	if movieResponse.Code != http.StatusOK {
		t.Fatalf("movie showtimes status=%d body=%s", movieResponse.Code, movieResponse.Body.String())
	}
	var moviePayload map[string]any
	if err := json.Unmarshal(movieResponse.Body.Bytes(), &moviePayload); err != nil {
		t.Fatal(err)
	}
	movieShowtime := moviePayload["theaters"].([]any)[0].(map[string]any)["showtimes"].([]any)[0].(map[string]any)
	if moviePayload["backdrop_url"] != "https://image.tmdb.org/t/p/w780/a.jpg" {
		t.Fatalf("movie showtimes root backdrop=%+v", moviePayload)
	}
	if hasAnyKey(movieShowtime, "poster_url", "backdrop_url", "start_offset_minutes", "duration_minutes") {
		t.Fatalf("timeline fields leaked to movie showtime: %+v", movieShowtime)
	}
	if hasAnyKey(moviePayload["movie"].(map[string]any), "backdrop_url") {
		t.Fatalf("backdrop leaked to movie catalog item: %+v", moviePayload["movie"])
	}
	catalogResponse := performRequest(t, handler, "/api/v1/movies?page_size=2")
	if catalogResponse.Code != http.StatusOK || strings.Contains(catalogResponse.Body.String(), "backdrop_url") {
		t.Fatalf("backdrop leaked to catalog: status=%d body=%s", catalogResponse.Code, catalogResponse.Body.String())
	}
}

func hasAnyKey(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, exists := values[key]; exists {
			return true
		}
	}
	return false
}

func TestMoviesTransport(t *testing.T) {
	handler := testHandler(t)
	wantGeneratedAt := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	response := performRequest(t, handler, "/api/v1/movies?search=film&page=1&page_size=1")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var catalog schedule.MovieCatalog
	if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if !catalog.GeneratedAt.Equal(wantGeneratedAt) || catalog.Page != 1 || catalog.PageSize != 1 || catalog.Total != 2 || len(catalog.Items) != 1 || catalog.Items[0].Slug != "tmdb-film-42" || catalog.Items[0].ShowtimeCount != 2 || catalog.Items[0].PosterURL == nil || catalog.Items[0].TMDBID == nil || *catalog.Items[0].TMDBID != 42 || catalog.Items[0].TrailerYouTubeKey == nil || *catalog.Items[0].TrailerYouTubeKey != "FRoff123456" || len(catalog.Items[0].Genres) != 1 {
		t.Fatalf("catalog=%+v", catalog)
	}
	if !strings.Contains(response.Body.String(), `"generated_at":"2026-08-14T12:00:00Z"`) {
		t.Fatalf("snapshot timestamp missing from catalog transport: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), `"movie":{"slug":"tmdb-film-42","tmdb_id"`) {
		t.Fatal("nested movie contract unexpectedly enriched")
	}
	all := performRequest(t, handler, "/api/v1/movies?page_size=2")
	if !strings.Contains(all.Body.String(), `"tmdb_id":null,"trailer_youtube_key":null,"overview":null,"release_date":null,"genres":[]`) {
		t.Fatalf("unmatched null/empty contract missing: %s", all.Body.String())
	}
	if !strings.Contains(all.Body.String(), `"available_genres":["Drame"]`) {
		t.Fatalf("available genres missing from catalog: %s", all.Body.String())
	}
	filtered := performRequest(t, handler, "/api/v1/movies?genres=drame&duration=medium&date=today&page_size=1")
	if err := json.Unmarshal(filtered.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if filtered.Code != http.StatusOK || catalog.Total != 1 || len(catalog.Items) != 1 || catalog.Items[0].Slug != "tmdb-film-42" || catalog.Items[0].ShowtimeCount != 2 || !reflect.DeepEqual(catalog.AvailableGenres, []string{"Drame"}) {
		t.Fatalf("advanced filtered status=%d payload=%+v", filtered.Code, catalog)
	}
	selected := performRequest(t, handler, "/api/v1/movies?theaters=ugc-26&page_size=1")
	if err := json.Unmarshal(selected.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if selected.Code != http.StatusOK || catalog.Total != 1 || len(catalog.Items) != 1 || catalog.Items[0].Slug != "tmdb-film-42" || catalog.Items[0].ShowtimeCount != 1 {
		t.Fatalf("selected catalog status=%d payload=%+v", selected.Code, catalog)
	}
	secondSelectedPage := performRequest(t, handler, "/api/v1/movies?theaters=ugc-25,ugc-99&page=2&page_size=1")
	if err := json.Unmarshal(secondSelectedPage.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if secondSelectedPage.Code != http.StatusOK || catalog.Total != 2 || len(catalog.Items) != 1 || catalog.Items[0].Slug != "ugc-film-201" || catalog.Items[0].ShowtimeCount != 1 {
		t.Fatalf("selected page status=%d payload=%+v", secondSelectedPage.Code, catalog)
	}

	empty := performRequest(t, handler, "/api/v1/movies?currently_screened=false")
	if empty.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", empty.Code, empty.Body.String())
	}
	if err := json.Unmarshal(empty.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if !catalog.GeneratedAt.Equal(wantGeneratedAt) || catalog.Total != 0 || catalog.Page != 1 || catalog.PageSize != 24 || len(catalog.Items) != 0 {
		t.Fatalf("empty catalog=%+v", catalog)
	}
	if !strings.Contains(empty.Body.String(), `"generated_at":"2026-08-14T12:00:00Z"`) {
		t.Fatalf("snapshot timestamp missing from empty catalog transport: %s", empty.Body.String())
	}
	if !strings.Contains(empty.Body.String(), `"items":[],"available_genres":[]`) {
		t.Fatalf("empty arrays missing from catalog transport: %s", empty.Body.String())
	}

	for _, test := range []struct {
		name      string
		target    string
		want      string
		wantCount int
	}{
		{name: "explicit sort", target: "/api/v1/movies?sort=title_desc&page_size=1", want: "ugc-film-201", wantCount: 1},
		{name: "missing sort defaults", target: "/api/v1/movies?page_size=1", want: "tmdb-film-42", wantCount: 2},
		{name: "invalid sort defaults", target: "/api/v1/movies?sort=invalid&page_size=1", want: "tmdb-film-42", wantCount: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := performRequest(t, handler, test.target)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var sorted schedule.MovieCatalog
			if err := json.Unmarshal(response.Body.Bytes(), &sorted); err != nil {
				t.Fatal(err)
			}
			if len(sorted.Items) != 1 || sorted.Items[0].Slug != test.want || sorted.Items[0].ShowtimeCount != test.wantCount {
				t.Fatalf("catalog=%+v", sorted)
			}
			if strings.Contains(response.Body.String(), "showtimes_count") {
				t.Fatalf("incorrect count field leaked: %s", response.Body.String())
			}
		})
	}
}

func TestCanonicalMoviesTransportEndedAliasesAndValidation(t *testing.T) {
	location, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 8, 15, 12, 0, 0, 0, location)
	updated := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	data := schedule.Dataset{
		SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderUGC, Scope: schedule.ScopeAll,
		GeneratedAt: updated, Timezone: schedule.Timezone, Window: schedule.Window{From: "2026-08-15", Through: "2026-08-15"},
		Theaters: []schedule.TheaterRecord{
			{ID: "ugc-25", ProviderID: "25", Slug: "ugc-25", Name: "UGC Lille", Address: "Lille", City: "Lille", PostalCode: "59000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}},
			{ID: "ugc-99", ProviderID: "99", Slug: "ugc-99", Name: "UGC Lyon", Address: "Lyon", City: "Lyon", PostalCode: "69000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}},
		},
		Showtimes:    []schedule.ShowtimeRecord{{ID: "ugc-showing-1", ProviderShowingID: "1", ServiceDate: "2026-08-15", TheaterID: "ugc-25", Movie: schedule.MovieRecord{ProviderID: "10", Slug: "ugc-film-10", Title: "Source", RuntimeMinutes: 90, PublicMovieID: 1}, StartTime: start, EndTime: start.Add(90 * time.Minute), Language: schedule.LanguageVF, ProviderVersion: "VF", Format: schedule.Format2D, BookingURL: "https://www.ugc.fr/reservationSeances.html?id=1"}},
		PublicMovies: []schedule.PublicMovieRecord{{ID: 1, IdentityAnchorProvider: schedule.ProviderUGC, IdentityAnchorSourceID: "10", Title: "Canonique", RuntimeMinutes: 100, UpdatedAt: updated}, {ID: 2, RedirectToID: 1, IdentityAnchorProvider: schedule.ProviderUGC, IdentityAnchorSourceID: "11", Title: "Tombstone", RuntimeMinutes: 100, UpdatedAt: updated}, {ID: 3, IdentityAnchorProvider: schedule.ProviderUGC, IdentityAnchorSourceID: "30", Title: "Terminé", RuntimeMinutes: 80, UpdatedAt: updated.Add(time.Hour)}},
		MovieSources: []schedule.PublicMovieSourceRecord{{Provider: schedule.ProviderUGC, SourceMovieID: "10", PublicMovieID: 1, SourceSlug: "ugc-film-10", Title: "Source", RuntimeMinutes: 90}, {Provider: schedule.ProviderUGC, SourceMovieID: "30", PublicMovieID: 3, SourceSlug: "ugc-film-30", Title: "Terminé", RuntimeMinutes: 80}},
		MovieAliases: []schedule.MovieSlugAliasRecord{{Slug: "ugc-film-10", PublicMovieID: 1, Kind: "source", Provider: schedule.ProviderUGC, SourceMovieID: "10"}},
	}
	service, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(data, schedule.SnapshotRevision{ScheduleVersion: 4, EnrichmentVersion: 2})}, schedule.ServiceOptions{Now: func() time.Time { return start.Add(-time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(service, "http://localhost:3000")
	response := performRequest(t, handler, "/api/v1/movies?include_ended=true&page_size=10")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"catalog_revision":"schedule:4;enrichment:2"`) || strings.Contains(response.Body.String(), `"provider"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var catalog schedule.MovieCatalog
	if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil || catalog.Total != 2 || catalog.Items[1].Slug != "film-3" {
		t.Fatalf("catalog=%+v err=%v", catalog, err)
	}
	for _, slug := range []string{"ugc-film-10", "film-2"} {
		detail := performRequest(t, handler, "/api/v1/movies/"+slug+"/showtimes?date=2026-08-15&theaters=ugc-99")
		if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"slug":"film-1"`) || !strings.Contains(detail.Body.String(), `"currently_screened":true`) || !strings.Contains(detail.Body.String(), `"available_dates":[],"theaters":[]`) {
			t.Fatalf("slug=%s status=%d body=%s", slug, detail.Code, detail.Body.String())
		}
	}
	ended := performRequest(t, handler, "/api/v1/movies/film-3/showtimes?date=2026-08-15")
	if ended.Code != http.StatusOK || !strings.Contains(ended.Body.String(), `"currently_screened":false`) || !strings.Contains(ended.Body.String(), `"available_dates":[],"theaters":[]`) {
		t.Fatalf("ended status=%d body=%s", ended.Code, ended.Body.String())
	}
	pastService, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(data)}, schedule.ServiceOptions{Now: func() time.Time { return start.Add(24 * time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}
	past := performRequest(t, NewHandler(pastService, "http://localhost:3000"), "/api/v1/movies/film-1/showtimes?date=2026-08-15")
	if past.Code != http.StatusOK || !strings.Contains(past.Body.String(), `"currently_screened":false`) {
		t.Fatalf("past status=%d body=%s", past.Code, past.Body.String())
	}
	assertAPIError(t, performRequest(t, handler, "/api/v1/movies?include_ended=1"), http.StatusBadRequest, "invalid_query", "Le paramètre include_ended doit être true ou false.")
	assertAPIError(t, performRequest(t, handler, "/api/v1/movies?include_ended=true&currently_screened=true"), http.StatusBadRequest, "invalid_query", "Le paramètre include_ended est incompatible avec currently_screened=true ou theaters.")
	assertAPIError(t, performRequest(t, handler, "/api/v1/movies?include_ended=true&theaters=ugc-25"), http.StatusBadRequest, "invalid_query", "Le paramètre include_ended est incompatible avec currently_screened=true ou theaters.")
	allWithFalse := performRequest(t, handler, "/api/v1/movies?include_ended=true&currently_screened=false&page_size=10")
	if allWithFalse.Code != http.StatusOK || !strings.Contains(allWithFalse.Body.String(), `"total":2`) {
		t.Fatalf("all with false status=%d body=%s", allWithFalse.Code, allWithFalse.Body.String())
	}
}

func TestMovieShowtimesTransport(t *testing.T) {
	handler := testHandler(t)
	response := performRequest(t, handler, "/api/v1/movies/tmdb-film-42/showtimes?date=2026-08-15&theaters=ugc-26%20,%20ugc-25")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result schedule.MovieSchedule
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Movie.Slug != "tmdb-film-42" || result.Date != "2026-08-15" || len(result.Theaters) != 2 || result.Theaters[0].ID != "ugc-25" || result.Theaters[0].CitySlug != "lille" || result.Theaters[1].CitySlug != "villeneuve-d-ascq" || result.Theaters[0].Showtimes[0].Movie.Slug != "tmdb-film-42" || result.Theaters[0].Showtimes[0].StartTime.Location() != time.UTC {
		t.Fatalf("schedule=%+v", result)
	}
	if !reflect.DeepEqual(result.AvailableDates, []string{"2026-08-15"}) {
		t.Fatalf("available dates=%v", result.AvailableDates)
	}
	if result.BackdropURL == nil || *result.BackdropURL != "https://image.tmdb.org/t/p/w780/a.jpg" {
		t.Fatalf("backdrop=%v", result.BackdropURL)
	}
	if result.Movie.TrailerYouTubeKey == nil || *result.Movie.TrailerYouTubeKey != "FRoff123456" {
		t.Fatalf("trailer YouTube key=%v", result.Movie.TrailerYouTubeKey)
	}
	missing := performRequest(t, handler, "/api/v1/movies/ugc-film-201/showtimes?date=2026-08-15")
	if missing.Code != http.StatusOK {
		t.Fatalf("missing backdrop status=%d body=%s", missing.Code, missing.Body.String())
	}
	var missingPayload map[string]any
	if err := json.Unmarshal(missing.Body.Bytes(), &missingPayload); err != nil {
		t.Fatal(err)
	}
	if backdrop, exists := missingPayload["backdrop_url"]; !exists || backdrop != nil {
		t.Fatalf("missing backdrop must serialize as explicit null: %+v", missingPayload)
	}
	emptyAvailability := performRequest(t, handler, "/api/v1/movies/tmdb-film-42/showtimes?date=2026-08-15&theaters=ugc-99")
	if emptyAvailability.Code != http.StatusOK {
		t.Fatalf("empty availability status=%d body=%s", emptyAvailability.Code, emptyAvailability.Body.String())
	}
	var emptyPayload map[string]any
	if err := json.Unmarshal(emptyAvailability.Body.Bytes(), &emptyPayload); err != nil {
		t.Fatal(err)
	}
	availableDates, exists := emptyPayload["available_dates"]
	dates, isArray := availableDates.([]any)
	if !exists || !isArray || len(dates) != 0 {
		t.Fatalf("available_dates must serialize as []: %+v", emptyPayload)
	}
	if emptyPayload["currently_screened"] != true {
		t.Fatalf("global current-screening signal lost under theater filter: %+v", emptyPayload)
	}
	obsolete := performRequest(t, handler, "/api/v1/movies/ugc-film-200/showtimes?date=2026-08-15")
	assertAPIError(t, obsolete, http.StatusNotFound, "not_found", "Film introuvable.")

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
	if len(results) != 1 || results[0].Theater.ID != "ugc-99" || results[0].BufferAdsMinutes != 15 || !results[0].EffectiveStartTime.Equal(results[0].Showtime.StartTime) || !results[0].EffectiveEndTime.Equal(results[0].Showtime.EndTime) {
		t.Fatalf("results=%+v", results)
	}
}

func TestSearchSlotIncludeAdsTransport(t *testing.T) {
	handler := testHandler(t)
	includedResponse := performRequest(t, handler, "/api/v1/search/slot?theaters=ugc-99&date=2026-08-15&start_after=12:30&finish_before=14:30")
	excludedResponse := performRequest(t, handler, "/api/v1/search/slot?theaters=ugc-99&date=2026-08-15&start_after=12:45&finish_before=14:30&include_ads=false")
	if includedResponse.Code != http.StatusOK || excludedResponse.Code != http.StatusOK {
		t.Fatalf("included status=%d body=%s excluded status=%d body=%s", includedResponse.Code, includedResponse.Body.String(), excludedResponse.Code, excludedResponse.Body.String())
	}
	var included, excluded []schedule.SlotResult
	if err := json.Unmarshal(includedResponse.Body.Bytes(), &included); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(excludedResponse.Body.Bytes(), &excluded); err != nil {
		t.Fatal(err)
	}
	if len(included) != 1 || len(excluded) != 1 {
		t.Fatalf("included=%+v excluded=%+v", included, excluded)
	}
	if !included[0].EffectiveStartTime.Equal(included[0].Showtime.StartTime) || !excluded[0].EffectiveStartTime.Equal(excluded[0].Showtime.StartTime.Add(15*time.Minute)) || !included[0].EffectiveEndTime.Equal(included[0].Showtime.EndTime) || !excluded[0].EffectiveEndTime.Equal(excluded[0].Showtime.EndTime) {
		t.Fatalf("included=%+v excluded=%+v", included[0], excluded[0])
	}
	if included[0].BufferAdsMinutes != 15 || excluded[0].BufferAdsMinutes != 15 || included[0].SlackBeforeMinutes != 0 || excluded[0].SlackBeforeMinutes != 0 || included[0].SlackAfterMinutes != 20 || excluded[0].SlackAfterMinutes != 20 {
		t.Fatalf("included=%+v excluded=%+v", included[0], excluded[0])
	}

	tooLateIncluded := performRequest(t, handler, "/api/v1/search/slot?theaters=ugc-99&date=2026-08-15&start_after=12:50&finish_before=14:30&include_ads=true")
	if tooLateIncluded.Code != http.StatusOK || tooLateIncluded.Body.String() != "[]\n" {
		t.Fatalf("too-late included status=%d body=%s", tooLateIncluded.Code, tooLateIncluded.Body.String())
	}
}

func TestSearchSlotExplicitAdsBufferTransport(t *testing.T) {
	handler := testHandler(t)
	tests := []struct {
		name       string
		target     string
		wantBuffer int
		wantShift  time.Duration
	}{
		{"omitted defaults to 15", "/api/v1/search/slot?theaters=ugc-99&date=2026-08-15&start_after=12:45&finish_before=15:00&include_ads=false", 15, 15 * time.Minute},
		{"explicit zero", "/api/v1/search/slot?theaters=ugc-99&date=2026-08-15&start_after=12:30&finish_before=15:00&include_ads=false&buffer_ads=0", 0, 0},
		{"explicit 20", "/api/v1/search/slot?theaters=ugc-99&date=2026-08-15&start_after=12:50&finish_before=15:00&include_ads=false&buffer_ads=20", 20, 20 * time.Minute},
		{"explicit 120", "/api/v1/search/slot?theaters=ugc-99&date=2026-08-15&start_after=14:30&finish_before=15:00&include_ads=false&buffer_ads=120", 120, 120 * time.Minute},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performRequest(t, handler, test.target)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var results []schedule.SlotResult
			if err := json.Unmarshal(response.Body.Bytes(), &results); err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].BufferAdsMinutes != test.wantBuffer || !results[0].EffectiveStartTime.Equal(results[0].Showtime.StartTime.Add(test.wantShift)) || !results[0].EffectiveEndTime.Equal(results[0].Showtime.EndTime) {
				t.Fatalf("results=%+v", results)
			}
		})
	}
}

func TestSearchSlotFormatTransport(t *testing.T) {
	handler := testHandler(t)
	filtered := performRequest(t, handler, "/api/v1/search/slot?city=Lille&date=2026-08-15&start_after=11:00&finish_before=15:00&buffer_ads=0&format=SCREENX")
	if filtered.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", filtered.Code, filtered.Body.String())
	}
	var results []schedule.SlotResult
	if err := json.Unmarshal(filtered.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Showtime.ID != "ugc-showing-100" || results[0].Showtime.Format != schedule.FormatScreenX {
		t.Fatalf("results=%+v", results)
	}
	omitted := performRequest(t, handler, "/api/v1/search/slot?city=Lille&date=2026-08-15&start_after=11:00&finish_before=21:00&buffer_ads=0")
	if omitted.Code != http.StatusOK || !strings.Contains(omitted.Body.String(), `"format":"SCREENX"`) || !strings.Contains(omitted.Body.String(), `"format":"LASER_ULTRA"`) {
		t.Fatalf("omitted format status=%d body=%s", omitted.Code, omitted.Body.String())
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
		{"movies empty genre", "/api/v1/movies?genres=Drame,", "Le paramètre genres contient une valeur vide."},
		{"movies empty duration", "/api/v1/movies?duration=", "Le paramètre duration doit être short, medium ou long."},
		{"movies invalid duration", "/api/v1/movies?duration=tiny", "Le paramètre duration doit être short, medium ou long."},
		{"movies empty date", "/api/v1/movies?date=", "Le paramètre date doit être today, tomorrow, weekend ou respecter le format YYYY-MM-DD."},
		{"movies invalid date", "/api/v1/movies?date=15-08-2026", "Le paramètre date doit être today, tomorrow, weekend ou respecter le format YYYY-MM-DD."},
		{"movies invalid date to", "/api/v1/movies?date=2026-08-16&date_to=17-08-2026", "Le paramètre date_to doit respecter le format YYYY-MM-DD."},
		{"movies past date", "/api/v1/movies?date=2026-08-14", "Les dates de séance ne peuvent pas être antérieures à aujourd’hui."},
		{"movies inverted dates", "/api/v1/movies?date=2026-08-17&date_to=2026-08-16", "Le paramètre date_to doit être supérieur ou égal au paramètre date."},
		{"movies date to without date", "/api/v1/movies?date_to=2026-08-16", "Le paramètre date_to nécessite une date personnalisée au format YYYY-MM-DD."},
		{"movies date to with preset", "/api/v1/movies?date=today&date_to=2026-08-16", "Le paramètre date_to nécessite une date personnalisée au format YYYY-MM-DD."},
		{"movies date to with malformed start", "/api/v1/movies?date=15-08-2026&date_to=2026-08-16", "Le paramètre date_to nécessite une date personnalisée au format YYYY-MM-DD."},
		{"movies ended date", "/api/v1/movies?include_ended=true&date=today", "Le paramètre include_ended est incompatible avec date ou date_to."},
		{"movies ended date to", "/api/v1/movies?include_ended=true&date_to=2026-08-16", "Le paramètre include_ended est incompatible avec date ou date_to."},
		{"movies empty theater", "/api/v1/movies?theaters=", "Le paramètre theaters contient un identifiant de cinéma inconnu."},
		{"movies unknown theater", "/api/v1/movies?theaters=inconnu", "Le paramètre theaters contient un identifiant de cinéma inconnu."},
		{"showtimes date required", "/api/v1/movies/tmdb-film-42/showtimes", "Le paramètre date est requis."},
		{"showtimes scopes", "/api/v1/movies/tmdb-film-42/showtimes?date=2026-08-15&city=Lille&theaters=ugc-25", "Les paramètres city et theaters sont mutuellement exclusifs."},
		{"showtimes unknown theater", "/api/v1/movies/tmdb-film-42/showtimes?date=2026-08-15&theaters=inconnu", "Le paramètre theaters contient un identifiant de cinéma inconnu."},
		{"slot missing scope", "/api/v1/search/slot?date=2026-08-15&start_after=12:00&finish_before=15:00", "Le paramètre city ou theaters est requis."},
		{"slot duplicate scopes", "/api/v1/search/slot?city=Lille&theaters=ugc-25&date=2026-08-15&start_after=12:00&finish_before=15:00", "Les paramètres city et theaters sont mutuellement exclusifs."},
		{"slot empty theater", "/api/v1/search/slot?theaters=&date=2026-08-15&start_after=12:00&finish_before=15:00", "Le paramètre theaters contient un identifiant de cinéma inconnu."},
		{"slot unknown theater", "/api/v1/search/slot?theaters=inconnu&date=2026-08-15&start_after=12:00&finish_before=15:00", "Le paramètre theaters contient un identifiant de cinéma inconnu."},
		{"slot empty format", "/api/v1/search/slot?city=Lille&date=2026-08-15&start_after=12:00&finish_before=15:00&format=", "Le paramètre format doit être ALL, 2D, 3D, IMAX, DOLBY, SCREENX, LASER_ULTRA, 4DX ou ICE."},
		{"slot invalid format", "/api/v1/search/slot?city=Lille&date=2026-08-15&start_after=12:00&finish_before=15:00&format=screenx", "Le paramètre format doit être ALL, 2D, 3D, IMAX, DOLBY, SCREENX, LASER_ULTRA, 4DX ou ICE."},
		{"slot invalid include ads", "/api/v1/search/slot?city=Lille&date=2026-08-15&start_after=12:00&finish_before=15:00&include_ads=0", "Le paramètre include_ads doit être true ou false."},
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
