package enrichment

import (
	"context"
	"testing"
	"time"

	"messeances/api/internal/tmdb"
)

func TestMatcherProductionTitleAliases(t *testing.T) {
	for _, test := range []struct {
		provider, source, query, candidate string
	}{
		{SourceKinepolis, "EV : Ready Player One", "ready player one", "Ready Player One"},
		{SourceKinepolis, "EV : AP : Cœur secret", "coeur secret", "Coeur secret"},
		{SourceKinepolis, "EV AP : Teenage Sex and Death at Camp Miasma", "teenage sex and death at camp miasma", "Teenage Sex and Death at Camp Miasma"},
		{SourceKinepolis, "Silent Movie EV : Vive le sport!", "vive le sport", "Vive le sport !"},
		{SourceKinepolis, "Halloween : Coco", "coco", "Coco"},
		{SourcePathe, "La Séance Ciné Hits : SKYFALL", "skyfall", "Skyfall"},
		{SourcePathe, "Il était une fois... Casino", "casino", "Casino"},
		{SourcePathe, "Elle(s) au cinéma : Garance", "garance", "Garance"},
		{SourcePathe, "Le pape en France, au coeur d'un voyage historique", "Le pape en France, au coeur d'un voyage historique", "Le Pape en France, au cœur d’un voyage historique"},
		{SourceCGR, "Thelma et Louise", "Thelma et Louise", "Thelma & Louise"},
		{SourceCGR, "Master and Commander", "master et commander", "Master & Commander"},
		{SourceCGR, "Wallace & Gromit", "Wallace & Gromit", "Wallace et Gromit"},
		{SourceKinepolis, "Kultissime : Fast & Furious", "fast furious", "Fast & Furious"},
		{SourceUGC, "Wallace et Gromit", "Wallace et Gromit", "Royal Ballet & Opera 2026/27: Wallace & Gromit"},
	} {
		t.Run(test.source, func(t *testing.T) {
			store := newMemoryStore()
			provider := &fakeProvider{
				searchByQuery: map[string][]tmdb.Candidate{test.query: {{ID: 42, Title: test.candidate}}},
				details:       map[int64]tmdb.Details{42: {ID: 42, Title: test.candidate, Runtime: 100}},
			}
			movie := Movie{SourceProvider: test.provider, ProviderID: "10", Title: test.source, RuntimeMinutes: 100}
			summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{movie})
			match := store.matches["10"]
			if err != nil || summary.Matched != 1 || match.MetadataMovieID != 42 || provider.detailCalls != 1 {
				t.Fatalf("summary=%+v match=%+v queries=%v err=%v", summary, match, provider.searchQueries, err)
			}
			if match.NormalizedSourceTitle != NormalizeTitle(test.source) || match.Candidates[0].Title != test.candidate || store.metadata[42].LocalizedTitle != test.candidate {
				t.Fatalf("fingerprint or display title changed: match=%+v metadata=%+v", match, store.metadata[42])
			}
		})
	}
}

func TestSourceEventLabelBoundaries(t *testing.T) {
	for _, test := range []struct{ provider, source, want string }{
		{SourceKinepolis, "EV : AP : Film", "film"},
		{SourceKinepolis, "EV AP : Film", "film"},
		{SourceKinepolis, "Horror Night : Film", "film"},
		{SourceKinepolis, "Avt 1ère + Direct : Film", "film"},
		{SourcePathe, "Il était une fois… Film", "film"},
		{SourcePathe, "Avant-première nationale : Film", "film"},
		{SourcePathe, "Horror Cinema Club : Film", "film"},
		{SourceKinepolis, "Halloween Ends", "halloween ends"},
		{SourceKinepolis, "Halloween", "halloween"},
		{SourceKinepolis, "EV :", "ev"},
		{SourceKinepolis, "EV Film", "ev film"},
		{SourceKinepolis, "EV : CM : Film", "cm film"},
		{SourceKinepolis, "Festival : Film", "festival film"},
		{SourceKinepolis, "Film : Halloween", "film halloween"},
		{SourceUGC, "EV : Film", "ev film"},
		{SourcePathe, "Halloween : Film", "halloween film"},
		{SourceUGC, "Il était une fois... Film", "il etait une fois film"},
		{SourcePathe, "Il était une fois dans l'Ouest", "il etait une fois dans l ouest"},
	} {
		t.Run(test.provider+"/"+test.source, func(t *testing.T) {
			if got := CanonicalTitle(test.provider, test.source); got != test.want {
				t.Fatalf("canonical=%q want=%q", got, test.want)
			}
		})
	}
	if got := NormalizeTitle("Cœur & Âme (VF)"); got != "cœur ame vf" {
		t.Fatalf("persisted fingerprint changed: %q", got)
	}
}

