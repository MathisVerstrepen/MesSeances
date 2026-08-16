package enrichment

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"movieflow/api/internal/tmdb"
)

type memoryStore struct {
	matches                 map[string]Match
	metadata                map[int64]Metadata
	decisions, publications int
}

func newMemoryStore() *memoryStore {
	return &memoryStore{matches: map[string]Match{}, metadata: map[int64]Metadata{}}
}
func (s *memoryStore) Match(_ context.Context, _, id, _ string) (Match, bool, error) {
	value, ok := s.matches[id]
	return value, ok, nil
}
func (s *memoryStore) Metadata(_ context.Context, _ string, id int64, _ string) (Metadata, bool, error) {
	value, ok := s.metadata[id]
	return value, ok, nil
}
func (s *memoryStore) SaveDecision(_ context.Context, match Match) error {
	s.decisions++
	s.matches[match.SourceMovieID] = match
	return nil
}
func (s *memoryStore) Publish(_ context.Context, match Match, metadata Metadata) error {
	s.publications++
	s.matches[match.SourceMovieID] = match
	s.metadata[metadata.ProviderMovieID] = metadata
	return nil
}

type fakeProvider struct {
	search                []tmdb.Candidate
	details               map[int64]tmdb.Details
	searchErr, detailErr  error
	searches, detailCalls int
}

func (p *fakeProvider) Search(context.Context, string) ([]tmdb.Candidate, error) {
	p.searches++
	return p.search, p.searchErr
}
func (p *fakeProvider) Details(_ context.Context, id int64) (tmdb.Details, error) {
	p.detailCalls++
	if p.detailErr != nil {
		return tmdb.Details{}, p.detailErr
	}
	return p.details[id], nil
}

var matcherNow = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

func TestNormalizeTitle(t *testing.T) {
	if got := NormalizeTitle("  Léon: L'Professionnel! "); got != "leon l professionnel" {
		t.Fatalf("normalized=%q", got)
	}
}

func TestMatcherAcceptanceBoundariesAndAmbiguity(t *testing.T) {
	tests := []struct {
		name       string
		runtimes   []int
		wantStatus string
	}{
		{"five minute boundary", []int{95}, StatusMatched},
		{"six minute boundary", []int{96}, StatusReviewRequired},
		{"margin exactly point zero five", []int{90, 95}, StatusMatched},
		{"ambiguous margin", []int{90, 94}, StatusReviewRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, provider := newMemoryStore(), &fakeProvider{details: map[int64]tmdb.Details{}}
			for index, runtime := range test.runtimes {
				id := int64(index + 1)
				provider.search = append(provider.search, tmdb.Candidate{ID: id, Title: "Amélie", OriginalTitle: "Amélie"})
				provider.details[id] = tmdb.Details{ID: id, Title: "Amélie", OriginalTitle: "Amélie", Runtime: runtime, Genres: []string{}}
			}
			summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{ProviderID: "10", Title: "Amélie", RuntimeMinutes: 90}})
			if err != nil || store.matches["10"].Status != test.wantStatus {
				t.Fatalf("summary=%+v match=%+v err=%v", summary, store.matches["10"], err)
			}
		})
	}
}

