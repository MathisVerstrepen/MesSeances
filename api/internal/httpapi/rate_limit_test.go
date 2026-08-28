package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/shortlink"
)

func TestTokenBucketContinuousRefillAndCeilingRetryAfter(t *testing.T) {
	if maxRateLimitClients != 10_000 {
		t.Fatalf("production client cap=%d", maxRateLimitClients)
	}
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	limiter := newTokenBucketLimiter(3, 0.5, 6*time.Second, 10, func() time.Time { return now })
	for request := 0; request < 3; request++ {
		if allowed, retryAfter := limiter.allow("client"); !allowed || retryAfter != 0 {
			t.Fatalf("request=%d allowed=%t retry=%d", request, allowed, retryAfter)
		}
	}
	if allowed, retryAfter := limiter.allow("client"); allowed || retryAfter != 2 {
		t.Fatalf("empty bucket allowed=%t retry=%d", allowed, retryAfter)
	}
	now = now.Add(1100 * time.Millisecond)
	if allowed, retryAfter := limiter.allow("client"); allowed || retryAfter != 1 {
		t.Fatalf("partial refill allowed=%t retry=%d", allowed, retryAfter)
	}
	now = now.Add(900 * time.Millisecond)
	if allowed, retryAfter := limiter.allow("client"); !allowed || retryAfter != 0 {
		t.Fatalf("full token allowed=%t retry=%d", allowed, retryAfter)
	}
}

func TestShortlinkCreationProductionPolicy(t *testing.T) {
	if shortlinkCreationBurst != 5 {
		t.Fatalf("short-link creation burst=%d", shortlinkCreationBurst)
	}
	if shortlinkCreationRefillRate != 1.0/144.0 {
		t.Fatalf("short-link creation refill rate=%f", shortlinkCreationRefillRate)
	}
	if shortlinkCreationIdleHorizon != 12*time.Minute {
		t.Fatalf("short-link creation idle horizon=%s", shortlinkCreationIdleHorizon)
	}
	if refilled := shortlinkCreationIdleHorizon.Seconds() * shortlinkCreationRefillRate; refilled != shortlinkCreationBurst {
		t.Fatalf("tokens refilled over idle horizon=%f", refilled)
	}
}

func TestTokenBucketEvictsIdleEntriesAndRejectsUnseenAtHardCap(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	limiter := newTokenBucketLimiter(1, 1, 2*time.Second, 2, func() time.Time { return now })
	for _, key := range []string{"first", "second"} {
		if allowed, _ := limiter.allow(key); !allowed {
			t.Fatalf("initial key %q rejected", key)
		}
	}
	if allowed, retryAfter := limiter.allow("third"); allowed || retryAfter != 2 || len(limiter.clients) != 2 {
		t.Fatalf("hard cap allowed=%t retry=%d clients=%d", allowed, retryAfter, len(limiter.clients))
	}
	if allowed, retryAfter := limiter.allow("first"); allowed || retryAfter != 1 || len(limiter.clients) != 2 {
		t.Fatalf("known key allowed=%t retry=%d clients=%d", allowed, retryAfter, len(limiter.clients))
	}
	now = now.Add(2 * time.Second)
	if allowed, retryAfter := limiter.allow("third"); !allowed || retryAfter != 0 || len(limiter.clients) != 1 {
		t.Fatalf("post-sweep allowed=%t retry=%d clients=%d", allowed, retryAfter, len(limiter.clients))
	}
	if !limiter.nextSweep.Equal(now.Add(2 * time.Second)) {
		t.Fatalf("next sweep=%s", limiter.nextSweep)
	}
}

func TestTokenBucketConcurrentStateRemainsBounded(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	limiter := newTokenBucketLimiter(1, 1, time.Hour, 8, func() time.Time { return now })
	var wait sync.WaitGroup
	for client := 0; client < 100; client++ {
		wait.Add(1)
		go func(client int) {
			defer wait.Done()
			limiter.allow(fmt.Sprintf("client-%d", client))
		}(client)
	}
	wait.Wait()
	if len(limiter.clients) != 8 {
		t.Fatalf("clients=%d", len(limiter.clients))
	}
}

