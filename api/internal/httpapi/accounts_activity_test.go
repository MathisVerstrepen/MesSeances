package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/accounts"
	"messeances/api/internal/schedule"
)

const accountActivityPath = "/api/v1/account/activity"

func TestAccountActivityQuery(t *testing.T) {
	for _, raw := range []string{"", "limit=1", "limit=20", "limit=100", "cursor=x", "limit=2&cursor=x", "%6cimit=20"} {
		q, err := parseAccountActivityQuery(raw, false)
		if err != nil || q.Limit < 1 || q.Limit > 100 {
			t.Fatal("valid query", raw, err)
		}
	}
	for _, raw := range []string{"&", "limit=1&", "&limit=1", "limit=0", "limit=01", "limit=-1", "limit=%2B1", "limit=1.0", "limit=101", "limit=1000", "limit=1&limit=2", "cursor=x&cursor=y", "cursor=x&%63ursor=y", "owner=x", "theater_ids=ugc-1", "limit=", "cursor=", "cursor=%FF", "cursor=%", "cursor=x;owner=y", "cursor=" + strings.Repeat("x", 1025), strings.Repeat("x", 2049)} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), "GET", accountActivityPath+"?"+raw, nil))
		if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_input") {
			t.Fatal("invalid query", raw, w.Code)
		}
		assertAccountHeaders(t, w)
	}
	if _, err := parseAccountActivityQuery("", true); err == nil {
		t.Fatal("bare query accepted")
	}
}

func TestAccountActivityRoutesAndCookie(t *testing.T) {
	for _, options := range []AccountOptions{{}, {Enabled: true}, lifecycleHTTPOptions(t)} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: options})
		for _, method := range []string{"GET", "POST", "PUT", "OPTIONS"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), method, accountActivityPath, nil))
			want := 503
			if method != "GET" {
				want = 405
			}
			if w.Code != want {
				t.Fatalf("%s status %d", method, w.Code)
			}
			assertAccountHeaders(t, w)
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("private CORS exposed")
			}
		}
	}
	for _, cookie := range []string{"", "admin=anything", "internal_account=anything", accountCookieName + "=bad", accountCookieName + "=" + strings.Repeat("A", 43) + "; " + accountCookieName + "=" + strings.Repeat("A", 43)} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: followHTTPOptions(t, false)})
		r := httptest.NewRequestWithContext(t.Context(), "GET", accountActivityPath, nil)
		r.Header.Set("Cookie", cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("nonaccount/duplicate identity authorized", w.Code)
		}
		assertAccountHeaders(t, w)
	}
	for _, pending := range []bool{false, true} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: followHTTPOptions(t, pending)})
		r := httptest.NewRequestWithContext(t.Context(), "GET", accountActivityPath, nil)
		r.Header.Set("Cookie", accountCookieName+"="+strings.Repeat("A", 43))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		assertAccountHeaders(t, w)
		if pending {
			if w.Code != 403 {
				t.Fatal("pending authorized")
			}
			continue
		}
		if w.Code != 200 {
			t.Fatal("empty feed", w.Code, w.Body.String())
		}
		var v map[string]any
		if json.Unmarshal(w.Body.Bytes(), &v) != nil || len(v) != 9 || v["username"] != "owner" || v["follows_revision"] != "0" || v["timezone"] != "Europe/Paris" || v["generated_at"] != "2026-10-03T12:00:00.123456Z" || v["next_cursor"] != nil || len(v["items"].([]any)) != 0 {
			t.Fatal("empty DTO", w.Body.String())
		}
	}
}

