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
		if err != nil || strings.Contains(string(encoded), "tag") || strings.Contains(string(encoded), "color") {
			t.Fatal("search DTO acquired private tags")
		}
	}
	view := WatchlistView{Username: "owner", Revision: "1", SortOrder: "added_desc", Tags: []WatchlistTag{{ID: "9223372036854775807", Name: "Action", Color: "blue"}}, Items: []WatchlistItem{{WatchlistMovie: WatchlistMovie{Slug: "film-1", Title: "Film"}, TagIDs: []string{}}}}
	for _, dto := range []any{view, WatchlistImportView{Watchlist: view, MovieSlug: "film-1"}} {
		encoded, err := json.Marshal(dto)
		if err != nil || !strings.Contains(string(encoded), `"tag_ids":[]`) || !strings.Contains(string(encoded), `"tags":[{"id":"9223372036854775807","name":"Action","color":"blue"}]`) {
			t.Fatalf("invalid snapshot wire: %s %v", encoded, err)
		}
	}
}

func TestWatchlistTagColorValidation(t *testing.T) {
	for _, color := range []string{"neutral", "red", "amber", "green", "teal", "blue", "violet", "rose"} {
		if !validWatchlistTagColor(color) {
			t.Fatalf("rejected palette value %q", color)
		}
	}
	// A nil store proves invalid colors never enter a write transaction or quota.
	s := &Service{}
	for _, color := range []string{"", "purple", "BLUE", " blue", "blue ", "#2563eb", "var(--blue)", "rgb(0,0,0)", "\xff", "blue\u0000", "blué"} {
		if validWatchlistTagColor(color) {
			t.Fatalf("accepted color %q", color)
		}
		if _, err := s.CreateWatchlistTag(t.Context(), "", "owner", "0", "Tag", color); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid create color", err)
		}
		if _, err := s.UpdateWatchlistTag(t.Context(), "", "owner", "0", "1", "Tag", color); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid update color", err)
		}
	}
}
