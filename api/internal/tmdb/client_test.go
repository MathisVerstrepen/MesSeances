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
			if r.URL.Query().Get("language") != "fr-FR" || r.URL.Query().Get("append_to_response") != "videos" {
				t.Error("details query mismatch")
			}
			switch r.URL.Query().Get("include_video_language") {
			case "fr":
				_, _ = w.Write([]byte(`{"id":42,"title":"Amélie","original_title":"Le Fabuleux Destin d'Amélie Poulain","original_language":"en","overview":"Résumé","release_date":"2001-04-25","poster_path":"/poster.jpg","backdrop_path":"/backdrop.jpg","runtime":122,"genres":[{"name":"Comédie"}],"videos":{"results":[{"key":"FRuno123456","site":"YouTube","type":"Trailer","iso_639_1":"fr","official":false}]}}`))
			case "en":
				_, _ = w.Write([]byte(`{"id":42,"videos":{"results":[{"key":"ENoff123456","site":"YouTube","type":"Trailer","iso_639_1":"en","official":true}]}}`))
			default:
				t.Errorf("video language query mismatch: %s", r.URL.RequestURI())
			}
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
	if err != nil || details.PosterURL != "https://image.tmdb.org/t/p/w500/poster.jpg" || details.BackdropURL != "https://image.tmdb.org/t/p/w780/backdrop.jpg" || details.OriginalLanguage != "en" || details.TrailerVFYouTubeKey != "FRuno123456" || details.TrailerVOYouTubeKey != "ENoff123456" || details.Runtime != 122 || len(details.Genres) != 1 {
		t.Fatalf("details=%+v err=%v", details, err)
	}
	if len(requests) != 4 {
		t.Fatalf("requests=%v", requests)
	}
}

func TestSelectTrailerYouTubeKeyUsesOfficialThenStableFallback(t *testing.T) {
	tests := []struct {
		name     string
		language string
		videos   []video
		want     string
	}{
		{
			name:     "official wins requested language",
			language: "ja",
			videos: []video{
				{Key: "JAuno123456", Site: "YouTube", Type: "Trailer", Language: "ja"},
				{Key: "JAoff123456", Site: "YouTube", Type: "Trailer", Language: "ja", Official: true},
			},
			want: "JAoff123456",
		},
		{
			name:     "unofficial fallback stays in requested language",
			language: "fr",
			videos: []video{
				{Key: "FRuno123456", Site: "YouTube", Type: "Trailer", Language: "fr"},
				{Key: "ENoff123456", Site: "YouTube", Type: "Trailer", Language: "en", Official: true},
			},
			want: "FRuno123456",
		},
		{
			name:     "first result wins within tier",
			language: "fr",
			videos: []video{
				{Key: "FRoff123456", Site: "YouTube", Type: "Trailer", Language: "fr", Official: true},
				{Key: "FRoff654321", Site: "YouTube", Type: "Trailer", Language: "fr", Official: true},
			},
			want: "FRoff123456",
		},
		{
			name:     "invalid and non trailers are ignored",
			language: "fr",
			videos: []video{
				{Key: "bad", Site: "YouTube", Type: "Trailer", Language: "fr", Official: true},
				{Key: "FRoff12345!", Site: "YouTube", Type: "Trailer", Language: "fr", Official: true},
				{Key: "FRoff123456", Site: "Vimeo", Type: "Trailer", Language: "fr", Official: true},
				{Key: "ENoff123456", Site: "YouTube", Type: "Teaser", Language: "en", Official: true},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := selectTrailerYouTubeKey(test.videos, test.language); got != test.want {
				t.Fatalf("selected=%q want=%q", got, test.want)
			}
		})
	}
}

