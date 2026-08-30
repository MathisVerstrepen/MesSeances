package enrichment

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

type adminMovieStoreStub struct {
	query AdminMovieQuery
	patch AdminMoviePatch
	id    int64
}

func (store *adminMovieStoreStub) AdminMovies(_ context.Context, query AdminMovieQuery) (AdminMovieList, error) {
	store.query = query
	return AdminMovieList{Items: []AdminMovieItem{}, Limit: query.Limit, Offset: query.Offset}, nil
}

func (store *adminMovieStoreStub) UpdateAdminMovie(_ context.Context, id int64, patch AdminMoviePatch) (AdminMovieItem, error) {
	store.id, store.patch = id, patch
	return AdminMovieItem{ID: "7"}, nil
}

func TestAdminMovieServiceValidatesAndNormalizesList(t *testing.T) {
	store := &adminMovieStoreStub{}
	service := NewAdminMovieService(store)
	runtimeMin, runtimeMax := 90, 120
	from, to := "2026-01-01", "2026-12-31"
	result, err := service.List(context.Background(), AdminMovieQuery{
		Limit: 50, Offset: 2, Search: "  Titre  ", RuntimeMin: &runtimeMin, RuntimeMax: &runtimeMax,
		ReleaseDateFrom: &from, ReleaseDateTo: &to, Genre: " Drame ", OverrideStatus: "overridden",
		OverrideField: AdminMovieFieldTitle, Sort: "updated_at", Direction: "desc",
	})
	if err != nil || result.Items == nil || store.query.Search != "Titre" || store.query.Genre != "Drame" {
		t.Fatalf("result=%+v query=%+v err=%v", result, store.query, err)
	}

	invalid := []AdminMovieQuery{
		{Limit: 0, OverrideStatus: "all", Sort: "title", Direction: "asc"},
		{Limit: 50, Offset: -1, OverrideStatus: "all", Sort: "title", Direction: "asc"},
		{Limit: 50, RuntimeMin: &runtimeMax, RuntimeMax: &runtimeMin, OverrideStatus: "all", Sort: "title", Direction: "asc"},
		{Limit: 50, ReleaseDateFrom: stringPointer("2026-02-30"), OverrideStatus: "all", Sort: "title", Direction: "asc"},
		{Limit: 50, OverrideStatus: "automatic", OverrideField: AdminMovieFieldTitle, Sort: "title", Direction: "asc"},
		{Limit: 50, OverrideStatus: "all", Sort: "unknown", Direction: "asc"},
	}
	for _, query := range invalid {
		if _, err := service.List(context.Background(), query); !errors.Is(err, ErrAdminMovieInvalid) {
			t.Fatalf("query %+v err=%v", query, err)
		}
	}
}

func TestAdminMovieServiceValidatesAndNormalizesPatch(t *testing.T) {
	store := &adminMovieStoreStub{}
	service := NewAdminMovieService(store)
	title := "  Film corrigé  "
	genres := []string{" Drame ", "Comédie"}
	patch := AdminMoviePatch{
		ExpectedUpdatedAt: time.Now(),
		Overrides: AdminMovieOverrides{
			Title:  AdminMovieOverrideValue[string]{Present: true, Value: &title},
			Genres: AdminMovieOverrideValue[[]string]{Present: true, Value: &genres},
		},
		Restore: []AdminMovieField{AdminMovieFieldOverview},
	}
	if _, err := service.Update(context.Background(), 7, patch); err != nil {
		t.Fatal(err)
	}
	if store.id != 7 || *store.patch.Overrides.Title.Value != "Film corrigé" || !reflect.DeepEqual(*store.patch.Overrides.Genres.Value, []string{"Drame", "Comédie"}) {
		t.Fatalf("stored patch=%+v", store.patch)
	}

	invalid := []AdminMoviePatch{
		{ExpectedUpdatedAt: time.Now()},
		{ExpectedUpdatedAt: time.Now(), Restore: []AdminMovieField{AdminMovieFieldTitle, AdminMovieFieldTitle}},
		{ExpectedUpdatedAt: time.Now(), Restore: []AdminMovieField{AdminMovieFieldTitle}, Overrides: AdminMovieOverrides{Title: AdminMovieOverrideValue[string]{Present: true, Value: &title}}},
		{ExpectedUpdatedAt: time.Now(), Overrides: AdminMovieOverrides{Title: AdminMovieOverrideValue[string]{Present: true}}},
		{ExpectedUpdatedAt: time.Now(), Overrides: AdminMovieOverrides{Genres: AdminMovieOverrideValue[[]string]{Present: true}}},
		{ExpectedUpdatedAt: time.Now(), Overrides: AdminMovieOverrides{PosterURL: AdminMovieOverrideValue[string]{Present: true, Value: stringPointer("http://example.com/a.jpg")}}},
		{ExpectedUpdatedAt: time.Now(), Overrides: AdminMovieOverrides{TrailerVFYouTubeKey: AdminMovieOverrideValue[string]{Present: true, Value: stringPointer("short")}}},
	}
	for _, candidate := range invalid {
		if _, err := service.Update(context.Background(), 7, candidate); !errors.Is(err, ErrAdminMovieInvalid) {
			t.Fatalf("patch %+v err=%v", candidate, err)
		}
	}
}