func TestMatcherTitleAliasesPreserveSafeguards(t *testing.T) {
	for _, test := range []struct {
		name, source string
		titles       []string
		runtime      int
	}{
		{"six minute mismatch", "EV : Cœur secret", []string{"Coeur secret"}, 106},
		{"equivalent IDs remain ambiguous", "EV : Thelma et Louise", []string{"Thelma et Louise", "Thelma & Louise"}, 100},
		{"sequel number", "EV : Film 2", []string{"Film 1"}, 100},
		{"part number", "EV : Film Partie 2", []string{"Film Partie 1"}, 100},
		{"year", "EV : Film 2026", []string{"Film 2025"}, 100},
		{"truncated title", "EV : Ducobu et le fantôme de...", []string{"Ducobu et le fantôme de Saint-Potache"}, 100},
		{"additional subtitle", "EV : Cœur secret", []string{"Coeur secret : Le retour"}, 100},
		{"leading connector is meaningful", "Et maintenant", []string{"And maintenant"}, 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryStore()
			provider := &fakeProvider{details: map[int64]tmdb.Details{}}
			for index, title := range test.titles {
				id := int64(index + 1)
				provider.search = append(provider.search, tmdb.Candidate{ID: id, Title: title})
				provider.details[id] = tmdb.Details{ID: id, Title: title, Runtime: test.runtime}
			}
			summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{SourceProvider: SourceKinepolis, ProviderID: "10", Title: test.source, RuntimeMinutes: 100}})
			if err != nil || summary.ReviewRequired != 1 || store.publications != 0 {
				t.Fatalf("summary=%+v match=%+v err=%v", summary, store.matches["10"], err)
			}
		})
	}

	t.Run("sticky rejection", func(t *testing.T) {
		const title = "EV : Cœur & Âme"
		store := newMemoryStore()
		store.matches["10"] = Match{Status: StatusRejected, NormalizedSourceTitle: NormalizeTitle(title), SourceRuntimeMinutes: 100}
		provider := &fakeProvider{}
		summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).ForceRun(context.Background(), []Movie{{SourceProvider: SourceKinepolis, ProviderID: "10", Title: title, RuntimeMinutes: 100}})
		if err != nil || summary.Reused != 1 || provider.searches != 0 || store.publications != 0 || store.decisions != 0 {
			t.Fatalf("summary=%+v err=%v", summary, err)
		}
	})
}

func TestMatcherEventLabelCrossProviderReuse(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "unique identity", true: "conflicting identities"}[conflict], func(t *testing.T) {
			store := newMemoryStore()
			store.reusable = []ReusableMetadataMatch{{SourceProvider: SourceCGR, NormalizedSourceTitle: "ready player one", SourceRuntimeMinutes: 140, MetadataMovieID: 42, Score: 1}}
			store.metadata[42] = Metadata{ProviderMovieID: 42, LocalizedTitle: "Ready Player One", RuntimeMinutes: 140, RefreshAfter: matcherNow.Add(time.Hour)}
			if conflict {
				store.reusable = append(store.reusable, ReusableMetadataMatch{SourceProvider: SourcePathe, NormalizedSourceTitle: "ready player one", SourceRuntimeMinutes: 140, MetadataMovieID: 43, Score: 1})
			}
			provider := &fakeProvider{}
			summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{SourceProvider: SourceKinepolis, ProviderID: "10", Title: "EV : Ready Player One", RuntimeMinutes: 140}})
			if err != nil {
				t.Fatal(err)
			}
			if conflict {
				if summary.Unmatched != 1 || store.publications != 0 || provider.searches == 0 {
					t.Fatalf("conflicting match reused: summary=%+v", summary)
				}
			} else if summary.Matched != 1 || store.matches["10"].MetadataMovieID != 42 || provider.searches != 0 || provider.detailCalls != 0 {
				t.Fatalf("confirmed match not reused: summary=%+v provider=%+v", summary, provider)
			}
		})
	}
}
