package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/tmdb"
)

type memoryStore struct {
	matches                 map[string]Match
	metadata                map[int64]Metadata
	locallyMerged           map[string]bool
	reusable                []ReusableMetadataMatch
	decisions, publications int
}

func newMemoryStore() *memoryStore {
	return &memoryStore{matches: map[string]Match{}, metadata: map[int64]Metadata{}, locallyMerged: map[string]bool{}}
}
func (s *memoryStore) IsLocallyMerged(_ context.Context, provider, id string) (bool, error) {
	return s.locallyMerged[provider+"\x00"+id], nil
}
func (s *memoryStore) Match(_ context.Context, _, id, _ string) (Match, bool, error) {
	value, ok := s.matches[id]
	return value, ok, nil
}
func (s *memoryStore) ConfirmedMatches(_ context.Context, excludeProvider, _ string, minimum, maximum int) ([]ReusableMetadataMatch, error) {
	result := []ReusableMetadataMatch{}
	for _, match := range s.reusable {
		if match.SourceProvider != excludeProvider && match.SourceRuntimeMinutes >= minimum && match.SourceRuntimeMinutes <= maximum {
			result = append(result, match)
		}
	}
	return result, nil
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
	searchByQuery         map[string][]tmdb.Candidate
	searchErrByQuery      map[string]error
	searchQueries         []string
	details               map[int64]tmdb.Details
	detailErrByID         map[int64]error
	searchErr, detailErr  error
	searches, detailCalls int
}

func (p *fakeProvider) Search(_ context.Context, query string) ([]tmdb.Candidate, error) {
	p.searches++
	p.searchQueries = append(p.searchQueries, query)
	if err := p.searchErrByQuery[query]; err != nil {
		return nil, err
	}
	if candidates, found := p.searchByQuery[query]; found {
		return candidates, nil
	}
	return p.search, p.searchErr
}
func (p *fakeProvider) Details(_ context.Context, id int64) (tmdb.Details, error) {
	p.detailCalls++
	if err := p.detailErrByID[id]; err != nil {
		return tmdb.Details{}, err
	}
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

func TestCanonicalTitleProviderEditorialWrappers(t *testing.T) {
	tests := []struct {
		name, provider, title, want string
	}{
		{"kultissime", SourceKinepolis, "Kultissime - Film", "film"},
		{"season", SourceKinepolis, "Saison 24/25 : Film", "film"},
		{"team visit", SourceKinepolis, "Visite d'équipe - Film", "film"},
		{"spotlight", SourceKinepolis, "Lumière Sur... Film", "film"},
		{"masterclass", SourceKinepolis, "MasterClass Film", "film"},
		{"magic morning", SourceKinepolis, "Matinée Magique Film", "film"},
		{"special showing", SourceKinepolis, "Séance Spéciale Film", "film"},
		{"cine cool", SourceKinepolis, "AP Ciné Cool Film", "film"},
		{"premiere", SourceKinepolis, "Avant Première Film", "film"},
		{"concert", SourceKinepolis, "Ciné concert Film", "film"},
		{"debate", SourceKinepolis, "Ciné Débat Film", "film"},
		{"relax", SourceKinepolis, "Ciné Relax Film", "film"},
		{"classics", SourceKinepolis, "Les Classiques Film", "film"},
		{"manga", SourceKinepolis, "Manga K Film", "film"},
		{"comedie francaise year", SourceKinepolis, "Comédie Française 2026 - Film", "film"},
		{"royal opera", SourceUGC, "Film (THE ROYAL OPERA)", "film"},
		{"royal ballet", SourceUGC, "Film (THE ROYAL BALLET)", "film"},
		{"paris opera", SourceUGC, "Film (OPERA DE PARIS)", "film"},
		{"comedie francaise", SourceUGC, "Film (COMEDIE-FRANCAISE)", "film"},
		{"repeated", SourceKinepolis, "Kultissime - Avant Première - Film", "film"},
		{"leading anniversary", SourceUGC, "40th anniversary - Film", "film"},
		{"trailing anniversary", SourceKinepolis, "Film - 40TH ANNIVERSARY", "film"},
		{"leading rediffusion", SourceKinepolis, "Rediffusion - Film", "film"},
		{"trailing rediffusion", SourceUGC, "Film - REDIFFUSION", "film"},
		{"combined edition and provider", SourceKinepolis, "40th anniversary - Avant Première - Film - rediffusion", "film"},
		{"empty remainder", SourceKinepolis, "Kultissime", "kultissime"},
		{"empty edition remainder", SourceUGC, "rediffusion", "rediffusion"},
		{"unknown", SourceKinepolis, "Festival Film", "festival film"},
		{"wrong provider prefix", SourceUGC, "Kultissime Film", "kultissime film"},
		{"wrong provider suffix", SourceKinepolis, "Film (THE ROYAL OPERA)", "film the royal opera"},
		{"invalid season width", SourceKinepolis, "Saison 4/25 Film", "saison 4 25 film"},
		{"invalid comedie year width", SourceKinepolis, "Comédie Française 26 Film", "comedie francaise 26 film"},
		{"comedie year wrong provider", SourceUGC, "Comédie Française 2026 Film", "comedie francaise 2026 film"},
		{"unknown anniversary", SourceUGC, "25th anniversary Film", "25th anniversary film"},
		{"embedded rediffusion", SourceUGC, "Film rediffusion gala", "film rediffusion gala"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := CanonicalTitle(test.provider, test.title); got != test.want {
				t.Fatalf("canonical=%q want=%q", got, test.want)
			}
		})
	}
}

