package accounts

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestWatchlistPreferencesInputBounds(t *testing.T) {
	// Syntax must fail before accessing quota/storage.
	s := &Service{}
	for _, mode := range []string{"", "List", "TAGS", "list ", " tags", "grouped", "\xff"} {
		if _, err := s.SaveWatchlistPreferences(t.Context(), "", "owner", "0", mode, ""); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid mode accepted", err)
		}
	}
	for _, id := range []string{"0", "-1", "+1", "01", " 1", "1 ", "1.0", "1e1", "9223372036854775808", "null", "\xff"} {
		if _, err := s.SaveWatchlistPreferences(t.Context(), "", "owner", "0", "list", id); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid filter accepted", err)
		}
	}
	for _, revision := range []string{"", "01", "-1", " 1", "9007199254740992"} {
		if _, err := s.SaveWatchlistPreferences(t.Context(), "", "owner", revision, "tags", ""); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid revision accepted", err)
		}
	}
}

func TestWatchlistPreferencesDTOBoundaries(t *testing.T) {
	id := "9223372036854775807"
	for _, filter := range []*string{nil, &id} {
		view := WatchlistView{ViewMode: "tags", FilterTagID: filter}
		want := `"filter_tag_id":null`
		if filter != nil {
			want = `"filter_tag_id":"` + id + `"`
		}
		for _, value := range []any{view, WatchlistImportView{Watchlist: view}} {
			data, err := json.Marshal(value)
			if err != nil || !strings.Contains(string(data), `"view_mode":"tags"`) || !strings.Contains(string(data), want) {
				t.Fatal("required preference fields missing or imprecise", err)
			}
		}
	}
	for _, value := range []any{WatchlistMovie{}, WatchlistSearchView{}, WatchlistTag{}} {
		data, err := json.Marshal(value)
		if err != nil || strings.Contains(string(data), "view_mode") || strings.Contains(string(data), "filter_tag_id") {
			t.Fatal("preferences leaked outside private snapshot", err)
		}
	}
}