func TestAdminMovieServicePatchBoundaries(t *testing.T) {
	service := NewAdminMovieService(&adminMovieStoreStub{})
	now := time.Now()
	maxTitle := strings.Repeat("é", 1024)
	maxOverview := strings.Repeat("é", 10000)
	maxURL := "https://a/" + strings.Repeat("x", 4086)
	maxGenre := strings.Repeat("é", 256)
	maxGenres := make([]string, 32)
	for index := range maxGenres {
		maxGenres[index] = maxGenre
	}
	maxRuntime := math.MaxInt32
	valid := []AdminMoviePatch{
		{ExpectedUpdatedAt: now, Overrides: AdminMovieOverrides{Title: AdminMovieOverrideValue[string]{Present: true, Value: &maxTitle}}},
		{ExpectedUpdatedAt: now, Overrides: AdminMovieOverrides{RuntimeMinutes: AdminMovieOverrideValue[int]{Present: true, Value: &maxRuntime}}},
		{ExpectedUpdatedAt: now, Overrides: AdminMovieOverrides{Genres: AdminMovieOverrideValue[[]string]{Present: true, Value: &maxGenres}}},
		{ExpectedUpdatedAt: now, Overrides: AdminMovieOverrides{Overview: AdminMovieOverrideValue[string]{Present: true, Value: &maxOverview}}},
		{ExpectedUpdatedAt: now, Overrides: AdminMovieOverrides{PosterURL: AdminMovieOverrideValue[string]{Present: true, Value: &maxURL}}},
		{ExpectedUpdatedAt: now, Overrides: AdminMovieOverrides{ReleaseDate: AdminMovieOverrideValue[string]{Present: true, Value: nil}}},
	}
	for _, patch := range valid {
		if _, err := service.Update(context.Background(), 1, patch); err != nil {
			t.Fatalf("valid boundary patch rejected: %v", err)
		}
	}
	tooManyGenres := append(append([]string(nil), maxGenres...), "extra")
	invalid := []AdminMoviePatch{
		{ExpectedUpdatedAt: now, Overrides: AdminMovieOverrides{Title: AdminMovieOverrideValue[string]{Present: true, Value: stringPointer(maxTitle + "x")}}},
		{ExpectedUpdatedAt: now, Overrides: AdminMovieOverrides{Genres: AdminMovieOverrideValue[[]string]{Present: true, Value: &tooManyGenres}}},
		{ExpectedUpdatedAt: now, Overrides: AdminMovieOverrides{Genres: AdminMovieOverrideValue[[]string]{Present: true, Value: &[]string{maxGenre + "x"}}}},
		{ExpectedUpdatedAt: now, Overrides: AdminMovieOverrides{Overview: AdminMovieOverrideValue[string]{Present: true, Value: stringPointer(maxOverview + "x")}}},
		{ExpectedUpdatedAt: now, Overrides: AdminMovieOverrides{PosterURL: AdminMovieOverrideValue[string]{Present: true, Value: stringPointer(maxURL + "x")}}},
		{ExpectedUpdatedAt: now, Overrides: AdminMovieOverrides{Overview: AdminMovieOverrideValue[string]{Present: true, Value: stringPointer("invalid\x00text")}}},
	}
	for _, patch := range invalid {
		if _, err := service.Update(context.Background(), 1, patch); !errors.Is(err, ErrAdminMovieInvalid) {
			t.Fatalf("invalid boundary patch accepted: %+v err=%v", patch, err)
		}
	}
}

func stringPointer(value string) *string { return &value }
