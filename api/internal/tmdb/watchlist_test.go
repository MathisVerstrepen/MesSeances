package tmdb

import (
	"fmt"
	"net/http"
	"testing"
)

func TestDetailsPreservesExplicitAdultClassification(t *testing.T) {
	for _, tc := range []struct {
		field                 string
		known, adult, invalid bool
	}{
		{field: ""}, {field: `,"adult":null`}, {field: `,"adult":false`, known: true}, {field: `,"adult":true`, known: true, adult: true}, {field: `,"adult":"false"`, invalid: true},
	} {
		t.Run(tc.field, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprintf(w, `{"id":42,"title":"Film","original_title":"Film","runtime":0,"genres":[]%s}`, tc.field)
			}, "test-token")
			details, err := client.Details(t.Context(), 42)
			if (err != nil) != tc.invalid {
				t.Fatalf("error=%v", err)
			}
			if !tc.invalid && ((details.Adult != nil) != tc.known || details.Adult != nil && *details.Adult != tc.adult) {
				t.Fatal("missing classification defaulted to non-adult")
			}
		})
	}
}

func TestSearchIncludesValidatedReleaseDates(t *testing.T) {
	for _, date := range []string{"", "2001-04-25", "2026-02-30"} {
		t.Run(date, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("include_adult") != "false" || r.URL.Query().Get("language") != "fr-FR" {
					t.Error("unsafe search policy")
				}
				_, _ = fmt.Fprintf(w, `{"results":[{"id":42,"title":"Film","original_title":"Film","release_date":%q}]}`, date)
			}, "test-token")
			movies, err := client.Search(t.Context(), "Film")
			if date == "2026-02-30" {
				if err == nil {
					t.Fatal("invalid release date accepted")
				}
				return
			}
			if err != nil || len(movies) != 1 || movies[0].ReleaseDate != date {
				t.Fatalf("movies=%+v err=%v", movies, err)
			}
		})
	}
}
