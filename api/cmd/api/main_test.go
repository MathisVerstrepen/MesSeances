package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	runtimeconfig "messeances/api/internal/config"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/geocoding"
	"messeances/api/internal/httpapi"
	"messeances/api/internal/observability"
	"messeances/api/internal/schedule"
	"messeances/api/internal/shortlink"
	"messeances/api/internal/syncproxy"
	"messeances/api/internal/tmdb"
)

type testReadCloser struct {
	io.Reader
	closeErr error
}

func (r testReadCloser) Close() error { return r.closeErr }

type testHTTPServer struct {
	serveStarted chan struct{}
	serveRelease chan error
	shutdown     func(context.Context) error
}

type testShortlinkService struct{}

type testTMDBProvider struct{}

type testCloseableWorker struct {
	close func()
}

type testSnapshotWriter struct{}

type testGeocodingStore struct{}

type testShortlinkRetentionStore struct {
	purge func(context.Context, time.Time) error
}

func (testSnapshotWriter) Replace(context.Context, []schedule.Dataset) (schedule.PublicationResult, error) {
	return schedule.PublicationResult{}, nil
}

func (testGeocodingStore) Select(context.Context) ([]geocoding.Theater, error) {
	return nil, nil
}

func (testGeocodingStore) Save(context.Context, *geocoding.Location, geocoding.Location) (bool, error) {
	return false, nil
}

func (s testShortlinkRetentionStore) PurgeCreatedBefore(ctx context.Context, cutoff time.Time) error {
	return s.purge(ctx, cutoff)
}

func (w testCloseableWorker) Close() { w.close() }

func (testTMDBProvider) Search(context.Context, string) ([]tmdb.Candidate, error) { return nil, nil }
func (testTMDBProvider) Details(context.Context, int64) (tmdb.Details, error) {
	return tmdb.Details{}, nil
}

func (testShortlinkService) Create(_ context.Context, target string) (shortlink.Link, error) {
	return shortlink.Link{Code: "AAAAAAAAAAAAAAAAAAAAAA", Target: target}, nil
}

func (testShortlinkService) Resolve(_ context.Context, code string) (shortlink.Link, error) {
	return shortlink.Link{Code: code, Target: "/"}, nil
}

func (s *testHTTPServer) ListenAndServe() error {
	close(s.serveStarted)
	return <-s.serveRelease
}

func (s *testHTTPServer) Shutdown(ctx context.Context) error {
	return s.shutdown(ctx)
}

func TestLoadSyncProxiesConfiguration(t *testing.T) {
	called := false
	proxies, err := loadSyncProxies("", func(string) (io.ReadCloser, error) {
		called = true
		return nil, errors.New("must not open")
	})
	if err != nil || proxies != nil || called {
		t.Fatalf("missing proxies=%v err=%v called=%t", proxies, err, called)
	}

	secret := "synthetic-user:synthetic-password"
	_, err = loadSyncProxies("/secret/path", func(path string) (io.ReadCloser, error) {
		if path != "/secret/path" {
			t.Fatalf("path=%q", path)
		}
		return testReadCloser{Reader: strings.NewReader(secret)}, nil
	})
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "/secret/path") {
		t.Fatalf("parse err=%v", err)
	}

	_, err = loadSyncProxies("/secret/path", func(string) (io.ReadCloser, error) {
		return nil, errors.New("contains synthetic-password")
	})
	if err == nil || strings.Contains(err.Error(), "synthetic") || strings.Contains(err.Error(), "/secret/path") {
		t.Fatalf("open err=%v", err)
	}

	proxies, err = loadSyncProxies("configured", func(string) (io.ReadCloser, error) {
		return testReadCloser{Reader: strings.NewReader("http://user:password@127.0.0.1:8080\n")}, nil
	})
	if err != nil || len(proxies) != 1 {
		t.Fatalf("valid count=%d err=%v", len(proxies), err)
	}
}

func TestLoadAPIConfigurationIgnoresSyncTimingWhenCapabilityDisabled(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":                    "postgres://configured",
		"SYNC_REQUEST_TIMEOUT":            "malformed-secret",
		"SYNC_KINEPOLIS_REQUEST_INTERVAL": "malformed-secret",
		"SYNC_OPERATION_TIMEOUT":          "malformed-secret",
	}
	getenv := func(name string) string { return values[name] }
	cfg, syncConfig, err := loadAPIConfiguration(getenv)
	if err != nil || cfg.Proxy.Path != "" || syncConfig.Sync.OperationTimeout != 0 {
		t.Fatalf("cfg=%+v sync=%+v err=%v", cfg, syncConfig, err)
	}
	values["PROXY_FILE"] = "/configured/proxies.txt"
	if _, _, err := loadAPIConfiguration(getenv); err == nil || strings.Contains(err.Error(), "malformed-secret") {
		t.Fatalf("enabled capability err=%v", err)
	}
}

