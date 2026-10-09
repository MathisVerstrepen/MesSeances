package moviesearch

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSharedMovieTitleSearch(t *testing.T) {
	data, err := os.ReadFile("../../../web/tests/fixtures/movie-title-search.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name          string  `json:"name"`
		Query         string  `json:"query"`
		Title         string  `json:"title"`
		OriginalTitle *string `json:"original_title"`
		Expected      bool    `json:"expected"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty corpus")
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			original := ""
			if tc.OriginalTitle != nil {
				original = *tc.OriginalTitle
			}
			query := Compile(tc.Query)
			if got := query.Matches(tc.Title, original); got != tc.Expected {
				t.Fatalf("Matches(%q, %q)=%t want=%t", tc.Title, original, got, tc.Expected)
			}
			if query.Blank() != (strings.TrimSpace(tc.Query) == "") {
				t.Fatal("blank raw query state lost")
			}
		})
	}
}

func TestQueryZeroValue(t *testing.T) {
	var query Query
	if !query.Blank() || !query.Matches("", "") {
		t.Fatal("zero query should apply no filter")
	}
}
