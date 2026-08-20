package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
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

func TestNewAdminOptionsWiresLocalMoviesWithoutTMDBProvider(t *testing.T) {
	store := enrichment.NewPostgresStore(nil)
	options := newAdminOptions("password", store, nil)
	if options.Password != "password" || options.Reviews == nil || options.LocalMovies == nil {
		t.Fatalf("options=%+v", options)
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
