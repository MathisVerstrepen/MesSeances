package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"messeances/api/internal/accountavatar"
	runtimeconfig "messeances/api/internal/config"
)

type avatarCleanupProbe struct{ started chan struct{} }

func (p avatarCleanupProbe) CleanupAvatars(ctx context.Context) (accountavatar.SweepResult, error) {
	close(p.started)
	<-ctx.Done()
	return accountavatar.SweepResult{}, ctx.Err()
}
func TestAvatarCleanupStartupAndDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	probe := avatarCleanupProbe{make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runAccountAvatarCleanup(ctx, probe, slog.New(slog.DiscardHandler))
	}()
	<-probe.started
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("GC not joined")
	}
	entered, resume := make(chan struct{}), make(chan struct{})
	drain := &accountRequestDrain{next: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(entered); <-resume })}
	go drain.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), "GET", "/", nil))
	<-entered
	drain.stop()
	joined := make(chan struct{})
	go func() { drain.wait(); close(joined) }()
	select {
	case <-joined:
		t.Fatal("active request not drained")
	default:
	}
	w := httptest.NewRecorder()
	drain.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), "GET", "/", nil))
	if w.Code != 503 {
		t.Fatal("new work admitted during drain")
	}
	close(resume)
	<-joined
}
func TestAvatarRuntimeDisabledAndSingleOwner(t *testing.T) {
	cfg := runtimeconfig.Config{}
	cfg.Accounts.AvatarDir = "/not/a/real/media/root"
	if s, err := newAccountService(nil, cfg, nil); err != nil || s != nil {
		t.Fatal("disabled media touched")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	cfg.Server.Origin = "http://localhost:3000"
	cfg.Accounts.Enabled = true
	cfg.Accounts.AvatarDir = root
	cfg.Accounts.GoogleClientID = "synthetic.apps.googleusercontent.com"
	cfg.Accounts.GoogleClientSecret = "synthetic"
	cfg.Accounts.GoogleCallbackURL = "http://localhost:3000/api/v1/auth/google/callback"
	cfg.Accounts.OutboxKeyID = "synthetic"
	for i := range cfg.Accounts.OutboxKey {
		cfg.Accounts.OutboxKey[i] = 1
		cfg.Accounts.AddressHMACKey[i] = 2
	}
	media, err := accountavatar.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = newAccountService(nil, cfg, media); err != nil {
		t.Fatal(err)
	}
	if _, err = accountavatar.Open(root); err == nil {
		t.Fatal("second writer acquired root")
	}
	if err = media.Close(); err != nil {
		t.Fatal(err)
	}
	two, err := accountavatar.Open(root)
	if err != nil {
		t.Fatal("lock leaked")
	}
	if err = two.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = newAccountService(nil, cfg, nil); err == nil || err.Error() != "configuration error" {
		t.Fatal("enabled service accepted missing root ownership")
	}
}
