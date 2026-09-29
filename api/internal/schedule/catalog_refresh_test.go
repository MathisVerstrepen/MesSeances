package schedule

import (
	"context"
	"sync"
	"testing"
	"time"
)

func importedCatalogFixture() Dataset {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	return Dataset{SchemaVersion: SchemaVersion, Timezone: Timezone, GeneratedAt: now, CatalogPublishedAt: now, PublicMovies: []PublicMovieRecord{{ID: 1, IdentityAnchorTMDBID: 42, TMDBID: 42, Title: "Catalog import", RuntimeMinutes: 0, UpdatedAt: now}}}
}

func TestImportedCatalogOnlyAndMissingRefresh(t *testing.T) {
	reader := &fakeSnapshotReader{loadErr: ErrNoCompleteSnapshot, versionErr: ErrNoCompleteSnapshot}
	source, err := NewPostgresSource(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(source, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reader.data = importedCatalogFixture()
	reader.enrichmentVersion = 1
	reader.loadErr = nil
	reader.versionErr = nil
	for _, slug := range []string{"whatever", "film-01", "film--1", "film-9223372036854775808"} {
		if err = service.RefreshMovie(t.Context(), slug); err == nil {
			t.Fatal("invalid slug refresh succeeded")
		}
	}
	if reader.checks != 0 {
		t.Fatal("malformed slugs caused database IO")
	}
	if err = service.RefreshMovie(t.Context(), "film-1"); err != nil {
		t.Fatal(err)
	}
	if !service.HasCatalog() || service.HasSnapshot() {
		t.Fatal("catalog import invented provider snapshot")
	}
	if _, err = service.UpcomingMovies(UpcomingMoviesQuery{Page: 1}); err == nil {
		t.Fatal("catalog import invented upcoming completion")
	}
	view, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: "film-1", Date: "2026-09-22"})
	if err != nil || view.CurrentlyScreened || view.Theaters == nil || len(view.Theaters) != 0 || view.Movie.RuntimeMinutes != 0 {
		t.Fatalf("import view %+v %v", view, err)
	}
	checks := reader.checks
	if err = service.RefreshMovie(t.Context(), "film-1"); err != nil || reader.checks != checks {
		t.Fatal("known movie caused unnecessary database IO")
	}
}

func TestCatalogRefreshSerializesPollingAndPublicRead(t *testing.T) {
	reader := &fakeSnapshotReader{data: importedCatalogFixture(), enrichmentVersion: 1}
	source, err := NewPostgresSource(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	reader.mu.Lock()
	reader.enrichmentVersion = 2
	reader.loadStarted = make(chan struct{}, 1)
	reader.loadRelease = make(chan struct{})
	reader.mu.Unlock()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := source.Refresh(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	<-reader.loadStarted
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := source.Refresh(ctx); err == nil {
		t.Fatal("waiting refresh ignored cancellation")
	}
	close(reader.loadRelease)
	wg.Wait()
	if err = source.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.loads != 2 {
		t.Fatalf("duplicate full reloads=%d", reader.loads)
	}
}
