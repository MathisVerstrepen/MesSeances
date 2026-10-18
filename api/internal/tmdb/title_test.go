package tmdb

import (
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestClientDetailsInternationalTitlePreservesFrenchMetadata(t *testing.T) {
	requests := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/3/movie/1234139":
			if r.URL.Query().Get("language") != "fr-FR" {
				t.Errorf("language=%q", r.URL.Query().Get("language"))
			}
			if r.URL.Query().Get("include_video_language") == "ta" {
				_, _ = w.Write([]byte(`{"id":1234139,"videos":{"results":[{"key":"TAoff123456","site":"YouTube","type":"Trailer","iso_639_1":"ta","official":true}]}}`))
				return
			}
			if r.URL.Query().Get("append_to_response") != "videos,external_ids,translations" || r.URL.Query().Get("include_video_language") != "fr" {
				t.Errorf("query=%s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"id":1234139,"adult":false,"imdb_id":"tt33318732","title":"ஜெயிலர் 2","original_title":"ஜெயிலர் 2","original_language":"ta","overview":"Un ancien gardien de prison revient.","release_date":"2027-01-01","poster_path":"/poster.jpg","backdrop_path":"/backdrop.jpg","runtime":150,"genres":[{"name":"Action"},{"name":"Comédie"}],"external_ids":{"wikidata_id":"Q42"},"videos":{"results":[{"key":"FRoff123456","site":"YouTube","type":"Trailer","iso_639_1":"fr","official":true}]},"translations":{"id":1234139,"translations":[{"iso_639_1":"en","iso_3166_1":"US","data":{"title":"Jailer 2","overview":"English synopsis","runtime":99}}]}}`))
		case "/3/configuration":
			_, _ = w.Write([]byte(`{"images":{"secure_base_url":"https://image.tmdb.org/t/p/","poster_sizes":["w500"],"backdrop_sizes":["w780"]}}`))
		default:
			t.Errorf("unexpected request=%s", r.URL.RequestURI())
			w.WriteHeader(http.StatusNotFound)
		}
	}, "synthetic")
	got, err := client.Details(t.Context(), 1234139)
	adult := false
	want := Details{
		Adult: &adult, ID: 1234139, IMDBID: "tt33318732", WikidataID: "Q42",
		Title: "Jailer 2", OriginalTitle: "ஜெயிலர் 2", OriginalLanguage: "ta",
		Overview: "Un ancien gardien de prison revient.", ReleaseDate: "2027-01-01",
		PosterURL: "https://image.tmdb.org/t/p/w500/poster.jpg", BackdropURL: "https://image.tmdb.org/t/p/w780/backdrop.jpg",
		TrailerVFYouTubeKey: "FRoff123456", TrailerVOYouTubeKey: "TAoff123456", Runtime: 150, Genres: []string{"Action", "Comédie"},
	}
	if err != nil || !reflect.DeepEqual(got, want) || requests != 3 {
		t.Fatalf("details=%+v want=%+v requests=%d err=%v", got, want, requests, err)
	}
}

