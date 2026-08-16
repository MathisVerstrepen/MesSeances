package tmdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc, token string) *Client {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClientWithConfig(token, Config{HTTPClient: server.Client(), BaseURL: server.URL, RequestInterval: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestClientSearchAndDetails(t *testing.T) {
	requests := []string{}
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		requests = append(requests, r.URL.RequestURI())
		switch r.URL.Path {
		case "/3/search/movie":
			if r.URL.Query().Get("language") != "fr-FR" || r.URL.Query().Get("region") != "FR" || r.URL.Query().Get("include_adult") != "false" {
				t.Error("search query mismatch")
			}
			_, _ = w.Write([]byte(`{"results":[{"id":42,"title":"Amélie","original_title":"Le Fabuleux Destin d'Amélie Poulain","poster_path":"/poster.jpg"}]}`))
		case "/3/movie/42":
			_, _ = w.Write([]byte(`{"id":42,"title":"Amélie","original_title":"Le Fabuleux Destin d'Amélie Poulain","overview":"Résumé","release_date":"2001-04-25","poster_path":"/poster.jpg","backdrop_path":"/backdrop.jpg","runtime":122,"genres":[{"name":"Comédie"}]}`))
		case "/3/configuration":
			_, _ = w.Write([]byte(`{"images":{"secure_base_url":"https://image.tmdb.org/t/p/","poster_sizes":["w342","w500"],"backdrop_sizes":["w300","w780"]}}`))
		default:
			t.Errorf("path=%s", r.URL.Path)
		}
	}, "secret")
	candidates, err := client.Search(context.Background(), "Amélie")
	if err != nil || len(candidates) != 1 || candidates[0].ID != 42 || candidates[0].PosterURL != "https://image.tmdb.org/t/p/w500/poster.jpg" {
		t.Fatalf("candidates=%+v err=%v", candidates, err)
	}
	details, err := client.Details(context.Background(), 42)
	if err != nil || details.PosterURL != "https://image.tmdb.org/t/p/w500/poster.jpg" || details.BackdropURL != "https://image.tmdb.org/t/p/w780/backdrop.jpg" || details.Runtime != 122 || len(details.Genres) != 1 {
		t.Fatalf("details=%+v err=%v", details, err)
	}
	if len(requests) != 3 {
		t.Fatalf("requests=%v", requests)
	}
}

func TestClientSearchRejectsMalformedPosterPath(t *testing.T) {
	requests := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"results":[{"id":42,"title":"Film","original_title":"Film","poster_path":"//evil.example/poster.jpg"}]}`))
	}, "token")
	if _, err := client.Search(context.Background(), "Film"); err == nil || requests != 1 {
		t.Fatalf("requests=%d err=%v", requests, err)
	}
}

func TestClientStopsAndRedacts(t *testing.T) {
	secret := "synthetic-token"
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, secret, http.StatusTooManyRequests) }, secret)
	_, err := client.Search(context.Background(), "Film")
	if !errors.Is(err, ErrStop) || strings.Contains(err.Error(), secret) {
		t.Fatalf("error=%v", err)
	}
}

func TestClientRejectsOversizedAndBadConfiguration(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
	}, "token")
	if _, err := client.Search(context.Background(), "Film"); err == nil {
		t.Fatal("oversized response accepted")
	}
	client = testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/3/movie/1" {
			_, _ = w.Write([]byte(`{"id":1,"title":"Film","original_title":"Film","poster_path":"/p.jpg","runtime":90,"genres":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"images":{"secure_base_url":"http://images.invalid/","poster_sizes":["w500"],"backdrop_sizes":["w780"]}}`))
	}, "token")
	if _, err := client.Details(context.Background(), 1); err == nil {
		t.Fatal("insecure image configuration accepted")
	}
}

func TestClientRejectsTMDBImageConfigurationOutsideCanonicalPath(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/3/search/movie" {
			_, _ = w.Write([]byte(`{"results":[{"id":1,"title":"Film","original_title":"Film","poster_path":"/p.jpg"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"images":{"secure_base_url":"https://image.tmdb.org/other/","poster_sizes":["w500"],"backdrop_sizes":["w780"]}}`))
	}, "token")
	if _, err := client.Search(context.Background(), "Film"); err == nil {
		t.Fatal("non-canonical image configuration accepted")
	}
}

func TestClientBackdropOptionalAndValidation(t *testing.T) {
	t.Run("empty path needs no configuration", func(t *testing.T) {
		requests := 0
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			requests++
			_, _ = w.Write([]byte(`{"id":1,"title":"Film","original_title":"Film","runtime":90,"genres":[]}`))
		}, "token")
		details, err := client.Details(context.Background(), 1)
		if err != nil || details.BackdropURL != "" || requests != 1 {
			t.Fatalf("details=%+v requests=%d err=%v", details, requests, err)
		}
	})

	for _, path := range []string{"//evil.example/a.jpg", "/../a.jpg", "/%2e%2e/a.jpg", "/a.jpg?token=x", "/a\\b.jpg"} {
		t.Run(path, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"id":1,"title":"Film","original_title":"Film","backdrop_path":` + strconv.Quote(path) + `,"runtime":90,"genres":[]}`))
			}, "token")
			if _, err := client.Details(context.Background(), 1); err == nil {
				t.Fatalf("malformed backdrop path %q accepted", path)
			}
		})
	}
}

func TestClientRequiresW780BackdropConfiguration(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/3/movie/1" {
			_, _ = w.Write([]byte(`{"id":1,"title":"Film","original_title":"Film","backdrop_path":"/a.jpg","runtime":90,"genres":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"images":{"secure_base_url":"https://image.tmdb.org/t/p/","poster_sizes":["w500"],"backdrop_sizes":["w300","original"]}}`))
	}, "token")
	if _, err := client.Details(context.Background(), 1); err == nil {
		t.Fatal("configuration without w780 accepted")
	}
}