func TestProductionScheduleOptionsDefaultToParisAndRetainLilleMetroAlias(t *testing.T) {
	options := newProductionScheduleOptions()
	if options.DefaultCity != "Paris" {
		t.Fatalf("default city=%q", options.DefaultCity)
	}
	if len(options.CityAliases) != 1 {
		t.Fatalf("city aliases=%v", options.CityAliases)
	}
	lille, ok := options.CityAliases["Lille"]
	if !ok || len(lille) != 2 || lille[0] != "Lille" || lille[1] != "Villeneuve d'Ascq" {
		t.Fatalf("Lille aliases=%v present=%t", lille, ok)
	}
}

func TestSyncExecutorOptionsWireAllProviderFactories(t *testing.T) {
	proxies, err := syncproxy.Parse(strings.NewReader("127.0.0.1:8080\n"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg runtimeconfig.Config
	cfg.Sync.RequestTimeout = 7 * time.Second
	cfg.Sync.KinepolisRequestInterval = 3 * time.Second
	cfg.Sync.OperationTimeout = 37 * time.Second
	options := newSyncExecutorOptions(testSnapshotWriter{}, proxies, cfg, nil, time.Now, observability.NewLogger(io.Discard), nil)
	if options.NewUGC == nil || options.NewKinepolis == nil || options.NewPathe == nil || options.NewCGR == nil || options.OperationTimeout != cfg.Sync.OperationTimeout {
		t.Fatalf("options=%+v", options)
	}
	if client, err := options.NewPathe(); err != nil || client == nil {
		t.Fatalf("Pathé client=%v err=%v", client, err)
	}
	if client, err := options.NewCGR(); err != nil || client == nil {
		t.Fatalf("CGR client=%v err=%v", client, err)
	}
}

func TestAPIStartupRejectsNonCanonicalOriginsBeforeAuthHandoff(t *testing.T) {
	for _, origin := range []string{"https://EXAMPLE.com", "https://example.com:443", "http://example.com:80"} {
		values := map[string]string{
			"DATABASE_URL":         "postgres://configured",
			"WEB_ORIGIN":           origin,
			"ADMIN_PASSWORD":       "password",
			"ADMIN_SESSION_SECRET": "session-secret",
		}
		_, _, err := loadAPIConfiguration(func(name string) string { return values[name] })
		if err == nil || err.Error() != "configuration error" || strings.Contains(err.Error(), origin) {
			t.Fatalf("origin=%q err=%v", origin, err)
		}
	}
}

func TestCanonicalStartupOriginReachesAdminAuthAndCORS(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":         "postgres://configured",
		"WEB_ORIGIN":           "https://example.com:8443",
		"ADMIN_PASSWORD":       "password",
		"ADMIN_SESSION_SECRET": "session-secret",
	}
	cfg, _, err := loadAPIConfiguration(func(name string) string { return values[name] })
	if err != nil {
		t.Fatal(err)
	}
	adminOptions, manager, err := newAdminOptions(context.Background(), cfg.Admin.Password, cfg.Admin.SessionSecret, enrichment.NewPostgresStore(nil), nil)
	if err != nil || manager != nil {
		t.Fatalf("admin options manager=%v err=%v", manager, err)
	}
	adminOptions.Now = time.Now
	handler := newAPIHandler(nil, cfg, adminOptions, nil, httpapi.ReadinessOptions{})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/login", strings.NewReader(`{"password":"password"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", cfg.Server.Origin)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Access-Control-Allow-Origin") != cfg.Server.Origin || len(response.Result().Cookies()) != 1 {
		t.Fatalf("status=%d headers=%v body=%s cookies=%v", response.Code, response.Header(), response.Body.String(), response.Result().Cookies())
	}
}