func TestClientDetailsSelectsNonEnglishOriginalAndDeduplicatesVariants(t *testing.T) {
	tests := []struct {
		name             string
		originalLanguage string
		frenchVideos     string
		originalVideos   string
		wantVF           string
		wantVO           string
		wantVideoRequest bool
	}{
		{name: "Japanese original", originalLanguage: "ja", frenchVideos: `[{"key":"FRuno123456","site":"YouTube","type":"Trailer","iso_639_1":"fr"}]`, originalVideos: `[{"key":"JAuno123456","site":"YouTube","type":"Trailer","iso_639_1":"ja"},{"key":"JAoff123456","site":"YouTube","type":"Trailer","iso_639_1":"ja","official":true}]`, wantVF: "FRuno123456", wantVO: "JAoff123456", wantVideoRequest: true},
		{name: "missing French", originalLanguage: "de", frenchVideos: `[]`, originalVideos: `[{"key":"DEuno123456","site":"YouTube","type":"Trailer","iso_639_1":"de"}]`, wantVO: "DEuno123456", wantVideoRequest: true},
		{name: "missing original", originalLanguage: "it", frenchVideos: `[{"key":"FRoff123456","site":"YouTube","type":"Trailer","iso_639_1":"fr","official":true}]`, originalVideos: `[]`, wantVF: "FRoff123456", wantVideoRequest: true},
		{name: "French original omits VO", originalLanguage: "fr", frenchVideos: `[{"key":"FRoff123456","site":"YouTube","type":"Trailer","iso_639_1":"fr","official":true}]`, wantVF: "FRoff123456"},
		{name: "identical keys omit VO", originalLanguage: "es", frenchVideos: `[{"key":"same1234567","site":"YouTube","type":"Trailer","iso_639_1":"fr","official":true}]`, originalVideos: `[{"key":"same1234567","site":"YouTube","type":"Trailer","iso_639_1":"es","official":true}]`, wantVF: "same1234567", wantVideoRequest: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			videoRequests := 0
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/3/movie/7":
					switch r.URL.Query().Get("include_video_language") {
					case "fr":
						_, _ = w.Write([]byte(`{"id":7,"title":"Film","original_title":"Original","original_language":"` + test.originalLanguage + `","runtime":90,"genres":[],"videos":{"results":` + test.frenchVideos + `}}`))
					case test.originalLanguage:
						videoRequests++
						_, _ = w.Write([]byte(`{"id":7,"videos":{"results":` + test.originalVideos + `}}`))
					default:
						t.Fatalf("unexpected language request=%s", r.URL.RequestURI())
					}
				default:
					t.Fatalf("unexpected request=%s", r.URL.RequestURI())
				}
			}, "token")
			details, err := client.Details(context.Background(), 7)
			if err != nil || details.TrailerVFYouTubeKey != test.wantVF || details.TrailerVOYouTubeKey != test.wantVO || (videoRequests == 1) != test.wantVideoRequest {
				t.Fatalf("details=%+v video requests=%d err=%v", details, videoRequests, err)
			}
		})
	}
}

func TestClientSearchReturnsFirstTwentyPageOneResults(t *testing.T) {
	requests := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/3/search/movie" || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("query") != "Film" || r.URL.Query().Get("language") != "fr-FR" || r.URL.Query().Get("region") != "FR" || r.URL.Query().Get("include_adult") != "false" {
			t.Fatalf("request=%s", r.URL.RequestURI())
		}
		var body strings.Builder
		body.WriteString(`{"results":[`)
		for id := 1; id <= 25; id++ {
			if id > 1 {
				body.WriteByte(',')
			}
			body.WriteString(`{"id":` + strconv.Itoa(id) + `,"title":"Film ` + strconv.Itoa(id) + `","original_title":"Film ` + strconv.Itoa(id) + `"}`)
		}
		body.WriteString(`]}`)
		_, _ = w.Write([]byte(body.String()))
	}, "token")
	candidates, err := client.Search(context.Background(), "Film")
	if err != nil || requests != 1 || len(candidates) != 20 || candidates[0].ID != 1 || candidates[19].ID != 20 {
		t.Fatalf("candidates=%+v requests=%d err=%v", candidates, requests, err)
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

func TestClientDetailsAllowsMissingRuntimeAndRejectsInvalidRuntime(t *testing.T) {
	tests := []struct {
		name        string
		runtime     int
		wantRuntime int
		wantErr     bool
	}{
		{name: "missing", runtime: 0, wantRuntime: 0},
		{name: "negative", runtime: -1, wantErr: true},
		{name: "too large", runtime: 601, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"id":1,"title":"Film","original_title":"Film","runtime":` + strconv.Itoa(test.runtime) + `,"genres":[]}`))
			}, "token")
			details, err := client.Details(context.Background(), 1)
			if (err != nil) != test.wantErr || !test.wantErr && details.Runtime != test.wantRuntime {
				t.Fatalf("details=%+v err=%v", details, err)
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