func TestMatcherSearchesRawThenCanonicalAndDeduplicatesIDs(t *testing.T) {
	store := newMemoryStore()
	provider := &fakeProvider{
		searchByQuery: map[string][]tmdb.Candidate{
			"Film (THE ROYAL OPERA)": {{ID: 1, Title: "Film", OriginalTitle: "Film"}},
			"film":                   {{ID: 1, Title: "Film", OriginalTitle: "Film"}, {ID: 2, Title: "Film", OriginalTitle: "Film"}},
		},
		details: map[int64]tmdb.Details{
			1: {ID: 1, Title: "Film", OriginalTitle: "Film", Runtime: 90, Genres: []string{}},
			2: {ID: 2, Title: "Film", OriginalTitle: "Film", Runtime: 100, Genres: []string{}},
		},
	}
	_, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{ProviderID: "10", Title: "Film (THE ROYAL OPERA)", RuntimeMinutes: 90}})
	match := store.matches["10"]
	if err != nil || len(provider.searchQueries) != 2 || provider.searchQueries[0] != "Film (THE ROYAL OPERA)" || provider.searchQueries[1] != "film" || provider.detailCalls != 2 || len(match.Candidates) != 2 {
		t.Fatalf("queries=%v details=%d match=%+v err=%v", provider.searchQueries, provider.detailCalls, match, err)
	}
}

func TestSourceEditionAndDynamicWrapperQueryOrder(t *testing.T) {
	for _, test := range []struct {
		provider, raw, canonical string
	}{
		{SourceUGC, "40th anniversary - Film", "film"},
		{SourceKinepolis, "Comédie Française 2026 - Film - rediffusion", "film"},
	} {
		queries, _ := searchQueries(test.raw, test.provider)
		if len(queries) != 2 || queries[0] != test.raw || queries[1] != test.canonical {
			t.Fatalf("provider=%q queries=%v", test.provider, queries)
		}
	}
}

func TestMatcherControlledTMDBCandidateWrappers(t *testing.T) {
	tests := []struct {
		name, source, candidate string
		original                bool
	}{
		{"royal localized", "CARMEN (THE ROYAL OPERA)", "Royal Ballet & Opera 2026/27: Carmen", false},
		{"royal original", "CARMEN (THE ROYAL OPERA)", "Royal Ballet & Opera 2026/27: Carmen", true},
		{"bastille localized", "NOTRE-DAME DE PARIS (OPERA DE PARIS)", "Notre-Dame de Paris (Opéra Bastille)", false},
		{"bastille original", "NOTRE-DAME DE PARIS (OPERA DE PARIS)", "Notre-Dame de Paris (Opéra Bastille)", true},
		{"national localized", "NOTRE-DAME DE PARIS (OPERA DE PARIS)", "Notre-Dame de Paris [Opéra National de Paris]", false},
		{"national original", "NOTRE-DAME DE PARIS (OPERA DE PARIS)", "Notre-Dame de Paris [Opéra National de Paris]", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := tmdb.Candidate{ID: 42, Title: test.candidate, OriginalTitle: "Other"}
			if test.original {
				candidate.Title, candidate.OriginalTitle = "Other", test.candidate
			}
			store := newMemoryStore()
			provider := &fakeProvider{search: []tmdb.Candidate{candidate}, details: map[int64]tmdb.Details{42: {ID: 42, Title: "Canonical metadata", OriginalTitle: "Canonical metadata", Runtime: 90, Genres: []string{}}}}
			summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{ProviderID: "10", Title: test.source, RuntimeMinutes: 90}})
			match := store.matches["10"]
			if err != nil || summary.Matched != 1 || provider.detailCalls != 1 || match.Status != StatusMatched || len(match.Candidates) != 1 || match.Candidates[0].Title != candidate.Title || match.Candidates[0].OriginalTitle != candidate.OriginalTitle {
				t.Fatalf("summary=%+v match=%+v details=%d err=%v", summary, match, provider.detailCalls, err)
			}
		})
	}
}