func TestMatcherNoResultRetryAndFreshCache(t *testing.T) {
	store, provider := newMemoryStore(), &fakeProvider{details: map[int64]tmdb.Details{}}
	matcher := NewMatcher(store, provider, func() time.Time { return matcherNow })
	summary, err := matcher.Run(context.Background(), []Movie{{ProviderID: "10", Title: "Film", RuntimeMinutes: 90}, {ProviderID: "10", Title: "Film", RuntimeMinutes: 90}})
	if err != nil || summary.Unmatched != 1 || provider.searches != 1 {
		t.Fatalf("summary=%+v searches=%d err=%v", summary, provider.searches, err)
	}
	summary, err = matcher.Run(context.Background(), []Movie{{ProviderID: "10", Title: "Film", RuntimeMinutes: 90}})
	if err != nil || summary.Reused != 1 || provider.searches != 1 {
		t.Fatalf("retry summary=%+v searches=%d err=%v", summary, provider.searches, err)
	}
	store.matches["20"] = Match{SourceProvider: SourceUGC, SourceMovieID: "20", MetadataProvider: ProviderTMDB, Status: StatusMatched, MetadataMovieID: 2, Score: 1, NormalizedSourceTitle: "cached", SourceRuntimeMinutes: 100, EvaluatedAt: matcherNow, RetryAfter: matcherNow.Add(metadataTTL)}
	store.metadata[2] = Metadata{Provider: ProviderTMDB, ProviderMovieID: 2, Locale: LocaleFrench, ProviderTitle: "Cached", LocalizedTitle: "Cached", RuntimeMinutes: 100, Genres: []string{}, FetchedAt: matcherNow, RefreshAfter: matcherNow.Add(time.Hour)}
	summary, err = matcher.Run(context.Background(), []Movie{{ProviderID: "20", Title: "Cached", RuntimeMinutes: 100}})
	if err != nil || summary.Reused != 1 || provider.detailCalls != 0 {
		t.Fatalf("cache summary=%+v details=%d err=%v", summary, provider.detailCalls, err)
	}
	store.matches["30"] = Match{SourceProvider: SourceUGC, SourceMovieID: "30", MetadataProvider: ProviderTMDB, Status: StatusMatched, MetadataMovieID: 3, Score: 1, NormalizedSourceTitle: "stale", SourceRuntimeMinutes: 90, EvaluatedAt: matcherNow.Add(-metadataTTL), RetryAfter: matcherNow}
	store.metadata[3] = Metadata{Provider: ProviderTMDB, ProviderMovieID: 3, Locale: LocaleFrench, ProviderTitle: "Stale", LocalizedTitle: "Stale", RuntimeMinutes: 90, Genres: []string{}, FetchedAt: matcherNow.Add(-metadataTTL), RefreshAfter: matcherNow.Add(-time.Minute)}
	provider.details[3] = tmdb.Details{ID: 3, Title: "Stale", OriginalTitle: "Stale", BackdropURL: "https://image.tmdb.org/t/p/w780/stale.jpg", Runtime: 90, Genres: []string{}}
	summary, err = matcher.Run(context.Background(), []Movie{{ProviderID: "30", Title: "Stale", RuntimeMinutes: 90}})
	if err != nil || summary.Matched != 1 || provider.detailCalls != 1 || store.publications != 1 {
		t.Fatalf("stale summary=%+v details=%d publications=%d err=%v", summary, provider.detailCalls, store.publications, err)
	}
	if store.metadata[3].BackdropURL != provider.details[3].BackdropURL {
		t.Fatalf("backdrop=%q", store.metadata[3].BackdropURL)
	}
}

func TestValidateMetadataRejectsUnsafeBackdrops(t *testing.T) {
	base := Metadata{Provider: ProviderTMDB, ProviderMovieID: 42, Locale: LocaleFrench, ProviderTitle: "Film", LocalizedTitle: "Film", BackdropURL: "https://image.tmdb.org/t/p/w780/backdrop.jpg", RuntimeMinutes: 90, Genres: []string{}, FetchedAt: matcherNow, RefreshAfter: matcherNow.Add(metadataTTL)}
	if err := validateMetadata(base); err != nil {
		t.Fatalf("valid backdrop rejected: %v", err)
	}
	invalid := []string{
		"http://image.tmdb.org/t/p/w780/a.jpg",
		"https://evil.example/t/p/w780/a.jpg",
		"https://image.tmdb.org/t/p/w500/a.jpg",
		"https://user@image.tmdb.org/t/p/w780/a.jpg",
		"https://image.tmdb.org:443/t/p/w780/a.jpg",
		"https://image.tmdb.org/t/p/w780/a.jpg?x=1",
		"https://image.tmdb.org/t/p/w780/a.jpg#x",
		"https://image.tmdb.org/t/p/w780/../a.jpg",
		"https://image.tmdb.org/t/p/w780/a\\b.jpg",
		"https://image.tmdb.org/t/p/w780/",
		"https://image.tmdb.org/t/p/w780//a.jpg",
		"https://image.tmdb.org/t/p/w780/%2e%2e/a.jpg",
		"https://image.tmdb.org/t/p/w780/" + strings.Repeat("a", 4096),
	}
	for _, raw := range invalid {
		metadata := base
		metadata.BackdropURL = raw
		if err := validateMetadata(metadata); err == nil {
			t.Fatalf("unsafe backdrop accepted: %q", raw)
		}
	}
}