func TestNewAPIHandlerWiresInternalSharedSecret(t *testing.T) {
	secret := strings.Repeat("a", 64)
	var cfg runtimeconfig.Config
	cfg.Server.Origin = "http://localhost:3000"
	cfg.Internal.SharedSecret = secret
	handler := newAPIHandler(nil, cfg, httpapi.AdminOptions{}, nil, httpapi.ReadinessOptions{})
	target := "/api/v1/internal/movies/tmdb-film-42/showtimes-bundle?date=2026-08-15&city=Paris"

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	request.Header.Set("X-Messeances-Internal-Token", secret)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"schedule_unavailable"`) {
		t.Fatalf("configured status=%d body=%q", response.Code, response.Body.String())
	}

	request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("missing credential status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestNewAdminOptionsWiresLocalMoviesWithoutTMDBProvider(t *testing.T) {
	store := enrichment.NewPostgresStore(nil)
	options, manager, err := newAdminOptions(context.Background(), "password", "session-secret", store, nil)
	if err != nil || manager != nil {
		t.Fatalf("without provider manager=%v err=%v", manager, err)
	}
	if options.Password != "password" || options.Reviews == nil || options.LocalMovies == nil || options.Movies == nil || options.TMDBReruns != nil || options.TMDBRefreshes != nil {
		t.Fatalf("options=%+v", options)
	}
	withProvider, manager, err := newAdminOptions(context.Background(), "password", "session-secret", store, testTMDBProvider{})
	if err != nil || manager == nil {
		t.Fatalf("with provider manager=%v err=%v", manager, err)
	}
	defer manager.Close()
	if withProvider.TMDBReruns == nil || withProvider.TMDBRefreshes == nil || withProvider.Reviews == nil || withProvider.LocalMovies == nil || withProvider.Movies == nil {
		t.Fatalf("provider options=%+v", withProvider)
	}
}

func TestAPIStartupBuildsTheaterLocationControllerWithoutProviderClient(t *testing.T) {
	controller := newTheaterLocationController(nil, func() time.Time { return time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC) })
	if controller == nil {
		t.Fatal("theater location controller was not wired")
	}
}

func TestAPIStartupBuildsIGNRunnerWithFixedTimeoutAndNoOptionalCapabilities(t *testing.T) {
	if runtimeconfig.DefaultRequestTimeout != 20*time.Second {
		t.Fatalf("default request timeout=%s", runtimeconfig.DefaultRequestTimeout)
	}
	runner, err := newTheaterGeocodingRunner(testGeocodingStore{}, runtimeconfig.DefaultRequestTimeout, time.Now)
	if err != nil || runner == nil {
		t.Fatalf("runner=%v err=%v", runner, err)
	}
	if _, err := newTheaterGeocodingRunner(testGeocodingStore{}, time.Second, time.Now); err == nil {
		t.Fatal("invalid IGN timeout accepted")
	}
}

func TestServerWriteTimeoutRemainsBoundedForBackgroundTMDBRefresh(t *testing.T) {
	if serverWriteTimeout != 3*time.Minute {
		t.Fatalf("write timeout=%s", serverWriteTimeout)
	}
}

func TestNewAPIHandlerWiresShortlinkServiceSeparatelyFromAdmin(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL": "postgres://configured",
		"WEB_ORIGIN":   "http://localhost:3000",
	}
	cfg, _, err := loadAPIConfiguration(func(name string) string { return values[name] })
	if err != nil {
		t.Fatal(err)
	}
	handler := newAPIHandler(nil, cfg, httpapi.AdminOptions{}, testShortlinkService{}, httpapi.ReadinessOptions{})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/shortlinks", strings.NewReader(`{"target":"/"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", cfg.Server.Origin)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || response.Body.String() != `{"code":"AAAAAAAAAAAAAAAAAAAAAA","target":"/"}`+"\n" {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestShortlinkRetentionStartupUsesStrictUTC90DayCutoffAndSanitizesFailure(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.FixedZone("test", 2*60*60))
	var gotCutoff time.Time
	store := testShortlinkRetentionStore{purge: func(ctx context.Context, cutoff time.Time) error {
		gotCutoff = cutoff
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("startup purge context has no deadline")
		}
		return errors.New("secret database detail")
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	err := purgeShortlinksAtStartup(ctx, store, func() time.Time { return now })
	wantCutoff := now.UTC().Add(-shortlink.RetentionPeriod)
	if err == nil || err.Error() != "shortlink retention startup failed" || strings.Contains(err.Error(), "secret") || !gotCutoff.Equal(wantCutoff) || gotCutoff.Location() != time.UTC {
		t.Fatalf("cutoff=%s location=%s err=%v", gotCutoff, gotCutoff.Location(), err)
	}
}

func TestShortlinkRetentionPeriodicFailureDoesNotStopTicksAndCancellationReturns(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	calls := make(chan time.Time, 2)
	var mu sync.Mutex
	callCount := 0
	store := testShortlinkRetentionStore{purge: func(ctx context.Context, cutoff time.Time) error {
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 30*time.Second {
			t.Errorf("periodic purge deadline=%s present=%t", deadline, ok)
		}
		calls <- cutoff
		mu.Lock()
		defer mu.Unlock()
		callCount++
		if callCount == 1 {
			return errors.New("secret database detail")
		}
		return nil
	}}
	var logs bytes.Buffer
	logger := observability.NewLogger(&logs)
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runShortlinkRetentionTicks(ctx, store, logger, func() time.Time { return now }, ticks)
	}()

	ticks <- now
	first := <-calls
	now = now.Add(24 * time.Hour)
	ticks <- now
	second := <-calls
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retention worker did not return after cancellation")
	}
	if !first.Equal(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)) || !second.Equal(time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("cutoffs=%s,%s", first, second)
	}
	if !strings.Contains(logs.String(), `"msg":"shortlink_retention_failed"`) || !strings.Contains(logs.String(), `"error_code":"database_error"`) || strings.Contains(logs.String(), "secret") {
		t.Fatalf("logs=%q", logs.String())
	}
	if shortlink.RetentionPurgeInterval != 24*time.Hour || shortlink.RetentionPeriod != 90*24*time.Hour {
		t.Fatalf("interval=%s retention=%s", shortlink.RetentionPurgeInterval, shortlink.RetentionPeriod)
	}
}

