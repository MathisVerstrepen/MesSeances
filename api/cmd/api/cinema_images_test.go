package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"messeances/api/internal/cinemaimage"
	runtimeconfig "messeances/api/internal/config"
	"messeances/api/internal/httpapi"
)

func privateCinemaDir(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	if os.Chmod(path, 0700) != nil {
		t.Fatal("chmod")
	}
	return path
}
func TestCinemaImageRuntimeIndependentOfAccounts(t *testing.T) {
	if s, e := openCinemaImages(runtimeconfig.Config{}); e != nil || s != nil {
		t.Fatal(e)
	}
	path := privateCinemaDir(t)
	cfg := runtimeconfig.Config{CinemaImageDir: path}
	s, e := openCinemaImages(cfg)
	if e != nil || s == nil {
		t.Fatal(e)
	}
	if other, e := openCinemaImages(cfg); e == nil {
		_ = other.Close()
		t.Fatal("second owner")
	}
	if s.Close() != nil {
		t.Fatal("close")
	}
	s, e = openCinemaImages(cfg)
	if e != nil {
		t.Fatal("ownership not released", e)
	}
	if s.Close() != nil {
		t.Fatal("close")
	}
	service := newCinemaImageService(nil, nil, nil, slog.New(slog.DiscardHandler))
	if _, e := service.List(t.Context(), cinemaimage.ListQuery{Limit: 20}); !errors.Is(e, cinemaimage.ErrStorage) {
		t.Fatal("disabled media", e)
	}
}
func TestCinemaImageRuntimeRejectsAliasedOverlapAndBadRoot(t *testing.T) {
	root := privateCinemaDir(t)
	child := filepath.Join(root, "cinema")
	if os.Mkdir(child, 0700) != nil {
		t.Fatal("mkdir")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if os.Symlink(root, alias) != nil {
		t.Fatal("symlink")
	}
	for _, cfg := range []runtimeconfig.Config{
		{CinemaImageDir: root, Accounts: runtimeconfig.AccountsConfig{Enabled: true, AvatarDir: child}},
		{CinemaImageDir: child, Accounts: runtimeconfig.AccountsConfig{Enabled: true, AvatarDir: alias}},
		{CinemaImageDir: alias}, {CinemaImageDir: filepath.Join(root, "missing")},
	} {
		s, e := openCinemaImages(cfg)
		if e == nil || s != nil || e.Error() != "configuration error" {
			t.Fatal("bad media configuration accepted", e)
		}
	}
}
func TestCinemaImageBadRootFailsBeforeDatabaseStartup(t *testing.T) {
	t.Setenv("DATABASE_URL", "://malformed")
	t.Setenv("CINEMA_IMAGE_DIR", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("ACCOUNTS_ENABLED", "false")
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("ADMIN_SESSION_SECRET", "")
	t.Setenv("INTERNAL_API_SHARED_SECRET", "")
	t.Setenv("WEB_ORIGIN", "http://localhost:3000")
	if e := run(context.Background()); e == nil || e.Error() != "configuration error" {
		t.Fatalf("startup order error %v", e)
	}
}

type cinemaCleanerFake struct{ started, done chan struct{} }

func (f cinemaCleanerFake) Cleanup(ctx context.Context) (cinemaimage.SweepResult, error) {
	close(f.started)
	<-ctx.Done()
	close(f.done)
	return cinemaimage.SweepResult{}, ctx.Err()
}
func TestCinemaImageCleanupAndHTTPDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := cinemaCleanerFake{make(chan struct{}), make(chan struct{})}
	workerDone := make(chan struct{})
	go func() {
		runCinemaImageCleanup(ctx, f, slog.New(slog.DiscardHandler))
		close(workerDone)
	}()
	<-f.started
	cancel()
	select {
	case <-workerDone:
	case <-time.After(time.Second):
		t.Fatal("collector did not drain")
	}
	<-f.done
	started, release, requestDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	drain := &accountRequestDrain{next: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(started); <-release })}
	go func() {
		drain.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/theaters", nil))
		close(requestDone)
	}()
	<-started
	drain.stop()
	response := httptest.NewRecorder()
	drain.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if response.Code != 503 {
		t.Fatal(response.Code)
	}
	close(release)
	drain.wait()
	<-requestDone
}

type runtimePublicCinemaImages struct{ calls int }

func (*runtimePublicCinemaImages) PublicImage(context.Context, cinemaimage.Identity) (*cinemaimage.PublicImage, error) {
	return nil, nil
}

func (f *runtimePublicCinemaImages) Read(_ context.Context, id cinemaimage.Identity, revision int64) ([]byte, error) {
	f.calls++
	if id != (cinemaimage.Identity{Provider: "ugc", ProviderTheaterID: "25"}) || revision != 1 {
		return nil, cinemaimage.ErrImageNotFound
	}
	return []byte("normalized fixture"), nil
}

func TestCinemaImagePublicRuntimeIndependentOfAdminAndAccounts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		accounts bool
		admin    httpapi.AdminOptions
		public   bool
	}{
		{name: "anonymous without admin or accounts", public: true},
		{name: "unavailable accounts unrelated", accounts: true, public: true},
		{name: "configured admin unrelated", admin: httpapi.AdminOptions{Password: "fixture-password", SessionSecret: "fixture-secret"}, public: true},
		{name: "absent public controller"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := runtimeconfig.Config{}
			cfg.Server.Origin = "http://localhost:3000"
			cfg.Accounts.Enabled = tc.accounts
			f := &runtimePublicCinemaImages{}
			var public httpapi.PublicTheaterImageController
			if tc.public {
				public = f
			}
			handler := newAPIHandler(nil, cfg, tc.admin, nil, nil, nil, nil, httpapi.ReadinessOptions{}, nil, public, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/theaters/ugc/25/image/1", nil))
			wantStatus, wantCalls := 404, 0
			if tc.public {
				wantStatus, wantCalls = 200, 1
			}
			if response.Code != wantStatus || f.calls != wantCalls || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
				t.Fatal(response.Code, response.Header(), f.calls)
			}
			admin := httptest.NewRecorder()
			handler.ServeHTTP(admin, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/theaters/ugc/25/image/1", nil))
			if admin.Code == 200 || f.calls != wantCalls {
				t.Fatal("public reader weakened admin authorization", admin.Code, f.calls)
			}
		})
	}
}