func TestMatcherCandidateWrapperNearMissesFailClosed(t *testing.T) {
	tests := []struct {
		name, source, candidate string
	}{
		{"missing short year", "CARMEN (THE ROYAL OPERA)", "Royal Ballet & Opera 2026: Carmen"},
		{"missing season", "CARMEN (THE ROYAL OPERA)", "Royal Ballet & Opera: Carmen"},
		{"malformed long year", "CARMEN (THE ROYAL OPERA)", "Royal Ballet & Opera 26/27: Carmen"},
		{"generic royal ballet", "CARMEN (THE ROYAL OPERA)", "Royal Ballet Carmen"},
		{"generic opera containment", "CARMEN (THE ROYAL OPERA)", "Opera Carmen"},
		{"unapproved parentheses", "CARMEN (THE ROYAL OPERA)", "Carmen (Royal Opera House)"},
		{"unapproved brackets", "NOTRE-DAME DE PARIS (OPERA DE PARIS)", "Notre-Dame de Paris [Palais Garnier]"},
		{"extra subtitle", "CARMEN (THE ROYAL OPERA)", "Royal Ballet & Opera 2026/27: Carmen: Gala"},
		{"truncated", "CARMEN (THE ROYAL OPERA)", "Royal Ballet & Opera 2026/27: Carm..."},
		{"arbitrary year", "CARMEN (THE ROYAL OPERA)", "Carmen 2026"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryStore()
			provider := &fakeProvider{search: []tmdb.Candidate{{ID: 42, Title: test.candidate, OriginalTitle: "Other"}}, details: map[int64]tmdb.Details{42: {ID: 42, Title: test.candidate, OriginalTitle: test.candidate, Runtime: 90, Genres: []string{}}}}
			summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{ProviderID: "10", Title: test.source, RuntimeMinutes: 90}})
			match := store.matches["10"]
			if err != nil || summary.ReviewRequired != 1 || provider.detailCalls != 0 || match.Status != StatusReviewRequired || len(match.Candidates) != 1 || match.Candidates[0].Title != test.candidate || match.Candidates[0].Score != 0 {
				t.Fatalf("summary=%+v match=%+v details=%d err=%v", summary, match, provider.detailCalls, err)
			}
		})
	}
}

func TestMatcherDistinctControlledCandidateIDsRemainAmbiguous(t *testing.T) {
	store := newMemoryStore()
	provider := &fakeProvider{
		search: []tmdb.Candidate{
			{ID: 1, Title: "Royal Ballet & Opera 2026/27: Carmen", OriginalTitle: "Other"},
			{ID: 2, Title: "Carmen (Opéra Bastille)", OriginalTitle: "Other"},
		},
		details: map[int64]tmdb.Details{
			1: {ID: 1, Title: "Carmen", OriginalTitle: "Carmen", Runtime: 90, Genres: []string{}},
			2: {ID: 2, Title: "Carmen", OriginalTitle: "Carmen", Runtime: 90, Genres: []string{}},
		},
	}
	summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{ProviderID: "10", Title: "CARMEN (THE ROYAL OPERA)", RuntimeMinutes: 90}})
	match := store.matches["10"]
	if err != nil || summary.ReviewRequired != 1 || match.Status != StatusReviewRequired || provider.detailCalls != 2 || len(match.Candidates) != 2 || match.Candidates[0].Title != provider.search[0].Title || match.Candidates[1].Title != provider.search[1].Title {
		t.Fatalf("summary=%+v match=%+v details=%d err=%v", summary, match, provider.detailCalls, err)
	}
}

func TestMatcherConfirmedCrossProviderReuse(t *testing.T) {
	for _, delta := range []int{0, 1, 2} {
		t.Run(string(rune('0'+delta)), func(t *testing.T) {
			store := newMemoryStore()
			store.matches["10"] = Match{SourceProvider: SourceUGC, SourceMovieID: "10", MetadataProvider: ProviderTMDB, Status: StatusUnmatched, NormalizedSourceTitle: NormalizeTitle("Film (THE ROYAL OPERA)"), SourceRuntimeMinutes: 100, Candidates: []Candidate{}, EvaluatedAt: matcherNow, RetryAfter: matcherNow.Add(decisionTTL)}
			store.reusable = []ReusableMetadataMatch{{SourceProvider: SourceKinepolis, NormalizedSourceTitle: "Avant Première Film", SourceRuntimeMinutes: 100 + delta, MetadataMovieID: 42, Score: .97}, {SourceProvider: SourceKinepolis, NormalizedSourceTitle: "Film", SourceRuntimeMinutes: 100 + delta, MetadataMovieID: 42, Score: .99}}
			store.metadata[42] = Metadata{Provider: ProviderTMDB, ProviderMovieID: 42, Locale: LocaleFrench, ProviderTitle: "Film", LocalizedTitle: "Film", RuntimeMinutes: 100, Genres: []string{}, FetchedAt: matcherNow, RefreshAfter: matcherNow.Add(time.Hour)}
			provider := &fakeProvider{searchErr: errors.New("search must not run")}
			summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{SourceProvider: SourceUGC, ProviderID: "10", Title: "Film (THE ROYAL OPERA)", RuntimeMinutes: 100}})
			match := store.matches["10"]
			if err != nil || summary.Matched != 1 || provider.searches != 0 || store.publications != 1 || match.MetadataMovieID != 42 || match.Score != .99 || match.NormalizedSourceTitle != NormalizeTitle("Film (THE ROYAL OPERA)") || match.SourceRuntimeMinutes != 100 {
				t.Fatalf("summary=%+v match=%+v provider=%+v err=%v", summary, match, provider, err)
			}
		})
	}
}

