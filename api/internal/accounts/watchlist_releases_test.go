package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/tmdb"
)

func TestWatchlistReleaseOptionalAndSharedAdmission(t *testing.T) {
	provider := watchlistReleaseFunc(func(context.Context, int64) (tmdb.ReleaseEvidence, error) {
		t.Fatal("unadmitted provider call")
		return tmdb.ReleaseEvidence{}, nil
	})
	s := &Service{now: time.Now, watchlistGate: make(chan struct{}, 2)}
	if r, err := s.SweepWatchlistReleases(t.Context()); err != nil || r.Attempted != 0 {
		t.Fatal("optional provider used database", r, err)
	}
	s.watchlistReleaseProvider = provider
	first, err := s.admitWatchlistExternal()
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	second, err := s.admitWatchlistExternal()
	if err != nil {
		t.Fatal(err)
	}
	defer second()
	if r, err := s.SweepWatchlistReleases(t.Context()); err != nil || r.Attempted != 0 {
		t.Fatal("gate not shared", r, err)
	}
	s.watchlistReleaseMu.Lock()
	if r, err := s.SweepWatchlistReleases(t.Context()); err != nil || r.Attempted != 0 {
		t.Fatal("overlapping sweep", r, err)
	}
	s.watchlistReleaseMu.Unlock()
}

type releaseBeginFunc func(context.Context, pgx.TxOptions) (pgx.Tx, error)

func (f releaseBeginFunc) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	return f(ctx, options)
}

func TestWatchlistReleaseDatabaseAdmissionFailure(t *testing.T) {
	begins := 0
	store := NewPostgresStore(releaseBeginFunc(func(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
		begins++
		deadline, ok := ctx.Deadline()
		if remaining := time.Until(deadline); !ok || remaining <= 0 || remaining > 2*time.Second {
			t.Fatal("unbounded database admission", remaining)
		}
		return nil, errors.New("private database diagnostic")
	}))
	s := &Service{store: store, now: time.Now, watchlistGate: make(chan struct{}, 2), watchlistReleaseProvider: watchlistReleaseFunc(func(context.Context, int64) (tmdb.ReleaseEvidence, error) {
		t.Fatal("provider called after database failure")
		return tmdb.ReleaseEvidence{}, nil
	})}
	if result, err := s.SweepWatchlistReleases(t.Context()); !errors.Is(err, ErrUnavailable) || result.Attempted != 0 || len(s.watchlistGate) != 0 {
		t.Fatal("selection failure not contained", result, err)
	}
	if revision, err := s.claimWatchlistRelease(t.Context(), 1); !errors.Is(err, ErrUnavailable) || revision != 0 {
		t.Fatal("quota database failure not contained", revision, err)
	}
	if begins != 2 {
		t.Fatal("unexpected database operations", begins)
	}
}

func TestWatchlistReleaseDTOOnlySavedItems(t *testing.T) {
	for _, tc := range []struct {
		value   any
		present bool
	}{
		{WatchlistItem{WatchlistMovie: WatchlistMovie{ReleaseDate: "1998-01-01"}, FrenchReleaseDate: "1998-10-14", TagIDs: []string{}}, true},
		{WatchlistItem{WatchlistMovie: WatchlistMovie{ReleaseDate: "1998-01-01"}, TagIDs: []string{}}, false},
		{WatchlistMovie{ReleaseDate: "1998-01-01"}, false},
		{WatchlistExternalMovie{ReleaseDate: "1998-01-01"}, false},
	} {
		data, err := json.Marshal(tc.value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"french_release_date"`) != tc.present {
			t.Fatal("saved-only field", string(data))
		}
	}
}
