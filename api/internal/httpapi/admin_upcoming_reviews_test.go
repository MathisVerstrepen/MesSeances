package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/tmdb"
)

type upcomingReviewHTTPStore struct {
	query enrichment.UpcomingReviewQuery
	input enrichment.UpcomingDecisionUpdate
	id    int64
	calls int
	err   error
	empty bool
	item  enrichment.AdminUpcomingMovie
}

func (s *upcomingReviewHTTPStore) UpcomingReviews(_ context.Context, q enrichment.UpcomingReviewQuery, _ time.Time) (enrichment.UpcomingReviewList, error) {
	s.calls++
	s.query = q
	items := []enrichment.AdminUpcomingMovie{}
	if !s.empty {
		items = append(items, s.item)
	}
	return enrichment.UpcomingReviewList{Items: items, Total: int64(len(items)), Limit: q.Limit, Offset: q.Offset}, s.err
}
func (s *upcomingReviewHTTPStore) SetUpcomingDecision(_ context.Context, id int64, input enrichment.UpcomingDecisionUpdate, _ time.Time) (enrichment.AdminUpcomingMovie, error) {
	s.calls++
	s.id = id
	s.input = input
	result := s.item
	result.Decision = input.Decision
	result.Revision++
	result.PubliclyVisible = input.Decision != "excluded"
	return result, s.err
}
func reviewHTTPFixture(t *testing.T) (http.Handler, *http.Cookie, *upcomingReviewHTTPStore) {
	t.Helper()
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	date := "2026-10-07"
	store := &upcomingReviewHTTPStore{item: enrichment.AdminUpcomingMovie{TMDBID: 42, PublicMovieID: "7", Slug: "film-7", Title: "Film test", FrenchReleaseDate: &date, Active: true, InWindow: true, PubliclyVisible: true, AssessmentStatus: "assessed", AssessedAt: &now, FrenchReleases: []tmdb.FrenchReleaseRow{{Type: 2, Date: date, Note: "Séance unique"}}, ReasonCodes: []string{"limited_only", "single_screening_note"}, Decision: "unreviewed", Revision: 1}}
	h := testHandlerWithAdmin(t, AdminOptions{Password: "password", SessionSecret: "test-session-secret", Reviews: enrichment.NewReviewService(adminReviewStore{}, nil, nil), UpcomingReviews: enrichment.NewUpcomingReviewService(store, func() time.Time { return now }), Now: func() time.Time { return now }})
	return h, loginAdmin(t, h, "password"), store
}

func TestAdminUpcomingReviewsWireAndQueries(t *testing.T) {
	h, cookie, store := reviewHTTPFixture(t)
	const path = "/api/v1/admin/tmdb-upcoming-movies"
	response := adminRequest(h, http.MethodGet, path, "", "", cookie)
	const item = `{"tmdb_id":42,"public_movie_id":"7","slug":"film-7","title":"Film test","poster_url":null,"french_release_date":"2026-10-07","active":true,"in_window":true,"publicly_visible":true,"assessment_status":"assessed","assessed_at":"2026-09-13T12:00:00Z","french_releases":[{"type":2,"date":"2026-10-07","note":"Séance unique"}],"reason_codes":["limited_only","single_screening_note"],"decision":"unreviewed","revision":1}`
	if response.Code != 200 || strings.TrimSpace(response.Body.String()) != `{"items":[`+item+`],"total":1,"limit":50,"offset":0}` || response.Header().Get("Cache-Control") != "no-store" || store.query != (enrichment.UpcomingReviewQuery{Filter: "needs_review", Limit: 50}) {
		t.Fatalf("wire %d %s %+v", response.Code, response.Body.String(), store.query)
	}
	for _, filter := range []string{"needs_review", "pending_assessment", "approved", "excluded", "all"} {
		response = adminRequest(h, http.MethodGet, path+"?filter="+filter+"&search="+url.QueryEscape("  %_\\42  ")+"&limit=100&offset=2147483647", "", "", cookie)
		if response.Code != 200 || store.query != (enrichment.UpcomingReviewQuery{Filter: filter, Search: "%_\\42", Limit: 100, Offset: 2147483647}) {
			t.Fatalf("query %d %+v", response.Code, store.query)
		}
	}
	for _, query := range []string{"unknown=1", "filter=all&filter=all", "search=a&search=b", "limit=1&limit=2", "offset=0&offset=1", "filter=", "filter=ALL", "filter=%20all%20", "search=%20", "search=%FF", "search=%00", "search=" + strings.Repeat("é", 1025), "limit=0", "limit=101", "limit=-1", "limit=+1", "limit=1.0", "limit=1e1", "limit=999999999999999999999", "offset=-1", "offset=2147483648", "offset=", "x=%ZZ", "filter=all;limit=1"} {
		before := store.calls
		response = adminRequest(h, http.MethodGet, path+"?"+query, "", "", cookie)
		if response.Code != 400 || !strings.Contains(response.Body.String(), `"code":"invalid_upcoming_review_query"`) || store.calls != before || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("invalid %q: %d %s", query, response.Code, response.Body.String())
		}
	}
	store.empty = true
	response = adminRequest(h, http.MethodGet, path, "", "", cookie)
	if strings.TrimSpace(response.Body.String()) != `{"items":[],"total":0,"limit":50,"offset":0}` {
		t.Fatal(response.Body.String())
	}
	// Pending nullable dates and evidence arrays have a distinct shape, not an assessed clean bill.
	store.empty = false
	store.item.FrenchReleaseDate = nil
	store.item.AssessedAt = nil
	store.item.FrenchReleases = []tmdb.FrenchReleaseRow{}
	store.item.ReasonCodes = []string{}
	store.item.AssessmentStatus = "pending"
	response = adminRequest(h, http.MethodGet, path, "", "", cookie)
	for _, fragment := range []string{`"french_release_date":null`, `"assessed_at":null`, `"french_releases":[]`, `"reason_codes":[]`, `"assessment_status":"pending"`} {
		if !strings.Contains(response.Body.String(), fragment) {
			t.Fatal(response.Body.String())
		}
	}
}

