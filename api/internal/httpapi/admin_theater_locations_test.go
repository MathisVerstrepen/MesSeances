package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/geocoding"
)

type theaterLocationControllerFake struct {
	items       []geocoding.PendingLocation
	err         error
	limit       int
	offset      int
	acceptCalls int
	manualCalls int
	provider    string
	theaterID   string
	expectedAt  time.Time
	latitude    float64
	longitude   float64
}

func (f *theaterLocationControllerFake) Pending(_ context.Context, limit, offset int) ([]geocoding.PendingLocation, error) {
	f.limit, f.offset = limit, offset
	return f.items, f.err
}

func (f *theaterLocationControllerFake) AcceptSuggestion(_ context.Context, provider, theaterID string, expectedAt time.Time) error {
	f.acceptCalls++
	f.provider, f.theaterID, f.expectedAt = provider, theaterID, expectedAt
	return f.err
}

func (f *theaterLocationControllerFake) SetManual(_ context.Context, provider, theaterID string, expectedAt time.Time, latitude, longitude float64) error {
	f.manualCalls++
	f.provider, f.theaterID, f.expectedAt, f.latitude, f.longitude = provider, theaterID, expectedAt, latitude, longitude
	return f.err
}

func theaterLocationAdminHandler(t *testing.T, controller TheaterLocationController) http.Handler {
	t.Helper()
	reviews := enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, time.Now)
	return testHandlerWithAdmin(t, AdminOptions{Password: "password", SessionSecret: "test-session-secret", Reviews: reviews, TheaterLocations: controller})
}

func TestAdminTheaterLocationsRequiresAuthAndReturnsStoredSuggestion(t *testing.T) {
	latitude, longitude, postalCode, city, candidateType := 50.63, 3.06, "59000", "Lille", "street"
	updatedAt := time.Date(2026, 8, 26, 12, 0, 0, 123000000, time.UTC)
	controller := &theaterLocationControllerFake{items: []geocoding.PendingLocation{{
		Provider: "cgr", ProviderTheaterID: "A1234", TheaterID: "cgr-A1234", Name: "CGR Lille",
		Address: "Rue du cinéma", PostalCode: "59000", City: "Lille", Status: geocoding.StatusAmbiguous, UpdatedAt: updatedAt,
		Suggestion:          &geocoding.ResolutionSuggestion{Label: "Rue du cinéma", Score: .81, Latitude: &latitude, Longitude: &longitude, PostalCode: &postalCode, City: &city, Type: &candidateType},
		CanAcceptSuggestion: true,
	}}}
	handler := theaterLocationAdminHandler(t, controller)
	unauthorized := adminRequest(handler, http.MethodGet, "/api/v1/admin/theater-locations", "", "", nil)
	assertAPIError(t, unauthorized, http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	if unauthorized.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unauthorized Cache-Control=%q", unauthorized.Header().Get("Cache-Control"))
	}
	cookie := loginAdmin(t, handler, "password")
	response := adminRequest(handler, http.MethodGet, "/api/v1/admin/theater-locations", "", "", cookie)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || controller.limit != 20 || controller.offset != 0 {
		t.Fatalf("status=%d headers=%v pagination=%d/%d body=%s", response.Code, response.Header(), controller.limit, controller.offset, response.Body.String())
	}
	want := `{"items":[{"provider":"cgr","provider_theater_id":"A1234","theater_id":"cgr-A1234","name":"CGR Lille","address":"Rue du cinéma","postal_code":"59000","city":"Lille","status":"ambiguous","updated_at":"2026-08-26T12:00:00.123Z","suggestion":{"label":"Rue du cinéma","score":0.81,"latitude":50.63,"longitude":3.06,"postal_code":"59000","city":"Lille","type":"street"},"can_accept_suggestion":true}],"limit":20,"offset":0}`
	if strings.TrimSpace(response.Body.String()) != want {
		t.Fatalf("body=%s", response.Body.String())
	}
}

func TestAdminTheaterLocationPaginationAndUnavailableController(t *testing.T) {
	controller := &theaterLocationControllerFake{}
	handler := theaterLocationAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")
	valid := adminRequest(handler, http.MethodGet, "/api/v1/admin/theater-locations?limit=100&offset=12", "", "", cookie)
	if valid.Code != http.StatusOK || controller.limit != 100 || controller.offset != 12 {
		t.Fatalf("valid status=%d pagination=%d/%d", valid.Code, controller.limit, controller.offset)
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?offset=-1", "?limit=x", "?limit=20&limit=30", "?unknown=1", "?offset="} {
		response := adminRequest(handler, http.MethodGet, "/api/v1/admin/theater-locations"+query, "", "", cookie)
		assertAPIError(t, response, http.StatusBadRequest, "invalid_query", "Pagination invalide.")
	}
	unavailableHandler := theaterLocationAdminHandler(t, nil)
	unavailableCookie := loginAdmin(t, unavailableHandler, "password")
	unavailable := adminRequest(unavailableHandler, http.MethodGet, "/api/v1/admin/theater-locations", "", "", unavailableCookie)
	assertAPIError(t, unavailable, http.StatusServiceUnavailable, "theater_location_unavailable", "Service de localisation indisponible.")
}