func TestServeGracefulShutdownCancelsWorkersBeforeBoundedShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	workerCanceled := false
	server := &testHTTPServer{
		serveStarted: make(chan struct{}),
		serveRelease: make(chan error, 1),
	}
	server.shutdown = func(ctx context.Context) error {
		if !workerCanceled {
			t.Fatal("Shutdown called before worker cancellation")
		}
		if err := ctx.Err(); err != nil {
			t.Fatalf("Shutdown context already canceled: %v", err)
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("Shutdown context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > shutdownTimeout {
			t.Fatalf("Shutdown deadline remaining=%s", remaining)
		}
		server.serveRelease <- http.ErrServerClosed
		return nil
	}
	if err := serve(ctx, server, func() { workerCanceled = true }); err != nil {
		t.Fatalf("serve err=%v", err)
	}
}

func TestServeAcceptsServerClosed(t *testing.T) {
	server := &testHTTPServer{
		serveStarted: make(chan struct{}),
		serveRelease: make(chan error, 1),
		shutdown:     func(context.Context) error { return nil },
	}
	server.serveRelease <- http.ErrServerClosed
	if err := serve(context.Background(), server, func() {}); err != nil {
		t.Fatalf("serve err=%v", err)
	}
}

func TestServeSanitizesUnexpectedListenerError(t *testing.T) {
	server := &testHTTPServer{
		serveStarted: make(chan struct{}),
		serveRelease: make(chan error, 1),
		shutdown:     func(context.Context) error { return nil },
	}
	server.serveRelease <- errors.New("listen tcp: secret internal detail")
	err := serve(context.Background(), server, func() {})
	if err == nil || err.Error() != "API server failed" {
		t.Fatalf("serve err=%v", err)
	}
}

func TestShutdownWorkersStopsSchedulerAndManagerBeforeSourcePollingWait(t *testing.T) {
	var mu sync.Mutex
	events := []string{}
	record := func(event string) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}
	geocodingClosed := make(chan struct{})
	var polling sync.WaitGroup
	polling.Add(1)
	go func() {
		defer polling.Done()
		<-geocodingClosed
		record("source")
	}()

	shutdownWorkers(
		func() { record("cancel") },
		testCloseableWorker{close: func() { record("schedules") }},
		testCloseableWorker{close: func() { record("sync-manager") }},
		testCloseableWorker{close: func() { record("geocoding-manager"); close(geocodingClosed) }},
		testCloseableWorker{close: func() { record("metadata-refresh-manager") }},
		&polling,
	)
	mu.Lock()
	got := strings.Join(events, ",")
	mu.Unlock()
	if got != "cancel,schedules,sync-manager,geocoding-manager,metadata-refresh-manager,source" {
		t.Fatalf("cleanup order=%s", got)
	}
}
