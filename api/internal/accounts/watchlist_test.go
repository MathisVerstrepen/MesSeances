package accounts

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestWatchlistInputBounds(t *testing.T) {
	for _, input := range []string{"", "1", strings.Repeat("é", 201), "ab\x00cd", "\xffab"} {
		if _, err := watchlistQuery(input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("query accepted %q", input)
		}
	}
	for _, input := range []string{"éà", strings.Repeat("é", 200), "  Movie  ", "%_"} {
		if query, err := watchlistQuery(input); err != nil || query != strings.TrimSpace(input) {
			t.Fatal("valid query rejected")
		}
	}
	for _, input := range []string{"", "0", "-1", "+1", "01", " 1", "1.0", "9223372036854775808"} {
		if _, err := watchlistTMDBID(input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("TMDB id accepted %q", input)
		}
	}
	for _, input := range []string{"1", "9223372036854775807"} {
		if _, err := watchlistTMDBID(input); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWatchlistExternalAdmissionHasNoQueue(t *testing.T) {
	s := &Service{watchlistGate: make(chan struct{}, 2)}
	one, err := s.admitWatchlistExternal()
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.admitWatchlistExternal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.admitWatchlistExternal(); err == nil {
		t.Fatal("third external operation admitted")
	}
	one()
	three, err := s.admitWatchlistExternal()
	if err != nil {
		t.Fatal(err)
	}
	two()
	three()
}

func TestWatchlistJSONHasOnlyPublicMovieFields(t *testing.T) {
	encoded, err := json.Marshal(WatchlistView{Username: "owner", Revision: "0", SortOrder: "added_desc", Items: []WatchlistItem{}, Tags: []WatchlistTag{}})
	if err != nil || string(encoded) != `{"username":"owner","revision":"0","sort_order":"added_desc","items":[],"tags":[],"external_search_available":false}` {
		t.Fatalf("wire=%s error=%v", encoded, err)
	}
	encoded, err = json.Marshal(WatchlistItem{WatchlistMovie: WatchlistMovie{Slug: "film-1", Title: "Film"}, TagIDs: []string{}})
	if err != nil || strings.Contains(string(encoded), "WatchlistMovie") || strings.Contains(string(encoded), "account_id") || strings.Contains(string(encoded), "tmdb_id") {
		t.Fatal("private identity or nested movie leaked")
	}
}
