package cinemovida

import (
	"fmt"
	"testing"
)

func TestMovieRuntimeSeconds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		field   string
		minutes int
		invalid bool
	}{
		{name: "missing"},
		{name: "null", field: `,"runtime":null`},
		{name: "zero", field: `,"runtime":0`},
		{name: "one minute", field: `,"runtime":60`, minutes: 1},
		{name: "seconds", field: `,"runtime":6059`, minutes: 100},
		{name: "negative", field: `,"runtime":-1`, invalid: true},
		{name: "subminute", field: `,"runtime":59`, invalid: true},
		{name: "duration overflow", field: `,"runtime":9223372036854775807`, invalid: true},
		{name: "wrong type", field: `,"runtime":"0"`, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`[{"id":"c2581","title":"Croire aux arbres"%s}]`, tc.field)
			movies, err := parseMovies([]byte(body))
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid runtime accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(movies) != 1 || movies["c2581"].RuntimeMinutes != tc.minutes || movies["c2581"].Title != "Croire aux arbres" {
				t.Fatal("movie or runtime lost")
			}
		})
	}
}
