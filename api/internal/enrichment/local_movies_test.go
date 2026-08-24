package enrichment

import (
	"context"
	"errors"
	"testing"
)

type localMovieMemoryStore struct {
	groups  []LocalMovieGroup
	merged  []LocalMovieSource
	primary LocalMovieSource
	unmerge int64
}

func (s *localMovieMemoryStore) LocalMovieGroups(context.Context, int, int) ([]LocalMovieGroup, error) {
	return s.groups, nil
}
func (s *localMovieMemoryStore) MergeLocalMovies(_ context.Context, members []LocalMovieSource, primary LocalMovieSource) (LocalMovieGroup, error) {
	s.merged, s.primary = members, primary
	return LocalMovieGroup{ID: 7, Primary: primary, Members: []LocalMovieMember{{LocalMovieSource: members[0], Available: true}, {LocalMovieSource: members[1], Available: true}}}, nil
}
func (s *localMovieMemoryStore) UnmergeLocalMovie(_ context.Context, id int64) error {
	s.unmerge = id
	return nil
}

func TestLocalMovieServiceValidatesMerge(t *testing.T) {
	ugc := LocalMovieSource{SourceProvider: SourceUGC, SourceMovieID: "10"}
	kinepolis := LocalMovieSource{SourceProvider: SourceKinepolis, SourceMovieID: "HO0001"}
	pathe := LocalMovieSource{SourceProvider: SourcePathe, SourceMovieID: "film-a"}
	tests := []struct {
		name    string
		members []LocalMovieSource
		primary LocalMovieSource
	}{
		{"too few", []LocalMovieSource{ugc}, ugc},
		{"duplicate", []LocalMovieSource{ugc, ugc}, ugc},
		{"primary outside members", []LocalMovieSource{ugc, kinepolis}, LocalMovieSource{SourceProvider: SourceUGC, SourceMovieID: "11"}},
		{"invalid member", []LocalMovieSource{ugc, {SourceProvider: SourceKinepolis, SourceMovieID: "bad id"}}, ugc},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &localMovieMemoryStore{}
			_, err := NewLocalMovieService(store).Merge(context.Background(), test.members, test.primary)
			if !errors.Is(err, ErrLocalMovieInvalid) || store.merged != nil {
				t.Fatalf("merged=%v error=%v", store.merged, err)
			}
		})
	}
	store := &localMovieMemoryStore{}
	group, err := NewLocalMovieService(store).Merge(context.Background(), []LocalMovieSource{ugc, kinepolis}, kinepolis)
	if err != nil || group.LocalMovieID != "local-film-7" || group.MetadataSource == nil || *group.MetadataSource != kinepolis {
		t.Fatalf("group=%+v error=%v", group, err)
	}
	if err := validateLocalMovieMerge([]LocalMovieSource{ugc, pathe}, pathe); err != nil {
		t.Fatalf("valid Pathé local movie member rejected: %v", err)
	}
}

func TestLocalMovieServiceIDsAndFallback(t *testing.T) {
	for _, invalid := range []string{"", "local-film-0", "local-film--1", "local-film-01", "ugc-film-1"} {
		if _, err := ParseLocalMovieID(invalid); !errors.Is(err, ErrLocalMovieInvalid) {
			t.Fatalf("value=%q error=%v", invalid, err)
		}
	}
	primary := LocalMovieSource{SourceProvider: SourceUGC, SourceMovieID: "10"}
	fallback := LocalMovieSource{SourceProvider: SourceKinepolis, SourceMovieID: "A1"}
	store := &localMovieMemoryStore{groups: []LocalMovieGroup{{ID: 9, Primary: primary, Members: []LocalMovieMember{{LocalMovieSource: fallback, Available: true}, {LocalMovieSource: primary}}}}}
	groups, err := NewLocalMovieService(store).Groups(context.Background(), 20, 0)
	if err != nil || len(groups) != 1 || groups[0].LocalMovieID != "local-film-9" || groups[0].MetadataSource == nil || *groups[0].MetadataSource != fallback {
		t.Fatalf("groups=%+v error=%v", groups, err)
	}
	if err := NewLocalMovieService(store).Unmerge(context.Background(), "local-film-9"); err != nil || store.unmerge != 9 {
		t.Fatalf("unmerge=%d error=%v", store.unmerge, err)
	}
}