func TestAdminUpcomingReviewPosterWire(t *testing.T) {
	poster := "https://image.tmdb.org/t/p/w500/poster.jpg"
	for _, test := range []struct {
		name   string
		poster *string
		wire   string
	}{
		{name: "missing", wire: "null"},
		{name: "populated", poster: &poster, wire: `"` + poster + `"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, cookie, store := reviewHTTPFixture(t)
			store.item.PosterURL = test.poster
			for _, method := range []string{http.MethodGet, http.MethodPatch} {
				path, body := "/api/v1/admin/tmdb-upcoming-movies", ""
				if method == http.MethodPatch {
					path += "/42/decision"
					body = `{"decision":"approved","expected_revision":1}`
				}
				response := adminRequest(h, method, path, body, "http://localhost:3000", cookie)
				if response.Code != http.StatusOK {
					t.Fatalf("%s: %d %s", method, response.Code, response.Body.String())
				}
				var item map[string]json.RawMessage
				if method == http.MethodGet {
					var list struct {
						Items []map[string]json.RawMessage `json:"items"`
					}
					if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil || len(list.Items) != 1 {
						t.Fatalf("list: %s, error=%v", response.Body.String(), err)
					}
					item = list.Items[0]
				} else if err := json.Unmarshal(response.Body.Bytes(), &item); err != nil {
					t.Fatal(err)
				}
				if got := string(item["poster_url"]); got != test.wire {
					t.Fatalf("%s poster_url=%s, want %s", method, got, test.wire)
				}
			}
		})
	}
}

func TestAdminUpcomingDecisionWireAndValidation(t *testing.T) {
	h, cookie, store := reviewHTTPFixture(t)
	const path = "/api/v1/admin/tmdb-upcoming-movies/42/decision"
	for _, decision := range []string{"approved", "excluded", "unreviewed"} {
		response := adminRequest(h, http.MethodPatch, path, `{"decision":"`+decision+`","expected_revision":1}`, "http://localhost:3000", cookie)
		var got enrichment.AdminUpcomingMovie
		if json.Unmarshal(response.Body.Bytes(), &got) != nil || response.Code != 200 || got.TMDBID != 42 || got.Decision != decision || got.Revision != 2 || !reflect.DeepEqual(got.FrenchReleases, store.item.FrenchReleases) || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("mutation %d %s", response.Code, response.Body.String())
		}
	}
	for _, body := range []string{"", `null`, `[]`, `{}`, `{"decision":"approved"}`, `{"expected_revision":1}`, `{"decision":null,"expected_revision":1}`, `{"decision":"approved","expected_revision":null}`, `{"decision":1,"expected_revision":1}`, `{"decision":"hidden","expected_revision":1}`, `{"decision":"approved","expected_revision":"1"}`, `{"decision":"approved","expected_revision":1.0}`, `{"decision":"approved","expected_revision":1e0}`, `{"decision":"approved","expected_revision":0}`, `{"decision":"approved","expected_revision":-1}`, `{"decision":"approved","expected_revision":9007199254740992}`, `{"decision":"approved","expected_revision":9223372036854775808}`, `{"decision":"approved","expected_revision":1,"x":1}`, `{"decision":"approved","expected_revision":1} {}`, `{"decision":"approved","decision":"excluded","expected_revision":1}`, `{"Decision":"approved","expected_revision":1}`, `{"decision":"approved","expected_revision":1}` + strings.Repeat(" ", 4096)} {
		before := store.calls
		response := adminRequest(h, http.MethodPatch, path, body, "http://localhost:3000", cookie)
		if response.Code != 400 || !strings.Contains(response.Body.String(), `"code":"invalid_upcoming_review_update"`) || store.calls != before || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("invalid body %q: %d %s", body, response.Code, response.Body.String())
		}
	}
	const body = `{"decision":"excluded","expected_revision":1}`
	for _, id := range []string{"0", "-1", "+1", "01", "1.0", "1e0", "9007199254740992", "9223372036854775808", "abc"} {
		response := adminRequest(h, http.MethodPatch, "/api/v1/admin/tmdb-upcoming-movies/"+id+"/decision", body, "http://localhost:3000", cookie)
		if response.Code != 400 || !strings.Contains(response.Body.String(), `"code":"invalid_upcoming_review_id"`) {
			t.Fatalf("ID %q %d %s", id, response.Code, response.Body.String())
		}
	}
	for _, suffix := range []string{"?x=1", "?", "?%ZZ"} {
		response := adminRequest(h, http.MethodPatch, path+suffix, body, "http://localhost:3000", cookie)
		if response.Code != 400 || !strings.Contains(response.Body.String(), "invalid_upcoming_review_update") {
			t.Fatalf("query %q %s", suffix, response.Body.String())
		}
	}
	for _, contentType := range []string{"", "text/plain", "application/json;bad"} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, path, strings.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("Origin", "http://localhost:3000")
		r.Header.Set("Content-Type", contentType)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, r)
		if response.Code != 400 {
			t.Fatalf("content type %q: %d", contentType, response.Code)
		}
	}
	response := adminRequest(h, http.MethodPatch, "/api/v1/admin/tmdb-upcoming-movies/9007199254740991/decision", `{"decision":"approved","expected_revision":9007199254740991}`, "http://localhost:3000", cookie)
	if response.Code != 200 || store.id != enrichment.MaxReviewInteger || store.input.ExpectedRevision != enrichment.MaxReviewInteger {
		t.Fatal("safe integer boundary rejected")
	}
}

func TestAdminUpcomingReviewsProtectionAndSafeErrors(t *testing.T) {
	h, cookie, store := reviewHTTPFixture(t)
	const list = "/api/v1/admin/tmdb-upcoming-movies"
	const patch = list + "/42/decision"
	const body = `{"decision":"excluded","expected_revision":1}`
	for _, test := range []struct {
		method, path, body, origin string
		auth                       bool
		status                     int
		code                       string
	}{
		{http.MethodGet, list, "", "", false, 401, "unauthorized"},
		{http.MethodPatch, patch, body, "http://localhost:3000", false, 401, "unauthorized"},
		{http.MethodPatch, patch, body, "https://evil.example", true, 403, "origin_forbidden"},
		{http.MethodPatch, patch, body, "", true, 403, "origin_forbidden"},
	} {
		var auth *http.Cookie
		if test.auth {
			auth = cookie
		}
		before := store.calls
		response := adminRequest(h, test.method, test.path, test.body, test.origin, auth)
		if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) || response.Header().Get("Cache-Control") != "no-store" || store.calls != before {
			t.Fatalf("protection %d %s", response.Code, response.Body.String())
		}
	}
	for _, test := range []struct {
		method, path, body string
		err                error
		status             int
		code               string
	}{
		{http.MethodGet, list, "", errors.New("SQL secret provider body"), 500, "upcoming_review_list_failed"},
		{http.MethodPatch, patch, body, errors.New("SQL secret provider body"), 500, "upcoming_review_update_failed"},
		{http.MethodPatch, patch, body, enrichment.ErrUpcomingReviewConflict, 409, "upcoming_review_conflict"},
		{http.MethodPatch, patch, body, enrichment.ErrUpcomingReviewNotFound, 404, "upcoming_review_not_found"},
	} {
		store.err = test.err
		response := adminRequest(h, test.method, test.path, test.body, "http://localhost:3000", cookie)
		if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) || strings.Contains(response.Body.String(), "secret") || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("safe error %d %s", response.Code, response.Body.String())
		}
	}
	unavailable := configuredAdminHandler(t, "password", nil)
	otherCookie := loginAdmin(t, unavailable, "password")
	for _, method := range []string{http.MethodGet, http.MethodPatch} {
		path, payload := list, ""
		if method == http.MethodPatch {
			path, payload = patch, body
		}
		response := adminRequest(unavailable, method, path, payload, "http://localhost:3000", otherCookie)
		if response.Code != 503 || !strings.Contains(response.Body.String(), `"code":"admin_unavailable"`) || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("unavailable %d %s", response.Code, response.Body.String())
		}
	}
}
