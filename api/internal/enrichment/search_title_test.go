package enrichment

import (
	"context"
	"testing"
	"time"

	"messeances/api/internal/tmdb"
)

func TestCleanSearchTitle(t *testing.T) {
	for _, test := range []struct{ title, want string }{
		{"America America 2 (Version Kannada)", "America America 2"},
		{"America (Version (Kannada)) America 2 (VF)", "America America 2"},
		{"Film(VF)2", "Film 2"},
		{"  Léon:\tL'Professionnel!\u00a0 (VF) \n", "Léon: L'Professionnel!"},
		{"Léon & Amélie: 2?", "Léon & Amélie: 2?"},
		{"(Version Kannada) (VF)", ""},
		{"Film ()", "Film"},
		{"Film (unfinished", "Film (unfinished"},
		{"Film ) unfinished", "Film ) unfinished"},
		{"Film (VF) (unfinished", "Film (unfinished"},
		{"Film (unfinished (VF)", "Film (unfinished (VF)"},
		{"", ""},
		{" \t\n ", ""},
	} {
		t.Run(test.title, func(t *testing.T) {
			if got := cleanSearchTitle(test.title); got != test.want {
				t.Fatalf("cleaned=%q want=%q", got, test.want)
			}
		})
	}
}

func TestMatcherSearchesAndScoresWithoutParenthesizedAnnotations(t *testing.T) {
	for _, sourceProvider := range []string{SourceUGC, SourceKinepolis, SourcePathe, SourceCGR, SourceMegarama} {
		t.Run(sourceProvider, func(t *testing.T) {
			movieID := "10"
			if sourceProvider == SourceMegarama {
				movieID = "ABCDE"
			}
			movie := Movie{SourceProvider: sourceProvider, ProviderID: movieID, Title: "America America 2 (Version Kannada)", RuntimeMinutes: 100}
			store := newMemoryStore()
			provider := &fakeProvider{
				searchByQuery: map[string][]tmdb.Candidate{"America America 2": {{ID: 42, Title: "America America 2 (Restored (2026))"}}},
				details:       map[int64]tmdb.Details{42: {ID: 42, Title: "America America 2 (Restored (2026))", OriginalTitle: "America America 2", Runtime: 100}},
			}
			summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{movie})
			match := store.matches[movie.ProviderID]
			if err != nil || summary.Matched != 1 || match.Score != 1 || provider.searches != 1 || provider.searchQueries[0] != "America America 2" || provider.detailCalls != 1 {
				t.Fatalf("summary=%+v match=%+v provider=%+v err=%v", summary, match, provider, err)
			}
			if movie.Title != "America America 2 (Version Kannada)" || match.NormalizedSourceTitle != "america america 2 version kannada" || match.Candidates[0].Title != provider.details[42].Title || store.metadata[42].LocalizedTitle != provider.details[42].Title {
				t.Fatalf("stored/display titles changed: movie=%+v match=%+v metadata=%+v", movie, match, store.metadata[42])
			}
		})
	}
}

func TestMatcherEmptyCleanedTitleDoesNotSearchOrReuse(t *testing.T) {
	store := newMemoryStore()
	store.reusable = []ReusableMetadataMatch{{SourceProvider: SourceKinepolis, NormalizedSourceTitle: "version kannada", SourceRuntimeMinutes: 100, MetadataMovieID: 42, Score: 1}}
	provider := &fakeProvider{}
	summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{ProviderID: "10", Title: "(Version Kannada)", RuntimeMinutes: 100}})
	if err != nil || summary.Unmatched != 1 || provider.searches != 0 || provider.detailCalls != 0 || store.publications != 0 {
		t.Fatalf("summary=%+v provider=%+v err=%v", summary, provider, err)
	}
	if err := validateMatch(store.matches["10"]); err != nil {
		t.Fatalf("invalid unmatched decision: %v", err)
	}
	for _, title := range []string{"", "(VF)", " (()) ", "!?"} {
		queries, exact := searchQueries(title, SourceUGC)
		if len(queries) != 0 || len(exact) != 0 || candidateTitleMatches(title, map[string]struct{}{"": {}}) {
			t.Fatalf("empty cleaned title accepted: %q queries=%v exact=%v", title, queries, exact)
		}
	}
}

func TestMatcherCleanedTitlePreservesRejectionAndScoringSafeguards(t *testing.T) {
	const title = "America America 2 (Version Kannada)"
	t.Run("sticky rejected fingerprint", func(t *testing.T) {
		store := newMemoryStore()
		store.matches["10"] = Match{SourceProvider: SourceUGC, SourceMovieID: "10", Status: StatusRejected, NormalizedSourceTitle: NormalizeTitle(title), SourceRuntimeMinutes: 100}
		provider := &fakeProvider{}
		summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).ForceRun(context.Background(), []Movie{{ProviderID: "10", Title: title, RuntimeMinutes: 100}})
		if err != nil || summary.Reused != 1 || provider.searches != 0 || store.decisions != 0 || store.publications != 0 {
			t.Fatalf("summary=%+v provider=%+v err=%v", summary, provider, err)
		}
	})
	for _, test := range []struct {
		name     string
		runtimes []int
	}{
		{"runtime mismatch", []int{106}},
		{"ambiguous candidates", []int{100, 100}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryStore()
			provider := &fakeProvider{details: map[int64]tmdb.Details{}}
			for index, runtime := range test.runtimes {
				id := int64(index + 1)
				provider.search = append(provider.search, tmdb.Candidate{ID: id, Title: "America America 2 (VF)"})
				provider.details[id] = tmdb.Details{ID: id, Title: "America America 2 (VF)", Runtime: runtime}
			}
			summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{ProviderID: "10", Title: title, RuntimeMinutes: 100}})
			if err != nil || summary.ReviewRequired != 1 || store.publications != 0 {
				t.Fatalf("summary=%+v match=%+v err=%v", summary, store.matches["10"], err)
			}
		})
	}
}