func TestAdminTheaterLocationWritesEnforceOriginAndTrustStoredSuggestion(t *testing.T) {
	controller := &theaterLocationControllerFake{}
	handler := theaterLocationAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")
	timestamp := "2026-08-26T12:00:00.123Z"
	target := "/api/v1/admin/theater-locations/cgr/A1234/accept-suggestion"
	for _, origin := range []string{"", "https://evil.example"} {
		response := adminRequest(handler, http.MethodPost, target, `{"expected_updated_at":"`+timestamp+`"}`, origin, cookie)
		assertAPIError(t, response, http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")
	}
	accepted := adminRequest(handler, http.MethodPost, target, `{"expected_updated_at":"`+timestamp+`"}`, "http://localhost:3000", cookie)
	if accepted.Code != http.StatusOK || accepted.Header().Get("Cache-Control") != "no-store" || strings.TrimSpace(accepted.Body.String()) != `{"status":"manual"}` || controller.acceptCalls != 1 || controller.provider != "cgr" || controller.theaterID != "A1234" || controller.expectedAt.Format(time.RFC3339Nano) != timestamp {
		t.Fatalf("accept status=%d body=%s fake=%+v", accepted.Code, accepted.Body.String(), controller)
	}
	manual := adminRequest(handler, http.MethodPost, "/api/v1/admin/theater-locations/ugc/25/manual", `{"expected_updated_at":"`+timestamp+`","latitude":-90,"longitude":180}`, "http://localhost:3000", cookie)
	if manual.Code != http.StatusOK || controller.manualCalls != 1 || controller.latitude != -90 || controller.longitude != 180 {
		t.Fatalf("manual status=%d body=%s fake=%+v", manual.Code, manual.Body.String(), controller)
	}
}

func TestAdminTheaterLocationWritesRejectMalformedInputs(t *testing.T) {
	controller := &theaterLocationControllerFake{}
	handler := theaterLocationAdminHandler(t, controller)
	cookie := loginAdmin(t, handler, "password")
	timestamp := "2026-08-26T12:00:00Z"
	tests := []struct{ target, body string }{
		{"/api/v1/admin/theater-locations/other/25/accept-suggestion", `{"expected_updated_at":"` + timestamp + `"}`},
		{"/api/v1/admin/theater-locations/cgr/12345/accept-suggestion", `{"expected_updated_at":"` + timestamp + `"}`},
		{"/api/v1/admin/theater-locations/ugc/25/accept-suggestion", `{}`},
		{"/api/v1/admin/theater-locations/ugc/25/accept-suggestion", `{"expected_updated_at":"bad"}`},
		{"/api/v1/admin/theater-locations/ugc/25/accept-suggestion", `{"expected_updated_at":"` + timestamp + `","latitude":1}`},
		{"/api/v1/admin/theater-locations/ugc/25/accept-suggestion", `{"expected_updated_at":"` + timestamp + `"} {}`},
		{"/api/v1/admin/theater-locations/ugc/25/manual", `{"expected_updated_at":"` + timestamp + `","longitude":3}`},
		{"/api/v1/admin/theater-locations/ugc/25/manual", `{"expected_updated_at":"` + timestamp + `","latitude":91,"longitude":3}`},
		{"/api/v1/admin/theater-locations/ugc/25/manual", `{"expected_updated_at":"` + timestamp + `","latitude":50,"longitude":181}`},
		{"/api/v1/admin/theater-locations/ugc/25/manual", `{"expected_updated_at":"` + timestamp + `","latitude":50,"longitude":3,"unknown":true}`},
		{"/api/v1/admin/theater-locations/ugc/25/manual", `{"expected_updated_at":"` + timestamp + `","latitude":50,"longitude":3,"padding":"` + strings.Repeat("x", maxAdminBody) + `"}`},
	}
	for _, test := range tests {
		response := adminRequest(handler, http.MethodPost, test.target, test.body, "http://localhost:3000", cookie)
		assertAPIError(t, response, http.StatusBadRequest, "invalid_request", "Requête invalide.")
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/theater-locations/ugc/25/manual", strings.NewReader(`{"expected_updated_at":"`+timestamp+`","latitude":50,"longitude":3}`))
	request.Header.Set("Content-Type", "text/plain")
	request.Header.Set("Origin", "http://localhost:3000")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusBadRequest, "invalid_request", "Requête invalide.")
	if controller.acceptCalls != 0 || controller.manualCalls != 0 {
		t.Fatalf("controller called accept=%d manual=%d", controller.acceptCalls, controller.manualCalls)
	}
}

func TestAdminTheaterLocationErrorMappings(t *testing.T) {
	tests := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{geocoding.ErrResolutionInvalid, http.StatusBadRequest, "invalid_request"},
		{geocoding.ErrResolutionNotFound, http.StatusNotFound, "theater_location_not_found"},
		{geocoding.ErrResolutionConflict, http.StatusConflict, "theater_location_conflict"},
		{geocoding.ErrResolutionUnavailable, http.StatusServiceUnavailable, "theater_location_unavailable"},
		{errors.New("database secret"), http.StatusInternalServerError, "internal_error"},
	}
	for _, test := range tests {
		controller := &theaterLocationControllerFake{err: test.err}
		handler := theaterLocationAdminHandler(t, controller)
		cookie := loginAdmin(t, handler, "password")
		response := adminRequest(handler, http.MethodPost, "/api/v1/admin/theater-locations/ugc/25/manual", `{"expected_updated_at":"2026-08-26T12:00:00Z","latitude":50,"longitude":3}`, "http://localhost:3000", cookie)
		if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) || strings.Contains(response.Body.String(), "database secret") {
			t.Fatalf("err=%v status=%d body=%s", test.err, response.Code, response.Body.String())
		}
	}
}
