package main

import (
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

func (testSnapshotWriter) Replace(context.Context, []schedule.Dataset) (schedule.PublicationResult, error) {
	return schedule.PublicationResult{}, nil
}

func (testGeocodingStore) Select(context.Context) ([]geocoding.Theater, error) {
	return nil, nil
}

func (testGeocodingStore) Save(context.Context, *geocoding.Location, geocoding.Location) (bool, error) {
	return false, nil
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
	adminOptions := newAdminOptions(cfg.Admin.Password, cfg.Admin.SessionSecret, enrichment.NewPostgresStore(nil), nil)
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

func TestNewAdminOptionsWiresLocalMoviesWithoutTMDBProvider(t *testing.T) {
	store := enrichment.NewPostgresStore(nil)
	options := newAdminOptions("password", "session-secret", store, nil)
	if options.Password != "password" || options.Reviews == nil || options.LocalMovies == nil || options.TMDBReruns != nil {
		t.Fatalf("options=%+v", options)
	}
	withProvider := newAdminOptions("password", "session-secret", store, testTMDBProvider{})
	if withProvider.TMDBReruns == nil || withProvider.Reviews == nil || withProvider.LocalMovies == nil {
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

func TestServerWriteTimeoutCoversSynchronousTMDBRerun(t *testing.T) {
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
		&polling,
	)
	mu.Lock()
	got := strings.Join(events, ",")
	mu.Unlock()
	if got != "cancel,schedules,sync-manager,geocoding-manager,source" {
		t.Fatalf("cleanup order=%s", got)
	}
}