func TestClientDetailsTitleFallback(t *testing.T) {
	english := `{"translations":[{"iso_639_1":"en","iso_3166_1":"US","data":{"title":"International"}}]}`
	for _, tc := range []struct {
		name, title, translations, want string
	}{
		{"French title unchanged", "Le Fabuleux Destin d'Amélie Poulain", english, "Le Fabuleux Destin d'Amélie Poulain"},
		{"Latin original unchanged", "Straße Łódź Æon Đorđe", english, "Straße Łódź Æon Đorđe"},
		{"combining accent unchanged", "A\u0301melie", english, "A\u0301melie"},
		{"numeric title unchanged", "1917", english, "1917"},
		{"non Latin localized title", "別の題名", english, "International"},
		{"missing translations", "ஜெயிலர் 2", "", "ஜெயிலர் 2"},
		{"null translations", "ஜெயிலர் 2", "null", "ஜெயிலர் 2"},
		{"malformed translations", "ஜெயிலர் 2", `[]`, "ஜெயிலர் 2"},
		{"malformed title type", "ஜெயிலர் 2", `{"translations":[{"iso_639_1":"en","data":{"title":42}}]}`, "ஜெயிலர் 2"},
		{"empty translations", "ஜெயிலர் 2", `{"translations":[]}`, "ஜெயிலர் 2"},
		{"missing English", "ஜெயிலர் 2", `{"translations":[{"iso_639_1":"de","data":{"title":"International"}}]}`, "ஜெயிலர் 2"},
		{"blank title", "ஜெயிலர் 2", `{"translations":[{"iso_639_1":"en","data":{"title":"  "}}]}`, "ஜெயிலர் 2"},
		{"non Latin English title", "ஜெயிலர் 2", `{"translations":[{"iso_639_1":"en","data":{"title":"ஜெயிலர் 2"}}]}`, "ஜெயிலர் 2"},
		{"mixed script English title", "ஜெயிலர் 2", `{"translations":[{"iso_639_1":"en","data":{"title":"Jailer ஜெயிலர் 2"}}]}`, "ஜெயிலர் 2"},
		{"control in English title", "ஜெயிலர் 2", `{"translations":[{"iso_639_1":"en","data":{"title":"Jailer\n2"}}]}`, "ஜெயிலர் 2"},
		{"punctuation only", "ஜெயிலர் 2", `{"translations":[{"iso_639_1":"en","data":{"title":"..."}}]}`, "ஜெயிலர் 2"},
		{"oversized title", "ஜெயிலர் 2", `{"translations":[{"iso_639_1":"en","data":{"title":"` + strings.Repeat("a", 1025) + `"}}]}`, "ஜெயிலர் 2"},
		{"Latin English title", "ஜெயிலர் 2", `{"translations":[{"iso_639_1":"en","data":{"title":"  Élève Łódź 2  "}}]}`, "Élève Łódź 2"},
		{"numeric English title", "ஜெயிலர் 2", `{"translations":[{"iso_639_1":"en","data":{"title":"1917"}}]}`, "1917"},
		{"US preferred over other English", "ஜெயிலர் 2", `{"translations":[{"iso_639_1":"en","iso_3166_1":"GB","data":{"title":"British"}},{"iso_639_1":"en","iso_3166_1":"US","data":{"title":"International"}}]}`, "International"},
		{"other English when US unusable", "ஜெயிலர் 2", `{"translations":[{"iso_639_1":"en","iso_3166_1":"US","data":{"title":""}},{"iso_639_1":"en","iso_3166_1":"GB","data":{"title":"British"}}]}`, "British"},
		{"matching appended ID", "ஜெயிலர் 2", `{"id":42,"translations":[{"iso_639_1":"en","data":{"title":"International"}}]}`, "International"},
		{"mismatched appended ID", "ஜெயிலர் 2", `{"id":43,"translations":[{"iso_639_1":"en","data":{"title":"Wrong film"}}]}`, "ஜெயிலர் 2"},
		{"invalid appended ID", "ஜெயிலர் 2", `{"id":"42","translations":[{"iso_639_1":"en","data":{"title":"Wrong film"}}]}`, "ஜெயிலர் 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path != "/3/movie/42" {
					t.Errorf("unexpected request=%s", r.URL.RequestURI())
				}
				block := ""
				if tc.translations != "" {
					block = `,"translations":` + tc.translations
				}
				_, _ = w.Write([]byte(`{"id":42,"title":` + strconv.Quote(tc.title) + `,"original_title":` + strconv.Quote(tc.title) + `,"overview":"Résumé français","genres":[{"name":"Drame"}]` + block + `}`))
			}, "synthetic")
			got, err := client.Details(t.Context(), 42)
			if err != nil || got.Title != tc.want || got.OriginalTitle != tc.title || got.Overview != "Résumé français" || !reflect.DeepEqual(got.Genres, []string{"Drame"}) || requests != 1 {
				t.Fatalf("details=%+v requests=%d err=%v", got, requests, err)
			}
		})
	}
}

func TestClientDetailsTitleFallbackPreservesValidationAndErrors(t *testing.T) {
	body := `{"id":42,"title":"ஜெயிலர் 2","original_title":"ஜெயிலர் 2","translations":{"id":42,"translations":[{"iso_639_1":"en","data":{"title":"Jailer 2"}}]}}`
	for _, tc := range []struct {
		name   string
		id     int64
		status int
		body   string
		stop   bool
	}{
		{"invalid request ID", 0, http.StatusOK, body, false},
		{"top level ID mismatch", 43, http.StatusOK, body, false},
		{"invalid base title", 42, http.StatusOK, strings.Replace(body, `"title":"ஜெயிலர் 2"`, `"title":""`, 1), false},
		{"invalid runtime", 42, http.StatusOK, strings.Replace(body, `"id":42`, `"id":42,"runtime":-1`, 1), false},
		{"malformed JSON", 42, http.StatusOK, body[:len(body)-1], false},
		{"rate limited", 42, http.StatusTooManyRequests, "synthetic-secret", true},
		{"unauthorized", 42, http.StatusUnauthorized, "synthetic-secret", true},
		{"server error", 42, http.StatusInternalServerError, "synthetic-secret", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				requests++
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}, "synthetic-secret")
			got, err := client.Details(t.Context(), tc.id)
			wantRequests := 1
			if tc.id <= 0 {
				wantRequests = 0
			}
			if err == nil || !reflect.DeepEqual(got, Details{}) || errors.Is(err, ErrStop) != tc.stop || strings.Contains(err.Error(), "synthetic-secret") || requests != wantRequests {
				t.Fatalf("details=%+v requests=%d err=%v", got, requests, err)
			}
		})
	}
}
