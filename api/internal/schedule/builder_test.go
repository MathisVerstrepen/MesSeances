package schedule

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestDatasetBuilderMergesMovieSightings(t *testing.T) {
	b := NewDatasetBuilder(ProviderMajestic, "2026-08-15", time.Now(), BuilderOptions{})
	if !b.MergeMovie(MovieRecord{ProviderID: "m", Title: "Film"}) ||
		!b.MergeMovie(MovieRecord{ProviderID: "m", Title: "Film", RuntimeMinutes: 100, PosterURL: "https://a.test/p.jpg"}) ||
		!b.MergeMovie(MovieRecord{ProviderID: "m", Title: "Film", PosterURL: "https://a.test/other.jpg"}) {
		t.Fatal("compatible sightings rejected")
	}
	if m := b.Movie("m"); m.RuntimeMinutes != 100 || m.PosterURL != "https://a.test/p.jpg" {
		t.Fatalf("merged=%+v", m)
	}
	if b.MergeMovie(MovieRecord{ProviderID: "m", Title: "Other"}) || b.MergeMovie(MovieRecord{ProviderID: "m", Title: "Film", RuntimeMinutes: 90}) {
		t.Fatal("contradictory sighting accepted")
	}
	strict := NewDatasetBuilder(ProviderOcine, "2026-08-15", time.Now(), BuilderOptions{RejectPosterConflicts: true})
	strict.MergeMovie(MovieRecord{ProviderID: "m", Title: "Film", PosterURL: "https://a.test/p.jpg"})
	if strict.MergeMovie(MovieRecord{ProviderID: "m", Title: "Film", PosterURL: "https://a.test/other.jpg"}) {
		t.Fatal("poster conflict accepted")
	}
	b.MergeMovie(MovieRecord{ProviderID: "a", Title: "A"})
	b.UpdateMovie(MovieRecord{ProviderID: "a", Title: "A", PosterURL: "https://a.test/a.jpg"})
	b.UpdateMovie(MovieRecord{ProviderID: "unknown", Title: "U"})
	if !slices.Equal(b.MovieIDs(), []string{"a", "m"}) || b.MovieCount() != 2 || b.Movie("a").PosterURL == "" {
		t.Fatalf("ids=%v", b.MovieIDs())
	}
}

func TestDatasetBuilderRejectsDuplicateShowings(t *testing.T) {
	b := NewDatasetBuilder(ProviderMajestic, "2026-08-15", time.Now(), BuilderOptions{})
	first := ShowtimeRecord{ProviderShowingID: "s1", TheaterID: "t"}
	if err := b.AddShowtime(first); err != nil {
		t.Fatal(err)
	}
	if err := b.AddShowtime(first); !errors.Is(err, ErrDuplicateShowing) {
		t.Fatalf("duplicate err=%v", err)
	}
	if prior, ok := b.Showtime("s1"); !ok || prior.TheaterID != "t" || b.ShowtimeCount() != 1 {
		t.Fatal("lookup")
	}
	if _, ok := b.Showtime("missing"); ok {
		t.Fatal("missing showing found")
	}
	b.Showtimes()[0].Room = "Salle 1"
	if prior, _ := b.Showtime("s1"); prior.Room != "Salle 1" {
		t.Fatal("in-place refinement lost")
	}
}

func TestDatasetBuilderBuildsValidatedSnapshot(t *testing.T) {
	want := cineodeTestDataset()
	b := NewDatasetBuilder(ProviderCineode, want.Window.From, want.GeneratedAt, BuilderOptions{})
	theater := want.Theaters[0]
	theater.AvailableDates = nil
	b.AddTheater(theater)
	empty := TheaterRecord{Provider: ProviderCineode, ID: "cineode-4640", Slug: "cineode-4640", ProviderID: "4640", Name: "Sans séance", City: "Ville", AcceptedPasses: []string{}}
	b.AddTheater(empty)
	if !b.MergeMovie(want.Showtimes[0].Movie) {
		t.Fatal("movie")
	}
	if err := b.AddShowtime(want.Showtimes[0]); err != nil {
		t.Fatal(err)
	}
	b.ExtendWindow(want.Window.Through)
	b.ExtendWindow("2000-01-01")
	got, err := b.Build("Cineode")
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != ProviderCineode || got.Scope != ScopeAll || got.SchemaVersion != SchemaVersion || got.Timezone != Timezone || got.Window != want.Window || !got.GeneratedAt.Equal(want.GeneratedAt.UTC()) {
		t.Fatalf("header=%+v", got)
	}
	if !reflect.DeepEqual(got.Theaters[0], want.Theaters[0]) || !reflect.DeepEqual(got.Showtimes, want.Showtimes) {
		t.Fatalf("theaters=%+v showtimes=%+v", got.Theaters, got.Showtimes)
	}
	if got.Theaters[1].AvailableDates == nil || len(got.Theaters[1].AvailableDates) != 0 {
		t.Fatalf("theater without showings dates=%#v", got.Theaters[1].AvailableDates)
	}

	broken := NewDatasetBuilder(ProviderCineode, want.Window.From, want.GeneratedAt, BuilderOptions{})
	if err := broken.AddShowtime(want.Showtimes[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := broken.Build("Cineode"); !errors.Is(err, ErrDatasetValidation) || err.Error() != "dataset validation failed: invalid synchronized Cineode dataset" {
		t.Fatalf("orphan showing err=%v", err)
	}
}

func TestSortShowtimesOrdersByTheaterStartAndID(t *testing.T) {
	base := time.Date(2026, 8, 15, 20, 0, 0, 0, time.UTC)
	showtimes := []ShowtimeRecord{
		{ID: "c", TheaterID: "t2", StartTime: base},
		{ID: "b", TheaterID: "t1", StartTime: base},
		{ID: "a", TheaterID: "t1", StartTime: base},
		{ID: "d", TheaterID: "t1", StartTime: base.Add(-time.Hour)},
	}
	SortShowtimes(showtimes)
	var ids []string
	for _, s := range showtimes {
		ids = append(ids, s.ID)
	}
	if !slices.Equal(ids, []string{"d", "a", "b", "c"}) {
		t.Fatalf("order=%v", ids)
	}
}

func TestSyncLocationValidatesCanonicalDates(t *testing.T) {
	if loc, err := SyncLocation("2026-08-15"); err != nil || loc.String() != Timezone {
		t.Fatalf("loc=%v err=%v", loc, err)
	}
	for _, from := range []string{"", "2026-8-15", "2026-02-30", "15/08/2026"} {
		if _, err := SyncLocation(from); err == nil || err.Error() != "invalid sync date" {
			t.Fatalf("from=%q err=%v", from, err)
		}
	}
}