func TestClientIdentifierTrustAndForwardedChain(t *testing.T) {
	trusted := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("2001:db8:1::/48"),
	}
	identifier := newClientIdentifier(trusted)
	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string][]string
		want       string
	}{
		{name: "plain IPv4", remoteAddr: "192.0.2.10:1234", want: "192.0.2.10"},
		{name: "mapped IPv4", remoteAddr: "[::ffff:192.0.2.10]:1234", want: "192.0.2.10"},
		{name: "IPv6", remoteAddr: "[2001:db8:2::10]:1234", want: "2001:db8:2::10"},
		{name: "untrusted spoof ignored", remoteAddr: "192.0.2.10:1234", headers: map[string][]string{"X-Forwarded-For": {"198.51.100.1"}}, want: "192.0.2.10"},
		{name: "trusted single proxy", remoteAddr: "10.0.0.2:1234", headers: map[string][]string{"X-Forwarded-For": {"198.51.100.1"}}, want: "198.51.100.1"},
		{name: "trusted proxy chain", remoteAddr: "10.0.0.3:1234", headers: map[string][]string{"X-Forwarded-For": {"198.51.100.1, 10.0.0.1, 10.0.0.2"}}, want: "198.51.100.1"},
		{name: "nearest untrusted wins", remoteAddr: "10.0.0.3:1234", headers: map[string][]string{"X-Forwarded-For": {"198.51.100.1, 203.0.113.2"}}, want: "203.0.113.2"},
		{name: "all trusted uses leftmost", remoteAddr: "10.0.0.3:1234", headers: map[string][]string{"X-Forwarded-For": {"10.0.0.1, 10.0.0.2"}}, want: "10.0.0.1"},
		{name: "multiple header lines", remoteAddr: "10.0.0.3:1234", headers: map[string][]string{"X-Forwarded-For": {"198.51.100.1", "10.0.0.2"}}, want: "198.51.100.1"},
		{name: "mapped forwarded address", remoteAddr: "10.0.0.3:1234", headers: map[string][]string{"X-Forwarded-For": {"::ffff:198.51.100.1"}}, want: "198.51.100.1"},
		{name: "scoped IPv6 falls back", remoteAddr: "10.0.0.3:1234", headers: map[string][]string{"X-Forwarded-For": {"fe80::1%eth0"}}, want: "10.0.0.3"},
		{name: "oversized IPv6 zone falls back", remoteAddr: "10.0.0.3:1234", headers: map[string][]string{"X-Forwarded-For": {"fe80::1%" + strings.Repeat("z", 1024)}}, want: "10.0.0.3"},
		{name: "oversized header falls back", remoteAddr: "10.0.0.3:1234", headers: map[string][]string{"X-Forwarded-For": {"198.51.100.1" + strings.Repeat(" ", maxForwardedForBytes)}}, want: "10.0.0.3"},
		{name: "malformed member falls back", remoteAddr: "10.0.0.3:1234", headers: map[string][]string{"X-Forwarded-For": {"198.51.100.1,,10.0.0.2"}}, want: "10.0.0.3"},
		{name: "invalid member falls back", remoteAddr: "10.0.0.3:1234", headers: map[string][]string{"X-Forwarded-For": {"not-an-address"}}, want: "10.0.0.3"},
		{name: "unknown peer", remoteAddr: "secret-invalid-peer", headers: map[string][]string{"X-Forwarded-For": {"198.51.100.1"}}, want: unknownClientKey},
		{name: "other forwarding headers ignored", remoteAddr: "10.0.0.3:1234", headers: map[string][]string{"Forwarded": {"for=198.51.100.1"}, "X-Real-IP": {"198.51.100.1"}}, want: "10.0.0.3"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			request.RemoteAddr = test.remoteAddr
			for name, values := range test.headers {
				for _, value := range values {
					request.Header.Add(name, value)
				}
			}
			if got := identifier.key(request); got != test.want {
				t.Fatalf("key=%q want=%q", got, test.want)
			}
		})
	}

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.3:1234"
	forwarded := make([]string, maxForwardedForAddrs+1)
	for index := range forwarded {
		forwarded[index] = fmt.Sprintf("198.51.100.%d", index%254+1)
	}
	request.Header.Set("X-Forwarded-For", strings.Join(forwarded, ","))
	if got := identifier.key(request); got != "10.0.0.3" {
		t.Fatalf("oversized forwarded key=%q", got)
	}
}