func TestMatcherConflictingOrIneligibleReuseFallsBackToSearch(t *testing.T) {
	tests := []struct {
		name     string
		reusable []ReusableMetadataMatch
	}{
		{"conflicting IDs", []ReusableMetadataMatch{{SourceProvider: SourceKinepolis, NormalizedSourceTitle: "Avant Première Film", SourceRuntimeMinutes: 100, MetadataMovieID: 1, Score: 1}, {SourceProvider: SourceKinepolis, NormalizedSourceTitle: "Film", SourceRuntimeMinutes: 100, MetadataMovieID: 2, Score: 1}}},
		{"canonical mismatch", []ReusableMetadataMatch{{SourceProvider: SourceKinepolis, NormalizedSourceTitle: "Autre", SourceRuntimeMinutes: 100, MetadataMovieID: 1, Score: 1}}},
		{"runtime delta three", []ReusableMetadataMatch{{SourceProvider: SourceKinepolis, NormalizedSourceTitle: "Film", SourceRuntimeMinutes: 103, MetadataMovieID: 1, Score: 1}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryStore()
			store.reusable = test.reusable
			provider := &fakeProvider{details: map[int64]tmdb.Details{}}
			summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{SourceProvider: SourceUGC, ProviderID: "10", Title: "Film", RuntimeMinutes: 100}})
			if err != nil || summary.Unmatched != 1 || provider.searches != 1 || store.publications != 0 {
				t.Fatalf("summary=%+v searches=%d publications=%d err=%v", summary, provider.searches, store.publications, err)
			}
		})
	}
}

func TestMatcherReusableMissingRuntimeRefreshUsesSourceFallback(t *testing.T) {
	store := newMemoryStore()
	store.reusable = []ReusableMetadataMatch{{SourceProvider: SourceUGC, NormalizedSourceTitle: "Film", SourceRuntimeMinutes: 99, MetadataMovieID: 42, Score: .98}}
	provider := &fakeProvider{details: map[int64]tmdb.Details{42: {ID: 42, Title: "Film", OriginalTitle: "Film", Runtime: 0, Genres: []string{}}}}
	summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{SourceProvider: SourceKinepolis, ProviderID: "K-1", Title: "Avant Première Film", RuntimeMinutes: 100}})
	if err != nil || summary.Matched != 1 || store.metadata[42].RuntimeMinutes != 100 || store.matches["K-1"].Candidates[0].Runtime != 0 {
		t.Fatalf("summary=%+v metadata=%+v match=%+v err=%v", summary, store.metadata[42], store.matches["K-1"], err)
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

func TestMatcherSoleExactTitleMissingProviderRuntimeUsesSourceFallback(t *testing.T) {
	store := newMemoryStore()
	provider := &fakeProvider{
		search:  []tmdb.Candidate{{ID: 1, Title: "Film", OriginalTitle: "Film"}},
		details: map[int64]tmdb.Details{1: {ID: 1, Title: "Film", OriginalTitle: "Film", Runtime: 0, Genres: []string{}}},
	}
	summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{ProviderID: "10", Title: "Film", RuntimeMinutes: 98}})
	match := store.matches["10"]
	if err != nil || summary.Matched != 1 || summary.ReviewRequired != 0 || match.Status != StatusMatched || match.Score != .95 || store.publications != 1 || len(match.Candidates) != 1 || match.Candidates[0].Runtime != 0 || match.Candidates[0].Score != .95 || store.metadata[1].RuntimeMinutes != 98 {
		t.Fatalf("summary=%+v match=%+v publications=%d err=%v", summary, match, store.publications, err)
	}
}

func TestMetadataFromDetailsReturnsNonNilEmptyGenres(t *testing.T) {
	metadata := metadataFromDetails(tmdb.Details{ID: 577599, IMDBID: "tt1234567", Title: "Film", OriginalTitle: "Film", TrailerVFYouTubeKey: "FRoff123456", TrailerVOYouTubeKey: "ENoff123456", Runtime: 90}, 90, matcherNow)
	if metadata.Genres == nil || len(metadata.Genres) != 0 || metadata.IMDBID != "tt1234567" || metadata.TrailerVFYouTubeKey != "FRoff123456" || metadata.TrailerVOYouTubeKey != "ENoff123456" {
		t.Fatalf("metadata=%+v", metadata)
	}
}

func TestValidateMetadataRejectsMalformedIMDBID(t *testing.T) {
	base := Metadata{Provider: ProviderTMDB, ProviderMovieID: 42, IMDBID: "tt1234567", Locale: LocaleFrench, ProviderTitle: "Film", LocalizedTitle: "Film", RuntimeMinutes: 90, Genres: []string{}, FetchedAt: matcherNow, RefreshAfter: matcherNow.Add(metadataTTL)}
	if err := validateMetadata(base); err != nil {
		t.Fatalf("valid IMDb ID rejected: %v", err)
	}
	for _, imdbID := range []string{"TT1234567", "tt123456", "tt123456x", "tt" + strings.Repeat("1", 31)} {
		metadata := base
		metadata.IMDBID = imdbID
		if err := validateMetadata(metadata); err == nil {
			t.Fatalf("malformed IMDb ID accepted: %q", imdbID)
		}
	}
	base.IMDBID = ""
	if err := validateMetadata(base); err != nil {
		t.Fatalf("absent IMDb ID rejected: %v", err)
	}
}

