package main

import (
	"errors"
	"io"
	"strings"
	"testing"

	"movieflow/api/internal/enrichment"
)

type testReadCloser struct {
	io.Reader
	closeErr error
}

func (r testReadCloser) Close() error { return r.closeErr }

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