func TestForwardedForParserEnforcesAggregateBytesBeforeBoundedMemberScan(t *testing.T) {
	address := "198.51.100.1"
	exactLimit := address + strings.Repeat(" ", maxForwardedForBytes-len(address))
	parsed, ok := forwardedForAddresses([]string{exactLimit})
	if !ok || len(parsed) != 1 || parsed[0].String() != address {
		t.Fatalf("exact-limit parsed=%v ok=%t", parsed, ok)
	}
	if parsed, ok := forwardedForAddresses([]string{exactLimit + " "}); ok || parsed != nil {
		t.Fatalf("over-limit parsed=%v ok=%t", parsed, ok)
	}

	first := address + strings.Repeat(" ", maxForwardedForBytes-1-2*len(address))
	parsed, ok = forwardedForAddresses([]string{first, address})
	if !ok || len(parsed) != 2 || parsed[0].String() != address || parsed[1].String() != address {
		t.Fatalf("exact aggregate limit parsed=%v ok=%t", parsed, ok)
	}
	if parsed, ok := forwardedForAddresses([]string{first, address + " "}); ok || parsed != nil {
		t.Fatalf("aggregate over-limit parsed=%v ok=%t", parsed, ok)
	}

	members := make([]string, maxForwardedForAddrs)
	for index := range members {
		members[index] = address
	}
	parsed, ok = forwardedForAddresses([]string{strings.Join(members, ",")})
	if !ok || len(parsed) != maxForwardedForAddrs {
		t.Fatalf("32 members count=%d ok=%t", len(parsed), ok)
	}
	members = append(members, address)
	if parsed, ok := forwardedForAddresses([]string{strings.Join(members, ",")}); ok || parsed != nil {
		t.Fatalf("33 members parsed=%v ok=%t", parsed, ok)
	}
}

func TestProtectedRouteMatrixSharesExpensiveReadQuota(t *testing.T) {
	now := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	service := &stubShortlinks{resolveLink: shortlink.Link{Code: "AAAAAAAAAAAAAAAAAAAAAA", Target: "/"}}
	handler := testHandlerWithOptions(t, HandlerOptions{Shortlinks: service, RateLimitClock: func() time.Time { return now }})
	protected := []string{
		"/api/v1/timeline?date=2026-08-15",
		"/api/v1/theaters/ugc-lille/showtimes?date=2026-08-15",
		"/api/v1/movies?page_size=1",
		"/api/v1/movies/tmdb-film-42/showtimes?date=2026-08-15",
		"/api/v1/search/slot?date=2026-08-15&start_after=10:00&finish_before=23:00",
	}
	for request := 0; request < expensiveReadBurst; request++ {
		response := requestFrom(t, handler, http.MethodGet, protected[request%len(protected)], "192.0.2.1:1234", nil)
		if response.Code == http.StatusTooManyRequests {
			t.Fatalf("request=%d unexpectedly limited", request)
		}
	}
	headers := http.Header{"Origin": {"http://localhost:3000"}}
	limited := requestFrom(t, handler, http.MethodGet, protected[0], "192.0.2.1:1234", headers)
	assertRateLimited(t, limited, "1")
	if limited.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("CORS origin=%q", limited.Header().Get("Access-Control-Allow-Origin"))
	}

	for _, target := range []string{"/api/v1/theaters", "/api/v1/cities", "/api/v1/cities/lille", "/api/v1/shortlinks/AAAAAAAAAAAAAAAAAAAAAA"} {
		for request := 0; request < expensiveReadBurst+1; request++ {
			response := requestFrom(t, handler, http.MethodGet, target, "192.0.2.1:1234", nil)
			if response.Code == http.StatusTooManyRequests {
				t.Fatalf("excluded target=%q request=%d limited", target, request)
			}
		}
	}
	admin := requestFrom(t, handler, http.MethodGet, "/api/v1/admin/session", "192.0.2.1:1234", nil)
	if admin.Code != http.StatusServiceUnavailable || !strings.Contains(admin.Body.String(), `"code":"admin_unavailable"`) {
		t.Fatalf("admin status=%d body=%q", admin.Code, admin.Body.String())
	}
	independent := requestFrom(t, handler, http.MethodGet, protected[0], "192.0.2.2:1234", nil)
	if independent.Code == http.StatusTooManyRequests {
		t.Fatal("independent client was limited")
	}
	if created := postShortlinkFrom(t, handler, "http://localhost:3000", `{"target":"/"}`); created.Code != http.StatusCreated {
		t.Fatalf("creation quota coupled to expensive reads: status=%d body=%q", created.Code, created.Body.String())
	}
}

