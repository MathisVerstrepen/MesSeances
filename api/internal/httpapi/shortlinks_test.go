package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"messeances/api/internal/shortlink"
)

type stubShortlinks struct {
	createdTarget string
	createLink    shortlink.Link
	createError   error
	resolvedCode  string
	resolveLink   shortlink.Link
	resolveError  error
}

type acceptingShortlinkStore struct{}

func (acceptingShortlinkStore) Create(context.Context, shortlink.Link) error {
	return nil
}

func (acceptingShortlinkStore) Resolve(context.Context, string) (shortlink.Link, error) {
	return shortlink.Link{}, shortlink.ErrNotFound
}

type legacyCinemasShortlinkStore struct {
	code string
}

func (legacyCinemasShortlinkStore) Create(context.Context, shortlink.Link) error {
	return nil
}

func (s legacyCinemasShortlinkStore) Resolve(context.Context, string) (shortlink.Link, error) {
	return shortlink.Link{Code: s.code, Target: "/cinemas?q=Lille"}, nil
}

func (s *stubShortlinks) Create(_ context.Context, target string) (shortlink.Link, error) {
	s.createdTarget = target
	return s.createLink, s.createError
}

func (s *stubShortlinks) Resolve(_ context.Context, code string) (shortlink.Link, error) {
	s.resolvedCode = code
	return s.resolveLink, s.resolveError
}

func shortlinkHandler(service ShortlinkService) http.Handler {
	return NewHandlerWithOptions(nil, "http://localhost:3000", HandlerOptions{Shortlinks: service})
}