func TestValidateMetadataRejectsMalformedTrailerYouTubeKeys(t *testing.T) {
	base := Metadata{Provider: ProviderTMDB, ProviderMovieID: 42, Locale: LocaleFrench, ProviderTitle: "Film", LocalizedTitle: "Film", TrailerVFYouTubeKey: "FRoff123456", TrailerVOYouTubeKey: "ENoff123456", RuntimeMinutes: 90, Genres: []string{}, FetchedAt: matcherNow, RefreshAfter: matcherNow.Add(metadataTTL)}
	if err := validateMetadata(base); err != nil {
		t.Fatalf("valid trailer key rejected: %v", err)
	}
	for _, key := range []string{"short", "FRoff12345!", "FRoff1234567", "FRoff12345/"} {
		for _, variant := range []string{"vf", "vo"} {
			metadata := base
			if variant == "vf" {
				metadata.TrailerVFYouTubeKey = key
			} else {
				metadata.TrailerVOYouTubeKey = key
			}
			if err := validateMetadata(metadata); err == nil {
				t.Fatalf("malformed %s trailer key accepted: %q", variant, key)
			}
		}
	}
	duplicate := base
	duplicate.TrailerVOYouTubeKey = duplicate.TrailerVFYouTubeKey
	if err := validateMetadata(duplicate); err == nil {
		t.Fatal("duplicate VF and VO trailer keys accepted")
	}
}

func TestMatcherMultipleExactTitlesWithMissingRuntimeRequireReview(t *testing.T) {
	store := newMemoryStore()
	provider := &fakeProvider{
		search: []tmdb.Candidate{{ID: 1, Title: "Film", OriginalTitle: "Film"}, {ID: 2, Title: "Film", OriginalTitle: "Film"}},
		details: map[int64]tmdb.Details{
			1: {ID: 1, Title: "Film", OriginalTitle: "Film", Runtime: 0, Genres: []string{}},
			2: {ID: 2, Title: "Film", OriginalTitle: "Film", Runtime: 0, Genres: []string{}},
		},
	}
	summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{ProviderID: "10", Title: "Film", RuntimeMinutes: 98}})
	match := store.matches["10"]
	if err != nil || summary.ReviewRequired != 1 || match.Status != StatusReviewRequired || len(match.Candidates) != 2 || match.Candidates[0].Score != .90 || store.publications != 0 {
		t.Fatalf("summary=%+v match=%+v err=%v", summary, match, err)
	}
}

func TestMatcherRanksCompleteUnionAndPersistsFive(t *testing.T) {
	store := newMemoryStore()
	search := make([]tmdb.Candidate, 0, 8)
	for id := int64(1); id <= 7; id++ {
		search = append(search, tmdb.Candidate{ID: id, Title: "Autre", OriginalTitle: "Other"})
	}
	search = append(search, tmdb.Candidate{ID: 100, Title: "Film", OriginalTitle: "Film", PosterURL: "https://image.tmdb.org/t/p/w500/film.jpg"})
	provider := &fakeProvider{search: search, details: map[int64]tmdb.Details{100: {ID: 100, Title: "Film", OriginalTitle: "Film", Runtime: 90, Genres: []string{}}}}
	summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{ProviderID: "10", Title: "Film", RuntimeMinutes: 90}})
	match := store.matches["10"]
	if err != nil || summary.Matched != 1 || match.Status != StatusMatched || len(match.Candidates) != 5 || match.Candidates[0].ID != 100 || match.Candidates[0].PosterURL != search[7].PosterURL || match.Candidates[1].ID != 1 {
		t.Fatalf("summary=%+v match=%+v err=%v", summary, match, err)
	}
}

func TestMatcherCandidateOutsidePersistedFiveStillAffectsAcceptance(t *testing.T) {
	store := newMemoryStore()
	provider := &fakeProvider{details: map[int64]tmdb.Details{}}
	for id := int64(1); id <= 6; id++ {
		provider.search = append(provider.search, tmdb.Candidate{ID: id, Title: "Film", OriginalTitle: "Film"})
		provider.details[id] = tmdb.Details{ID: id, Title: "Film", OriginalTitle: "Film", Runtime: 90, Genres: []string{}}
	}
	summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{ProviderID: "10", Title: "Film", RuntimeMinutes: 90}})
	match := store.matches["10"]
	if err != nil || summary.ReviewRequired != 1 || match.Status != StatusReviewRequired || len(match.Candidates) != 5 || provider.detailCalls != 6 || match.Candidates[4].ID != 5 {
		t.Fatalf("summary=%+v match=%+v details=%d err=%v", summary, match, provider.detailCalls, err)
	}
}

func TestMatcherCanonicalSearchFailurePersistsBoundedRawCandidates(t *testing.T) {
	providerFailure := errors.New("provider failure")
	store := newMemoryStore()
	raw := "Film (THE ROYAL BALLET)"
	provider := &fakeProvider{
		searchByQuery:    map[string][]tmdb.Candidate{raw: {}},
		searchErrByQuery: map[string]error{"film": providerFailure},
		details:          map[int64]tmdb.Details{},
	}
	for id := int64(10); id >= 1; id-- {
		provider.searchByQuery[raw] = append(provider.searchByQuery[raw], tmdb.Candidate{ID: id, Title: "Autre", OriginalTitle: "Other"})
	}
	summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{ProviderID: "10", Title: raw, RuntimeMinutes: 90}})
	match := store.matches["10"]
	if err != nil || summary.Failed != 1 || len(match.Candidates) != 5 || match.Candidates[0].ID != 1 || match.Candidates[4].ID != 5 || !match.RetryAfter.Equal(matcherNow) {
		t.Fatalf("summary=%+v match=%+v err=%v", summary, match, err)
	}
}

