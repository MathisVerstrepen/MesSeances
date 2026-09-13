package tmdb

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const posterTestConfiguration = `{"images":{"secure_base_url":"https://image.tmdb.org/t/p/","poster_sizes":["w500"],"backdrop_sizes":["w780"]}}`

func TestClientPostersAllLanguagesAndDeduplication(t *testing.T) {
	configurationCalls := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.URL.RawQuery != "" {
			t.Error("expected bearer authentication and no query parameters")
		}
		switch r.URL.Path {
		case "/3/movie/42/images":
			_, _ = w.Write([]byte(`{"id":42,"posters":[
				{"file_path":"/fr.jpg","width":1000,"height":1500,"iso_639_1":"fr"},
				{"file_path":"/ja.jpg","width":2000,"height":3000,"iso_639_1":"ja"},
				{"file_path":"/neutral.jpg","width":500,"height":750,"iso_639_1":null},
				{"file_path":"/fr.jpg","width":1000,"height":1500,"iso_639_1":"fr"},
				{"file_path":"/empty-language.jpg","width":500,"height":750,"iso_639_1":""}
			],"backdrops":[{"file_path":"/backdrop.jpg"}]}`))
		case "/3/configuration":
			configurationCalls++
			_, _ = w.Write([]byte(posterTestConfiguration))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}, "secret")
	posters, err := client.Posters(context.Background(), 42)
	if err != nil || len(posters) != 4 || configurationCalls != 1 {
		t.Fatalf("posters=%+v configurationCalls=%d err=%v", posters, configurationCalls, err)
	}
	if posters[0].URL != "https://image.tmdb.org/t/p/w500/fr.jpg" || posters[0].Width != 1000 || posters[0].Height != 1500 || *posters[0].Language != "fr" || *posters[1].Language != "ja" || posters[2].Language != nil || posters[3].Language != nil {
		t.Fatalf("posters=%+v", posters)
	}
}

func TestClientPostersSkipsUnsafeAndMalformedEntries(t *testing.T) {
	paths := []string{"", "/", "/.", "https://evil.test/p.jpg", "//evil.test/p.jpg", "/../p.jpg", "/%2e%2e/p.jpg", "/p%2f.jpg", "/p.jpg?x=1", "/p.jpg#x", "/p\\x.jpg", "/p\nx.jpg", "/p x.jpg", "/nested/p.jpg", "/p\x00.jpg", "/" + strings.Repeat("a", 1024)}
	images := make([]map[string]any, 0, len(paths)+4)
	for _, path := range paths {
		images = append(images, map[string]any{"file_path": path, "width": 500, "height": 750})
	}
	images = append(images,
		map[string]any{"file_path": "/width.jpg", "width": 0, "height": 750},
		map[string]any{"file_path": "/height.jpg", "width": 500, "height": -1},
		map[string]any{"file_path": "/language.jpg", "width": 500, "height": 750, "iso_639_1": "secret-invalid"},
		map[string]any{"file_path": "/valid_1-2.jpg", "width": 500, "height": 750},
	)
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/3/configuration" {
			_, _ = w.Write([]byte(posterTestConfiguration))
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"id": 42, "posters": images}); err != nil {
			t.Error(err)
		}
	}, "secret")
	posters, err := client.Posters(context.Background(), 42)
	if err != nil || len(posters) != 1 || posters[0].URL != "https://image.tmdb.org/t/p/w500/valid_1-2.jpg" {
		t.Fatalf("posters=%+v err=%v", posters, err)
	}
}

func TestClientPostersEmpty(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/3/movie/42/images" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":42,"posters":[]}`))
	}, "secret")
	posters, err := client.Posters(context.Background(), 42)
	if err != nil || posters == nil || len(posters) != 0 {
		t.Fatalf("posters=%+v err=%v", posters, err)
	}
}

func TestClientPostersErrorsAreSafe(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, "secret upstream body"},
		{"forbidden", 403, "secret upstream body"},
		{"rate limited", 429, "secret upstream body"},
		{"unavailable", 503, "secret upstream body"},
		{"missing upstream movie", 404, "secret upstream body"},
		{"malformed JSON", 200, "secret upstream body"},
		{"oversized response", 200, strings.Repeat("x", maxResponseBytes+1)},
		{"wrong identity", 200, `{"id":43,"posters":[]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}, "secret-token")
			if _, err := client.Posters(context.Background(), 42); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe or missing error: %v", err)
			}
		})
	}
}

func TestClientPostersRejectsUnsafeConfiguration(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/3/configuration" {
			_, _ = w.Write([]byte(strings.Replace(posterTestConfiguration, "image.tmdb.org", "evil.test", 1)))
			return
		}
		_, _ = w.Write([]byte(`{"id":42,"posters":[{"file_path":"/poster.jpg","width":500,"height":750}]}`))
	}, "secret")
	if _, err := client.Posters(context.Background(), 42); err == nil {
		t.Fatal("expected unsafe configuration rejection")
	}
}

func TestClientPostersInvalidIDAndCancellation(t *testing.T) {
	client := testClient(t, func(http.ResponseWriter, *http.Request) {
		t.Error("unexpected upstream request")
	}, "secret")
	for _, id := range []int64{0, -1} {
		if _, err := client.Posters(context.Background(), id); err == nil {
			t.Fatal("expected invalid ID error")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Posters(ctx, 42); err == nil {
		t.Fatal("expected cancellation error")
	}
}