func TestPublicHandlerConstructorsEnableBothLimitersByDefault(t *testing.T) {
	constructors := map[string]func() http.Handler{
		"NewHandler":          func() http.Handler { return NewHandler(nil, "http://localhost:3000") },
		"NewHandlerWithAdmin": func() http.Handler { return NewHandlerWithAdmin(nil, "http://localhost:3000", AdminOptions{}) },
	}
	for name, construct := range constructors {
		t.Run(name, func(t *testing.T) {
			handler := construct()
			for request := 0; request < expensiveReadBurst; request++ {
				response := requestFrom(t, handler, http.MethodGet, "/api/v1/movies", "192.0.2.1:1234", nil)
				if response.Code == http.StatusTooManyRequests {
					t.Fatalf("expensive request=%d unexpectedly limited", request)
				}
			}
			assertRateLimited(t, requestFrom(t, handler, http.MethodGet, "/api/v1/movies", "192.0.2.1:1234", nil), "1")

			handler = construct()
			for request := 0; request < shortlinkCreationBurst; request++ {
				response := postShortlinkFrom(t, handler, "http://localhost:3000", `{"target":"/"}`)
				if response.Code == http.StatusTooManyRequests {
					t.Fatalf("creation request=%d unexpectedly limited", request)
				}
			}
			assertRateLimited(t, postShortlinkFrom(t, handler, "http://localhost:3000", `{"target":"/"}`), "144")
		})
	}
}

func TestShortlinkCreationQuotaRunsAfterOriginAndBeforeValidation(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	service := &stubShortlinks{createLink: shortlink.Link{Code: "AAAAAAAAAAAAAAAAAAAAAA", Target: "/"}}
	handler := NewHandlerWithOptions(nil, "http://localhost:3000", HandlerOptions{Shortlinks: service, RateLimitClock: func() time.Time { return now }})
	for request := 0; request < 5; request++ {
		response := postShortlinkFrom(t, handler, "http://evil.example", `{"target":"/"}`)
		if response.Code != http.StatusForbidden {
			t.Fatalf("cross-origin request=%d status=%d", request, response.Code)
		}
	}
	malformed := postShortlinkFrom(t, handler, "http://localhost:3000", `{`)
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed status=%d", malformed.Code)
	}
	for request := 0; request < shortlinkCreationBurst-1; request++ {
		response := postShortlinkFrom(t, handler, "http://localhost:3000", `{"target":"/"}`)
		if response.Code != http.StatusCreated {
			t.Fatalf("valid request=%d status=%d body=%q", request, response.Code, response.Body.String())
		}
	}
	assertRateLimited(t, postShortlinkFrom(t, handler, "http://localhost:3000", `{"target":"/"}`), "144")
	if expensive := requestFrom(t, handler, http.MethodGet, "/api/v1/movies", "192.0.2.1:1234", nil); expensive.Code == http.StatusTooManyRequests {
		t.Fatal("expensive-read quota coupled to shortlink creation")
	}

	now = now.Add(72 * time.Second)
	assertRateLimited(t, postShortlinkFrom(t, handler, "http://localhost:3000", `{"target":"/"}`), "72")
	now = now.Add(72 * time.Second)
	if response := postShortlinkFrom(t, handler, "http://localhost:3000", `{"target":"/"}`); response.Code != http.StatusCreated {
		t.Fatalf("refilled status=%d body=%q", response.Code, response.Body.String())
	}
}

func requestFrom(t *testing.T, handler http.Handler, method, target, remoteAddr string, headers http.Header) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	request.RemoteAddr = remoteAddr
	request.Header = headers.Clone()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func postShortlinkFrom(t *testing.T, handler http.Handler, origin, body string) *httptest.ResponseRecorder {
	t.Helper()
	headers := http.Header{"Content-Type": {"application/json"}, "Origin": {origin}}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/shortlinks", strings.NewReader(body))
	request.RemoteAddr = "192.0.2.1:1234"
	request.Header = headers
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertRateLimited(t *testing.T, response *httptest.ResponseRecorder, retryAfter string) {
	t.Helper()
	wantBody := `{"error":{"code":"rate_limited","message":"Trop de requêtes. Réessayez plus tard."}}` + "\n"
	if response.Code != http.StatusTooManyRequests || response.Body.String() != wantBody || response.Header().Get("Retry-After") != retryAfter || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status=%d retry=%q cache=%q content-type=%q body=%q", response.Code, response.Header().Get("Retry-After"), response.Header().Get("Cache-Control"), response.Header().Get("Content-Type"), response.Body.String())
	}
}