func postShortlink(handler http.Handler, origin, contentType, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/shortlinks", strings.NewReader(body))
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestCreateShortlinkContract(t *testing.T) {
	target := "/films?sort=title&q=Am%C3%A9lie"
	link := shortlink.Link{Code: "AAAAAAAAAAAAAAAAAAAAAA", Target: target}
	service := &stubShortlinks{createLink: link}
	response := postShortlink(shortlinkHandler(service), "http://localhost:3000", "application/json", `{"target":"`+target+`"}`)
	if response.Code != http.StatusCreated || response.Body.String() != `{"code":"AAAAAAAAAAAAAAAAAAAAAA","target":"/films?sort=title&q=Am%C3%A9lie"}`+"\n" || response.Header().Get("Cache-Control") != "no-store" || service.createdTarget != target {
		t.Fatalf("status=%d cache=%q target=%q body=%q", response.Code, response.Header().Get("Cache-Control"), service.createdTarget, response.Body.String())
	}
}

func TestCreateShortlinkValidatesSharedTheatersWithoutChangingResponseShape(t *testing.T) {
	service := shortlink.NewService(acceptingShortlinkStore{}, shortlink.ServiceOptions{Random: bytes.NewReader(make([]byte, 16))})
	handler := shortlinkHandler(service)
	validTarget := "/credits?shared_theaters=ugc-25%2Ckinepolis_42"
	valid := postShortlink(handler, "http://localhost:3000", "application/json", `{"target":"`+validTarget+`"}`)
	if valid.Code != http.StatusCreated || valid.Body.String() != `{"code":"AAAAAAAAAAAAAAAAAAAAAA","target":"/credits?shared_theaters=ugc-25%2Ckinepolis_42"}`+"\n" {
		t.Fatalf("valid status=%d body=%q", valid.Code, valid.Body.String())
	}

	invalid := postShortlink(handler, "http://localhost:3000", "application/json", `{"target":"/credits?shared_theaters=ugc-25,,kinepolis_42"}`)
	if invalid.Code != http.StatusBadRequest || invalid.Body.String() != `{"error":{"code":"invalid_request","message":"La cible de partage est invalide."}}`+"\n" {
		t.Fatalf("invalid status=%d body=%q", invalid.Code, invalid.Body.String())
	}
}

func TestCreateShortlinkRejectsCinemasTarget(t *testing.T) {
	service := shortlink.NewService(acceptingShortlinkStore{}, shortlink.ServiceOptions{Random: bytes.NewReader(make([]byte, 16))})
	response := postShortlink(shortlinkHandler(service), "http://localhost:3000", "application/json", `{"target":"/cinemas?q=Lille&shared_theaters=ugc-25"}`)
	if response.Code != http.StatusBadRequest || response.Body.String() != `{"error":{"code":"invalid_request","message":"La cible de partage est invalide."}}`+"\n" {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestCreateShortlinkRequiresExactOrigin(t *testing.T) {
	for _, origin := range []string{"", "http://evil.example", "http://localhost:3000/", "HTTP://LOCALHOST:3000"} {
		response := postShortlink(shortlinkHandler(&stubShortlinks{}), origin, "application/json", `{"target":"/"}`)
		if response.Code != http.StatusForbidden || response.Body.String() != `{"error":{"code":"origin_forbidden","message":"Origine non autorisée."}}`+"\n" || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("origin=%q status=%d cache=%q body=%q", origin, response.Code, response.Header().Get("Cache-Control"), response.Body.String())
		}
	}
}

func TestCreateShortlinkRejectsInvalidJSON(t *testing.T) {
	large := `{"target":"/` + strings.Repeat("x", 4096) + `"}`
	for _, test := range []struct {
		contentType string
		body        string
	}{
		{contentType: "", body: `{"target":"/"}`},
		{contentType: "application/json; charset=utf-8", body: `{"target":"/"}`},
		{contentType: "text/plain", body: `{"target":"/"}`},
		{contentType: "application/json", body: ""},
		{contentType: "application/json", body: `{}`},
		{contentType: "application/json", body: `{"target":"/","extra":true}`},
		{contentType: "application/json", body: `{"target":"/"} {}`},
		{contentType: "application/json", body: large},
	} {
		response := postShortlink(shortlinkHandler(&stubShortlinks{createError: shortlink.ErrInvalidTarget}), "http://localhost:3000", test.contentType, test.body)
		if response.Code != http.StatusBadRequest || response.Body.String() != `{"error":{"code":"invalid_request","message":"La cible de partage est invalide."}}`+"\n" {
			t.Fatalf("content-type=%q body-size=%d status=%d body=%q", test.contentType, len(test.body), response.Code, response.Body.String())
		}
	}
}

func TestCreateShortlinkErrorMapping(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		body   string
	}{
		{err: shortlink.ErrInvalidTarget, status: http.StatusBadRequest, body: `{"error":{"code":"invalid_request","message":"La cible de partage est invalide."}}` + "\n"},
		{err: shortlink.ErrUnavailable, status: http.StatusServiceUnavailable, body: `{"error":{"code":"shortlink_unavailable","message":"Le service de partage est temporairement indisponible."}}` + "\n"},
		{err: errors.New("secret provider detail"), status: http.StatusInternalServerError, body: `{"error":{"code":"internal_error","message":"Une erreur interne est survenue."}}` + "\n"},
	} {
		response := postShortlink(shortlinkHandler(&stubShortlinks{createError: test.err}), "http://localhost:3000", "application/json", `{"target":"/"}`)
		if response.Code != test.status || response.Body.String() != test.body || strings.Contains(response.Body.String(), "secret") {
			t.Fatalf("err=%v status=%d body=%q", test.err, response.Code, response.Body.String())
		}
	}
}

func TestResolveShortlinkContract(t *testing.T) {
	code := "AAAAAAAAAAAAAAAAAAAAAA"
	link := shortlink.Link{Code: code, Target: "/planning?zoom=12&mode=map"}
	service := &stubShortlinks{resolveLink: link}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/shortlinks/"+code, nil)
	response := httptest.NewRecorder()
	shortlinkHandler(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != `{"code":"AAAAAAAAAAAAAAAAAAAAAA","target":"/planning?zoom=12&mode=map"}`+"\n" || response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || service.resolvedCode != code {
		t.Fatalf("status=%d cache=%q code=%q body=%q", response.Code, response.Header().Get("Cache-Control"), service.resolvedCode, response.Body.String())
	}
}

func TestResolveLegacyCinemasShortlinkIsUnavailable(t *testing.T) {
	code := "AAAAAAAAAAAAAAAAAAAAAA"
	service := shortlink.NewService(legacyCinemasShortlinkStore{code: code}, shortlink.ServiceOptions{})
	response := httptest.NewRecorder()
	shortlinkHandler(service).ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/shortlinks/"+code, nil))
	if response.Code != http.StatusServiceUnavailable || response.Body.String() != `{"error":{"code":"shortlink_unavailable","message":"Le service de partage est temporairement indisponible."}}`+"\n" || response.Header().Get("Cache-Control") != "" {
		t.Fatalf("status=%d cache=%q body=%q", response.Code, response.Header().Get("Cache-Control"), response.Body.String())
	}
}

func TestResolveShortlinkErrorMapping(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{
		{err: shortlink.ErrNotFound, status: http.StatusNotFound, code: "not_found"},
		{err: shortlink.ErrInvalidCode, status: http.StatusNotFound, code: "not_found"},
		{err: shortlink.ErrUnavailable, status: http.StatusServiceUnavailable, code: "shortlink_unavailable"},
		{err: errors.New("secret provider detail"), status: http.StatusInternalServerError, code: "internal_error"},
	} {
		service := &stubShortlinks{resolveError: test.err}
		response := httptest.NewRecorder()
		shortlinkHandler(service).ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/shortlinks/AAAAAAAAAAAAAAAAAAAAAA", nil))
		if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) || strings.Contains(response.Body.String(), "secret") || response.Header().Get("Cache-Control") != "" {
			t.Fatalf("err=%v status=%d cache=%q body=%q", test.err, response.Code, response.Header().Get("Cache-Control"), response.Body.String())
		}
	}
}

func TestShortlinkRoutesRejectWrongMethods(t *testing.T) {
	for _, request := range []*http.Request{
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/shortlinks", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/shortlinks/AAAAAAAAAAAAAAAAAAAAAA", nil),
	} {
		response := httptest.NewRecorder()
		shortlinkHandler(&stubShortlinks{}).ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("method=%s path=%s status=%d body=%q", request.Method, request.URL.Path, response.Code, response.Body.String())
		}
	}
}
