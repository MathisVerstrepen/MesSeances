package accounts

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWatchedDefaultsAndWire(t *testing.T) {
	p := defaultWatchedPreferences()
	if !p.valid() || p.AutoRemoveWatchlist || p.DefaultVisibility != "hide" {
		t.Fatal("invalid defaults")
	}
	b, err := json.Marshal(WatchedSnapshot{Username: "owner", Revision: "0", Items: []WatchedItem{}, Preferences: p})
	want := `{"username":"owner","revision":"0","items":[],"preferences":{"default_visibility":"hide","search_visibility":"inherit","planning_visibility":"inherit","films_visibility":"inherit","upcoming_visibility":"inherit","home_visibility":"inherit","cinema_city_visibility":"inherit","auto_remove_watchlist":false}}`
	if err != nil || string(b) != want {
		t.Fatalf("wire=%s err=%v", b, err)
	}
	b, err = json.Marshal(WatchedMutationResult{Watched: WatchedSnapshot{Items: []WatchedItem{}, Preferences: p}})
	if err != nil || !strings.Contains(string(b), `"watchlist":null`) {
		t.Fatal("missing explicit null watchlist", err)
	}
	b, err = json.Marshal(WatchedItem{WatchlistMovie: WatchlistMovie{Slug: "film-1", Title: "Film"}, WatchedAt: time.Date(2026, 10, 15, 12, 0, 0, 123456000, time.UTC)})
	if err != nil || string(b) != `{"slug":"film-1","title":"Film","watched_at":"2026-10-15T12:00:00.123456Z"}` {
		t.Fatalf("item=%s err=%v", b, err)
	}
}

func TestWatchedValidationBeforeStorage(t *testing.T) {
	s := &Service{}
	for _, revision := range []string{"", "-1", "+1", "01", " 1", "1 ", "1.0", "1e1", "9007199254740992", "9223372036854775808"} {
		for _, watchlist := range []bool{false, true} {
			r, wr := revision, "0"
			if watchlist {
				r, wr = "0", revision
			}
			if _, err := s.SaveWatched(t.Context(), "", "owner", r, "film-1", wr, false); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("revision %q accepted", revision)
			}
		}
		if _, err := s.SaveWatchedPreferences(t.Context(), "", "owner", revision, defaultWatchedPreferences()); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("preference revision accepted")
		}
	}
	for _, value := range []string{"", "SHOW", "hide ", " inherit", "auto", "\xff"} {
		p := defaultWatchedPreferences()
		fields := []*string{&p.DefaultVisibility, &p.SearchVisibility, &p.PlanningVisibility, &p.FilmsVisibility, &p.UpcomingVisibility, &p.HomeVisibility, &p.CinemaCityVisibility}
		for _, field := range fields {
			before := *field
			*field = value
			if _, err := s.SaveWatchedPreferences(t.Context(), "", "owner", "0", p); !errors.Is(err, ErrInvalidInput) {
				t.Fatal("invalid enum accepted")
			}
			*field = before
		}
	}
	for _, value := range []string{"hide", "show", "inherit"} {
		p := defaultWatchedPreferences()
		p.SearchVisibility, p.PlanningVisibility, p.FilmsVisibility, p.UpcomingVisibility, p.HomeVisibility, p.CinemaCityVisibility = value, value, value, value, value, value
		if !p.valid() {
			t.Fatal("valid section enum rejected")
		}
	}
}