func TestMatcherUnmatchedRetryUsesEarliestShowingCalendarDate(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		showing time.Time
		want    time.Duration
	}{
		{"zero", time.Time{}, decisionTTL},
		{"future", time.Date(2026, 8, 17, 0, 1, 0, 0, paris), 24 * time.Hour},
		{"today timezone boundary", time.Date(2026, 8, 16, 23, 59, 0, 0, paris), decisionTTL},
		{"past", time.Date(2026, 8, 15, 23, 59, 0, 0, paris), decisionTTL},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryStore()
			provider := &fakeProvider{details: map[int64]tmdb.Details{}}
			_, runErr := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{ProviderID: "10", Title: "Film", RuntimeMinutes: 90, FirstShowingAt: test.showing}})
			match := store.matches["10"]
			if runErr != nil || !match.RetryAfter.Equal(matcherNow.Add(test.want)) {
				t.Fatalf("retry=%v want=%v err=%v", match.RetryAfter, matcherNow.Add(test.want), runErr)
			}
		})
	}
}

func TestMatcherReusesLocalMemberWithoutProviderCall(t *testing.T) {
	store := newMemoryStore()
	store.locallyMerged[SourceKinepolis+"\x00HO0001"] = true
	provider := &fakeProvider{searchErr: errors.New("must not be called")}
	summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{SourceProvider: SourceKinepolis, ProviderID: "HO0001", Title: "Film", RuntimeMinutes: 90}})
	if err != nil || summary.Reused != 1 || provider.searches != 0 || provider.detailCalls != 0 || store.decisions != 0 || store.publications != 0 {
		t.Fatalf("summary=%+v provider=%+v decisions=%d publications=%d error=%v", summary, provider, store.decisions, store.publications, err)
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

func TestMatcherStaleSameSourceMissingRuntimeUsesSourceFallback(t *testing.T) {
	store := newMemoryStore()
	store.matches["10"] = Match{SourceProvider: SourceUGC, SourceMovieID: "10", MetadataProvider: ProviderTMDB, Status: StatusMatched, MetadataMovieID: 3, Score: 1, NormalizedSourceTitle: "film", SourceRuntimeMinutes: 90, EvaluatedAt: matcherNow.Add(-metadataTTL), RetryAfter: matcherNow}
	provider := &fakeProvider{details: map[int64]tmdb.Details{3: {ID: 3, Title: "Film", OriginalTitle: "Film", Runtime: 0, Genres: []string{}}}}
	summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{ProviderID: "10", Title: "Film", RuntimeMinutes: 90}})
	if err != nil || summary.Matched != 1 || store.metadata[3].RuntimeMinutes != 90 {
		t.Fatalf("summary=%+v metadata=%+v err=%v", summary, store.metadata[3], err)
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

func TestRuntimeValidationAcceptsUnknownAndMarathonsAndRejectsInvalidValues(t *testing.T) {
	match := Match{SourceProvider: SourceUGC, SourceMovieID: "10", MetadataProvider: ProviderTMDB, Status: StatusUnmatched, NormalizedSourceTitle: "film", SourceRuntimeMinutes: 721, Candidates: []Candidate{{ID: 1, Title: "Film", Runtime: 721}}, EvaluatedAt: matcherNow, RetryAfter: matcherNow.Add(decisionTTL)}
	metadata := Metadata{Provider: ProviderTMDB, ProviderMovieID: 1, Locale: LocaleFrench, ProviderTitle: "Film", LocalizedTitle: "Film", RuntimeMinutes: 721, Genres: []string{}, FetchedAt: matcherNow, RefreshAfter: matcherNow.Add(metadataTTL)}
	if err := validateMatch(match); err != nil {
		t.Fatalf("marathon match rejected: %v", err)
	}
	if err := validateMetadata(metadata); err != nil {
		t.Fatalf("marathon metadata rejected: %v", err)
	}
	unknownMatch := match
	unknownMatch.SourceRuntimeMinutes = 0
	if err := validateMatch(unknownMatch); err != nil {
		t.Fatalf("unknown match runtime rejected: %v", err)
	}
	unknownMetadata := metadata
	unknownMetadata.RuntimeMinutes = 0
	if err := validateMetadata(unknownMetadata); err != nil {
		t.Fatalf("unknown metadata runtime rejected: %v", err)
	}
	for _, runtime := range []int{-1, int(math.MaxInt64/time.Minute) + 1} {
		invalidMatch := match
		invalidMatch.SourceRuntimeMinutes = runtime
		if err := validateMatch(invalidMatch); err == nil {
			t.Fatalf("invalid match runtime accepted: %d", runtime)
		}
		invalidMetadata := metadata
		invalidMetadata.RuntimeMinutes = runtime
		if err := validateMetadata(invalidMetadata); err == nil {
			t.Fatalf("invalid metadata runtime accepted: %d", runtime)
		}
	}
	invalidCandidate := match
	invalidCandidate.Candidates = []Candidate{{ID: 1, Title: "Film", Runtime: int(math.MaxInt64/time.Minute) + 1}}
	if err := validateMatch(invalidCandidate); err == nil {
		t.Fatal("representation-unsafe candidate runtime accepted")
	}
}

func TestPatheSourceIdentityContracts(t *testing.T) {
	valid := Match{SourceProvider: SourcePathe, SourceMovieID: "film-a_2", MetadataProvider: ProviderTMDB, Status: StatusUnmatched, NormalizedSourceTitle: "film", SourceRuntimeMinutes: 90, Candidates: []Candidate{}, EvaluatedAt: matcherNow, RetryAfter: matcherNow.Add(decisionTTL)}
	if err := validateMatch(valid); err != nil {
		t.Fatalf("valid Pathé source rejected: %v", err)
	}
	valid.SourceMovieID = strings.Repeat("a", 128-len("pathe-film-"))
	if err := validateMatch(valid); err != nil {
		t.Fatalf("maximum Pathé source identity rejected: %v", err)
	}
	for _, id := range []string{"", "-film", "bad id", strings.Repeat("a", 128-len("pathe-film-")+1)} {
		invalid := valid
		invalid.SourceMovieID = id
		if err := validateMatch(invalid); err == nil {
			t.Fatalf("invalid Pathé source accepted: %q", id)
		}
	}
}

func TestCGRSourceIdentityContracts(t *testing.T) {
	valid := Match{SourceProvider: SourceCGR, SourceMovieID: "1001", MetadataProvider: ProviderTMDB, Status: StatusUnmatched, NormalizedSourceTitle: "film", SourceRuntimeMinutes: 90, Candidates: []Candidate{}, EvaluatedAt: matcherNow, RetryAfter: matcherNow.Add(decisionTTL)}
	if err := validateMatch(valid); err != nil {
		t.Fatalf("valid CGR source rejected: %v", err)
	}
	for _, id := range []string{"", "0", "01", "film-a"} {
		invalid := valid
		invalid.SourceMovieID = id
		if err := validateMatch(invalid); err == nil {
			t.Fatalf("invalid CGR source accepted: %q", id)
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

func TestMatcherForceRunBypassesOnlyRetryAfter(t *testing.T) {
	for _, status := range []string{StatusUnmatched, StatusReviewRequired} {
		t.Run(status, func(t *testing.T) {
			store := newMemoryStore()
			store.matches["10"] = Match{SourceProvider: SourceUGC, SourceMovieID: "10", MetadataProvider: ProviderTMDB, Status: status, NormalizedSourceTitle: "film", SourceRuntimeMinutes: 90, Candidates: []Candidate{}, EvaluatedAt: matcherNow, RetryAfter: matcherNow.Add(time.Hour)}
			provider := &fakeProvider{details: map[int64]tmdb.Details{}}
			matcher := NewMatcher(store, provider, func() time.Time { return matcherNow })
			movie := Movie{ProviderID: "10", Title: "Film", RuntimeMinutes: 90}

			normal, err := matcher.Run(context.Background(), []Movie{movie})
			if err != nil || normal.Reused != 1 || provider.searches != 0 {
				t.Fatalf("normal summary=%+v searches=%d err=%v", normal, provider.searches, err)
			}
			forced, err := matcher.ForceRun(context.Background(), []Movie{movie})
			if err != nil || forced.Unmatched != 1 || forced.Reused != 0 || provider.searches != 1 {
				t.Fatalf("forced summary=%+v searches=%d err=%v", forced, provider.searches, err)
			}
		})
	}
}

func TestMatcherForceRunPreservesStickyRejection(t *testing.T) {
	store := newMemoryStore()
	store.matches["10"] = Match{SourceProvider: SourceUGC, SourceMovieID: "10", MetadataProvider: ProviderTMDB, Status: StatusRejected, NormalizedSourceTitle: "film", SourceRuntimeMinutes: 90, Candidates: []Candidate{}, EvaluatedAt: matcherNow, RetryAfter: matcherNow}
	provider := &fakeProvider{searchErr: errors.New("must not run")}
	summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).ForceRun(context.Background(), []Movie{{ProviderID: "10", Title: "Film", RuntimeMinutes: 90}})
	if err != nil || summary.Reused != 1 || provider.searches != 0 || store.decisions != 0 || store.publications != 0 {
		t.Fatalf("summary=%+v provider=%+v decisions=%d publications=%d err=%v", summary, provider, store.decisions, store.publications, err)
	}
}

func TestMatcherForceRunProviderStopHaltsRemainingMovies(t *testing.T) {
	store := newMemoryStore()
	provider := &fakeProvider{searchErr: tmdb.ErrStop}
	summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).ForceRun(context.Background(), []Movie{
		{ProviderID: "10", Title: "First", RuntimeMinutes: 90},
		{ProviderID: "20", Title: "Second", RuntimeMinutes: 90},
	})
	if !errors.Is(err, tmdb.ErrStop) || summary.Failed != 1 || provider.searches != 1 || len(store.matches) != 0 {
		t.Fatalf("summary=%+v searches=%d matches=%v err=%v", summary, provider.searches, store.matches, err)
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

func TestMatcherNonTerminalProviderFailureCreatesRetryableReviewDecision(t *testing.T) {
	providerFailure := errors.New("provider failure")
	tests := []struct {
		name              string
		movie             Movie
		provider          *fakeProvider
		detailCallsPerRun int
	}{
		{
			name:     "search failure",
			movie:    Movie{SourceProvider: SourceKinepolis, ProviderID: "HO00016099", Title: "Film", RuntimeMinutes: 90},
			provider: &fakeProvider{searchErr: providerFailure},
		},
		{
			name:  "details failure",
			movie: Movie{SourceProvider: SourceUGC, ProviderID: "17950", Title: "Film", RuntimeMinutes: 90},
			provider: &fakeProvider{
				search:    []tmdb.Candidate{{ID: 42, Title: "Film", OriginalTitle: "Film"}},
				detailErr: providerFailure,
			},
			detailCallsPerRun: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryStore()
			matcher := NewMatcher(store, test.provider, func() time.Time { return matcherNow })
			summary, err := matcher.Run(context.Background(), []Movie{test.movie})
			match, found := store.matches[test.movie.ProviderID]
			if err != nil || summary.Failed != 1 || !found || store.decisions != 1 {
				t.Fatalf("summary=%+v match=%+v found=%t decisions=%d err=%v", summary, match, found, store.decisions, err)
			}
			if match.Status != StatusReviewRequired || !match.RetryAfter.Equal(matcherNow) || validateMatch(match) != nil {
				t.Fatalf("match=%+v validation=%v", match, validateMatch(match))
			}
			candidates, marshalErr := json.Marshal(match.Candidates)
			_, manuallyResolvable := validReviewCandidate(match.Status, match.NormalizedSourceTitle, match.SourceRuntimeMinutes, candidates, test.movie.Title, test.movie.RuntimeMinutes, 99)
			if marshalErr != nil || !manuallyResolvable {
				t.Fatalf("candidates=%v marshal err=%v manually resolvable=%t", match.Candidates, marshalErr, manuallyResolvable)
			}
			firstSearches := test.provider.searches
			summary, err = matcher.Run(context.Background(), []Movie{test.movie})
			if err != nil || summary.Failed != 1 || store.decisions != 2 || test.provider.searches != firstSearches+1 || test.provider.detailCalls != 2*test.detailCallsPerRun {
				t.Fatalf("retry summary=%+v searches=%d details=%d decisions=%d err=%v", summary, test.provider.searches, test.provider.detailCalls, store.decisions, err)
			}
		})
	}
}

func TestMatcherForceRunContinuesAfterRetryableProviderFailure(t *testing.T) {
	store := newMemoryStore()
	providerFailure := errors.New("temporary provider failure")
	provider := &fakeProvider{
		searchErrByQuery: map[string]error{"First": providerFailure},
		details:          map[int64]tmdb.Details{},
	}
	summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).ForceRun(context.Background(), []Movie{
		{ProviderID: "10", Title: "First", RuntimeMinutes: 90},
		{ProviderID: "20", Title: "Second", RuntimeMinutes: 90},
	})
	if err != nil || summary.Failed != 1 || summary.Unmatched != 1 || provider.searches != 2 || store.matches["10"].Status != StatusReviewRequired || store.matches["20"].Status != StatusUnmatched {
		t.Fatalf("summary=%+v searches=%d matches=%+v err=%v", summary, provider.searches, store.matches, err)
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

type qualificationStore struct{ matches map[string]Match }

func (s *qualificationStore) IsLocallyMerged(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *qualificationStore) Match(_ context.Context, provider, id, _ string) (Match, bool, error) {
	value, ok := s.matches[provider+"\x00"+id]
	return value, ok, nil
}
func (s *qualificationStore) ConfirmedMatches(context.Context, string, string, int, int) ([]ReusableMetadataMatch, error) {
	return nil, nil
}
func (s *qualificationStore) Metadata(context.Context, string, int64, string) (Metadata, bool, error) {
	return Metadata{}, false, nil
}
func (s *qualificationStore) SaveDecision(_ context.Context, match Match) error {
	s.matches[match.SourceProvider+"\x00"+match.SourceMovieID] = match
	return nil
}
func (s *qualificationStore) Publish(_ context.Context, match Match, _ Metadata) error {
	s.matches[match.SourceProvider+"\x00"+match.SourceMovieID] = match
	return nil
}

func TestMatcherQualifiesOverlappingMovieIDsBySourceProvider(t *testing.T) {
	store := &qualificationStore{matches: map[string]Match{}}
	provider := &fakeProvider{details: map[int64]tmdb.Details{}}
	summary, err := NewMatcher(store, provider, func() time.Time { return matcherNow }).Run(context.Background(), []Movie{{SourceProvider: SourceUGC, ProviderID: "10", Title: "UGC", RuntimeMinutes: 90}, {SourceProvider: SourceKinepolis, ProviderID: "10", Title: "Kinepolis", RuntimeMinutes: 95}})
	if err != nil || summary.Unmatched != 2 || len(store.matches) != 2 || store.matches[SourceUGC+"\x0010"].SourceProvider != SourceUGC || store.matches[SourceKinepolis+"\x0010"].SourceProvider != SourceKinepolis {
		t.Fatalf("summary=%+v matches=%v err=%v", summary, store.matches, err)
	}
}