func TestAccountActivityDTOSerialization(t *testing.T) {
	at := time.Date(2026, 10, 3, 12, 0, 0, 123456000, time.UTC)
	v := accounts.FollowedActivityView{Username: "owner", FollowsRevision: "1", FollowedTheaterCount: 1, GeneratedAt: at, Timezone: schedule.Timezone, Coverage: accounts.FollowedActivityCoverage{InitializedTheaterCount: 1, Completeness: "partial", Bootstrap: "baseline", ReturnMinimumBreakDays: 28}, Limit: 20, Items: []accounts.FollowedActivityItem{{ActivityEvent: schedule.ActivityEvent{EventID: "10", Type: "added_to_program", DetectedAt: at, FirstScreeningDate: "2026-10-03", Movie: schedule.ActivityMovie{Slug: "film-1", Title: "Movie", UpdatedAt: at}}, Theater: accounts.FollowedActivityTheater{ID: "ugc-1", Slug: "ugc-1", Name: "Cinema", City: "Paris", Provider: "ugc"}}}}
	w := httptest.NewRecorder()
	writeJSON(w, 200, v)
	var data map[string]any
	if json.Unmarshal(w.Body.Bytes(), &data) != nil {
		t.Fatal("invalid JSON")
	}
	item := data["items"].([]any)[0].(map[string]any)
	movie := item["movie"].(map[string]any)
	theater := item["theater"].(map[string]any)
	if len(item) != 9 || len(movie) != 4 || len(theater) != 5 || item["event_id"] != "10" || item["detected_at"] != "2026-10-03T12:00:00.123456Z" || item["has_upcoming_showtimes"] != false {
		t.Fatal("item contract", w.Body.String())
	}
	for _, key := range []string{"previous_program_end_date", "next_showtime_date"} {
		if value, ok := item[key]; !ok || value != nil {
			t.Fatal("nullable omitted", key)
		}
	}
	if value, ok := movie["poster_url"]; !ok || value != nil {
		t.Fatal("poster null omitted")
	}
}

func TestAccountActivityLimiter(t *testing.T) {
	h, err := newAccountHTTP(lifecycleHTTPOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	h.activityReads.now = func() time.Time { return now }
	for range 20 {
		w := httptest.NewRecorder()
		accountBoundary(http.HandlerFunc(h.followedActivity)).ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), "GET", accountActivityPath, nil))
		if w.Code != 503 {
			t.Fatal("capacity", w.Code)
		}
	}
	w := httptest.NewRecorder()
	accountBoundary(http.HandlerFunc(h.followedActivity)).ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), "GET", accountActivityPath, nil))
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Fatal("activity rate", w.Code)
	}
	assertAccountHeaders(t, w)
	for _, limiter := range []*tokenBucketLimiter{h.theaters, h.login, h.send, h.step} {
		if ok, _ := limiter.allow(unknownClientKey); !ok {
			t.Fatal("reads consumed unrelated quota")
		}
	}
	now = now.Add(time.Second)
	w = httptest.NewRecorder()
	accountBoundary(http.HandlerFunc(h.followedActivity)).ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), "GET", accountActivityPath, nil))
	if w.Code != 503 {
		t.Fatal("refill")
	}
}

func TestAccountActivityCursorFence(t *testing.T) {
	payload := []byte(`{"v":1,"follows_revision":"1","upper_id":"10","last_detected_at":"2026-10-03T12:00:00.123456Z","last_id":"9"}`)
	sign := func(owner string) string {
		mac := hmac.New(sha256.New, []byte(strings.Repeat("h", 32)))
		_, _ = mac.Write([]byte("account-activity-v1\x00" + owner + "\x00"))
		_, _ = mac.Write(payload)
		return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	}
	for _, tc := range []struct {
		cursor string
		status int
		code   string
	}{
		{sign("1"), 409, "theater_follows_changed"},
		{sign("2"), 400, "invalid_input"},
		{"malformed", 400, "invalid_input"},
		{sign("1") + "A", 400, "invalid_input"},
	} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: followHTTPOptions(t, false)})
		r := httptest.NewRequestWithContext(t.Context(), "GET", accountActivityPath+"?cursor="+tc.cursor, nil)
		r.Header.Set("Cookie", accountCookieName+"="+strings.Repeat("A", 43))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) || strings.Contains(w.Body.String(), "follows_revision") || strings.Contains(w.Body.String(), "owner") {
			t.Fatal("unsafe cursor error contract", w.Code, w.Body.String())
		}
		assertAccountHeaders(t, w)
	}
}
