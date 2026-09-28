package accounts

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestWatchlistTagNormalization(t *testing.T) {
	for _, tc := range []struct{ input, name, key string }{
		{"  Action\u00a0  aventure\u2003", "Action aventure", "action aventure"},
		{"E\u0301te\u0301", "Été", "été"},
		{"Straße", "Straße", "strasse"},
		{"<b>Film</b>", "<b>Film</b>", "<b>film</b>"},
		{strings.Repeat("É", 40), strings.Repeat("É", 40), strings.Repeat("é", 40)},
	} {
		name, key, err := normalizeWatchlistTag(tc.input)
		if err != nil || name != tc.name || key != tc.key {
			t.Fatalf("normalization %q: %q %q %v", tc.input, name, key, err)
		}
	}
	for _, name := range []string{"", "  \u00a0", "\xff", "a\x00b", "a\tb", "a\nb", "a\rb", "a\u0085b", "a\u2028b", "a\u2029b", strings.Repeat("é", 41)} {
		if _, _, err := normalizeWatchlistTag(name); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("accepted %q", name)
		}
	}
}

func TestWatchlistTagDTOBoundaries(t *testing.T) {
	for _, dto := range []any{WatchlistMovie{Slug: "film-1", Title: "Film"}, WatchlistExternalMovie{TMDBID: "42", Title: "Film"}} {
		encoded, err := json.Marshal(dto)
		if err != nil || strings.Contains(string(encoded), "tag") {
			t.Fatal("search DTO acquired private tags")
		}
	}
	view := WatchlistView{Username: "owner", Revision: "1", SortOrder: "added_desc", Tags: []WatchlistTag{{ID: "9223372036854775807", Name: "Action"}}, Items: []WatchlistItem{{WatchlistMovie: WatchlistMovie{Slug: "film-1", Title: "Film"}, TagIDs: []string{}}}}
	for _, dto := range []any{view, WatchlistImportView{Watchlist: view, MovieSlug: "film-1"}} {
		encoded, err := json.Marshal(dto)
		if err != nil || !strings.Contains(string(encoded), `"tag_ids":[]`) || !strings.Contains(string(encoded), `"tags":[{"id":"9223372036854775807","name":"Action"}]`) {
			t.Fatalf("invalid snapshot wire: %s %v", encoded, err)
		}
	}
}