func TestMatcherReusesRejectionUntilSourceFingerprintChanges(t *testing.T) {
	store, provider := newMemoryStore(), &fakeProvider{details: map[int64]tmdb.Details{}}
	store.matches["10"] = Match{SourceProvider: SourceUGC, SourceMovieID: "10", MetadataProvider: ProviderTMDB, Status: StatusRejected, NormalizedSourceTitle: "film", SourceRuntimeMinutes: 90, Candidates: []Candidate{{ID: 1, Title: "Film"}}, EvaluatedAt: matcherNow, RetryAfter: matcherNow}
	matcher := NewMatcher(store, provider, func() time.Time { return matcherNow.Add(365 * 24 * time.Hour) })
	summary, err := matcher.Run(context.Background(), []Movie{{ProviderID: "10", Title: "Film", RuntimeMinutes: 90}})
	if err != nil || summary.Reused != 1 || provider.searches != 0 {
		t.Fatalf("same fingerprint summary=%+v searches=%d err=%v", summary, provider.searches, err)
	}
	summary, err = matcher.Run(context.Background(), []Movie{{ProviderID: "10", Title: "Film remonté", RuntimeMinutes: 90}})
	if err != nil || summary.Unmatched != 1 || provider.searches != 1 || store.matches["10"].Status != StatusUnmatched {
		t.Fatalf("changed fingerprint summary=%+v searches=%d match=%+v err=%v", summary, provider.searches, store.matches["10"], err)
	}
}

func TestMatcherProviderFailureCreatesNoDecisionAndStops(t *testing.T) {
	store := newMemoryStore()
	provider := &fakeProvider{searchErr: tmdb.ErrStop}
	summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{ProviderID: "10", Title: "Film", RuntimeMinutes: 90}})
	if !errors.Is(err, tmdb.ErrStop) || summary.Failed != 1 || len(store.matches) != 0 {
		t.Fatalf("summary=%+v matches=%v err=%v", summary, store.matches, err)
	}
}

func TestMatcherPersistsSearchPostersForScoredAndUnscoredCandidates(t *testing.T) {
	store := newMemoryStore()
	provider := &fakeProvider{
		search: []tmdb.Candidate{
			{ID: 1, Title: "Film", OriginalTitle: "Film", PosterURL: "https://image.tmdb.org/t/p/w500/scored.jpg"},
			{ID: 2, Title: "Autre", OriginalTitle: "Other", PosterURL: "https://image.tmdb.org/t/p/w500/unscored.jpg"},
		},
		details: map[int64]tmdb.Details{1: {ID: 1, Title: "Film", OriginalTitle: "Film", Runtime: 96, Genres: []string{}}},
	}
	_, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{ProviderID: "10", Title: "Film", RuntimeMinutes: 90}})
	match := store.matches["10"]
	if err != nil || match.Status != StatusReviewRequired || len(match.Candidates) != 2 || match.Candidates[0].PosterURL != provider.search[0].PosterURL || match.Candidates[1].PosterURL != provider.search[1].PosterURL || provider.detailCalls != 1 {
		t.Fatalf("match=%+v detail calls=%d err=%v", match, provider.detailCalls, err)
	}
}

func TestValidateMatchRejectsUnsafeOrTransientCandidateURLs(t *testing.T) {
	base := Match{SourceProvider: SourceUGC, SourceMovieID: "10", MetadataProvider: ProviderTMDB, Status: StatusReviewRequired, NormalizedSourceTitle: "film", SourceRuntimeMinutes: 90, Candidates: []Candidate{{ID: 1, Title: "Film", PosterURL: "https://image.tmdb.org/t/p/w500/poster.jpg"}}, EvaluatedAt: matcherNow, RetryAfter: matcherNow.Add(decisionTTL)}
	if err := validateMatch(base); err != nil {
		t.Fatalf("valid poster rejected: %v", err)
	}
	tests := []Candidate{
		{ID: 1, Title: "Film", PosterURL: "http://image.tmdb.org/t/p/w500/poster.jpg"},
		{ID: 1, Title: "Film", PosterURL: "https://evil.example/t/p/w500/poster.jpg"},
		{ID: 1, Title: "Film", DetailURL: "https://www.themoviedb.org/movie/1?language=fr-FR"},
	}
	for _, candidate := range tests {
		match := base
		match.Candidates = []Candidate{candidate}
		if err := validateMatch(match); err == nil {
			t.Fatalf("unsafe candidate accepted: %+v", candidate)
		}
	}
}
