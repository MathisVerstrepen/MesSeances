package enrichment

import (
	"context"
	"errors"
	"testing"
	"time"

	"messeances/api/internal/tmdb"
)

type reviewStoreStub struct {
	items           []PendingMatch
	filter          PendingMatchFilter
	search          string
	limit           int
	offset          int
	candidate       Candidate
	candidateErr    error
	sourceRuntime   int
	reviewedID      int64
	approved        Metadata
	approvedID      int64
	fallbackRuntime int
	approvedAt      time.Time
	rejectedAt      time.Time
	correctionID    int64
	correctionToken time.Time
	correctionErr   error
	corrected       Metadata
	correctedID     int64
	correctedToken  time.Time
	correctedAt     time.Time
	correctedError  error
}

func (s *reviewStoreStub) PendingMatches(_ context.Context, filter PendingMatchFilter, search string, limit, offset int) ([]PendingMatch, error) {
	s.filter, s.search, s.limit, s.offset = filter, search, limit, offset
	return s.items, nil
}
func (s *reviewStoreStub) ReviewCandidate(_ context.Context, _, _ string, candidateID int64) (Candidate, int, error) {
	s.reviewedID = candidateID
	return s.candidate, s.sourceRuntime, s.candidateErr
}
func (s *reviewStoreStub) ApproveReview(_ context.Context, _, _ string, candidateID int64, metadata Metadata, fallbackRuntime int, now time.Time) error {
	s.approvedID, s.approved, s.fallbackRuntime, s.approvedAt = candidateID, metadata, fallbackRuntime, now
	return nil
}
func (s *reviewStoreStub) RejectReview(_ context.Context, _, _ string, now time.Time) error {
	s.rejectedAt = now
	return nil
}
func (s *reviewStoreStub) CorrectionSource(_ context.Context, _, _ string, replacementID int64, expectedUpdatedAt time.Time) (int, error) {
	s.correctionID, s.correctionToken = replacementID, expectedUpdatedAt
	return s.sourceRuntime, s.correctionErr
}
func (s *reviewStoreStub) CorrectReview(_ context.Context, _, _ string, replacementID int64, expectedUpdatedAt time.Time, metadata Metadata, fallbackRuntime int, now time.Time) error {
	s.correctedID, s.correctedToken, s.corrected, s.fallbackRuntime, s.correctedAt = replacementID, expectedUpdatedAt, metadata, fallbackRuntime, now
	return s.correctedError
}

type reviewProviderStub struct {
	details tmdb.Details
	err     error
	calls   int
	lastID  int64
}

func (p *reviewProviderStub) Details(_ context.Context, candidateID int64) (tmdb.Details, error) {
	p.calls++
	p.lastID = candidateID
	return p.details, p.err
}

func TestReviewServiceApprovesStoredCandidateUsingProviderDetails(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	store := &reviewStoreStub{candidate: Candidate{ID: 42, Title: "Film", Score: .91}, sourceRuntime: 98}
	provider := &reviewProviderStub{details: tmdb.Details{ID: 42, Title: "Film FR", OriginalTitle: "Film", BackdropURL: "https://image.tmdb.org/t/p/w780/42.jpg", Runtime: 101, Genres: []string{"Drame"}}}
	err := NewReviewService(store, provider, func() time.Time { return now }).Approve(context.Background(), SourceUGC, "200", 42)
	if err != nil || provider.calls != 1 || provider.lastID != 42 || store.reviewedID != 42 || store.approvedID != 42 || store.approved.ProviderMovieID != 42 || store.approved.LocalizedTitle != "Film FR" || store.approved.BackdropURL != provider.details.BackdropURL || !store.approvedAt.Equal(now) || !store.approved.RefreshAfter.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("approved=%+v at=%s calls=%d err=%v", store.approved, store.approvedAt, provider.calls, err)
	}
}

func TestReviewServiceApprovesManualCandidateUsingProviderDetails(t *testing.T) {
	store := &reviewStoreStub{candidate: Candidate{ID: 999, Score: 1}, sourceRuntime: 98}
	provider := &reviewProviderStub{details: tmdb.Details{ID: 999, Title: "Film manuel", Runtime: 101, Genres: []string{}}}
	err := NewReviewService(store, provider, nil).Approve(context.Background(), SourceUGC, "200", 999)
	if err != nil || provider.calls != 1 || provider.lastID != 999 || store.reviewedID != 999 || store.approvedID != 999 || store.approved.ProviderMovieID != 999 {
		t.Fatalf("reviewed=%d approved=%d metadata=%+v calls=%d providerID=%d err=%v", store.reviewedID, store.approvedID, store.approved, provider.calls, provider.lastID, err)
	}
}

func TestReviewServiceDoesNotFetchWhenPreflightConflicts(t *testing.T) {
	store := &reviewStoreStub{candidateErr: ErrReviewConflict}
	provider := &reviewProviderStub{}
	err := NewReviewService(store, provider, nil).Approve(context.Background(), SourceUGC, "200", 999)
	if !errors.Is(err, ErrReviewConflict) || provider.calls != 0 {
		t.Fatalf("calls=%d err=%v", provider.calls, err)
	}
}

func TestReviewServiceUsesSourceRuntimeOnlyWhenProviderRuntimeMissing(t *testing.T) {
	tests := []struct {
		name            string
		sourceRuntime   int
		providerRuntime int
		wantRuntime     int
		wantFallback    int
	}{
		{name: "missing provider runtime", sourceRuntime: 98, providerRuntime: 0, wantRuntime: 98, wantFallback: 98},
		{name: "both runtimes unknown", sourceRuntime: 0, providerRuntime: 0, wantRuntime: 0, wantFallback: 0},
		{name: "positive provider runtime", sourceRuntime: 98, providerRuntime: 101, wantRuntime: 101},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &reviewStoreStub{candidate: Candidate{ID: 42, Title: "Film"}, sourceRuntime: test.sourceRuntime}
			provider := &reviewProviderStub{details: tmdb.Details{ID: 42, Title: "Film", OriginalTitle: "Film", Runtime: test.providerRuntime, Genres: []string{}}}
			err := NewReviewService(store, provider, func() time.Time { return matcherNow }).Approve(context.Background(), SourceKinepolis, "HO00016258", 42)
			if err != nil || store.approved.RuntimeMinutes != test.wantRuntime || store.fallbackRuntime != test.wantFallback {
				t.Fatalf("approved=%+v err=%v", store.approved, err)
			}
		})
	}
}

func TestReviewServiceDoesNotStoreProviderFailureOrMismatchedID(t *testing.T) {
	tests := []struct {
		name     string
		provider *reviewProviderStub
	}{
		{name: "provider failure", provider: &reviewProviderStub{err: errors.New("provider failed")}},
		{name: "mismatched ID", provider: &reviewProviderStub{details: tmdb.Details{ID: 43, Title: "Wrong", Runtime: 100}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &reviewStoreStub{candidate: Candidate{ID: 42, Score: .9}}
			err := NewReviewService(store, test.provider, nil).Approve(context.Background(), SourceUGC, "200", 42)
			if err == nil || test.provider.calls != 1 || store.approvedID != 0 || store.approved.ProviderMovieID != 0 {
				t.Fatalf("approved=%d metadata=%+v calls=%d err=%v", store.approvedID, store.approved, test.provider.calls, err)
			}
		})
	}
}

func TestReviewServiceFailsClosedWithoutProvider(t *testing.T) {
	store := &reviewStoreStub{candidate: Candidate{ID: 42, Score: .9}}
	err := NewReviewService(store, nil, nil).Approve(context.Background(), SourceUGC, "200", 42)
	if !errors.Is(err, ErrReviewUnavailable) || store.reviewedID != 0 || store.approvedID != 0 {
		t.Fatalf("reviewed=%d approved=%d err=%v", store.reviewedID, store.approvedID, err)
	}
}

func TestReviewServiceCorrectsMatchedIdentityUsingFreshMetadata(t *testing.T) {
	now := time.Date(2026, 8, 16, 13, 0, 0, 123456789, time.UTC)
	expected := now.Add(-time.Hour)
	store := &reviewStoreStub{sourceRuntime: 98}
	provider := &reviewProviderStub{details: tmdb.Details{ID: 99, Title: "Film corrigé", OriginalTitle: "Corrected Film", Runtime: 101, PosterURL: "https://image.tmdb.org/t/p/w500/99.jpg", Genres: []string{"Drame"}}}
	err := NewReviewService(store, provider, func() time.Time { return now }).Correct(context.Background(), SourceUGC, "200", 99, expected)
	if err != nil || provider.calls != 1 || provider.lastID != 99 || store.correctionID != 99 || !store.correctionToken.Equal(expected) || store.correctedID != 99 || !store.correctedToken.Equal(expected) || store.corrected.LocalizedTitle != "Film corrigé" || store.corrected.ProviderTitle != "Corrected Film" || store.corrected.RuntimeMinutes != 101 || store.fallbackRuntime != 0 || !store.correctedAt.Equal(now) || !store.corrected.RefreshAfter.Equal(now.Add(reviewMetadataTTL)) {
		t.Fatalf("corrected=%+v preflight=%d/%s stored=%d/%s calls=%d err=%v", store.corrected, store.correctionID, store.correctionToken, store.correctedID, store.correctedToken, provider.calls, err)
	}
}

func TestReviewServiceCorrectionUsesRuntimeFallback(t *testing.T) {
	now := time.Date(2026, 8, 16, 13, 0, 0, 0, time.UTC)
	store := &reviewStoreStub{sourceRuntime: 98}
	provider := &reviewProviderStub{details: tmdb.Details{ID: 99, Title: "Film corrigé", OriginalTitle: "Corrected Film", Genres: []string{}}}
	err := NewReviewService(store, provider, func() time.Time { return now }).Correct(context.Background(), SourceUGC, "200", 99, now.Add(-time.Hour))
	if err != nil || store.corrected.RuntimeMinutes != 98 || store.fallbackRuntime != 98 {
		t.Fatalf("corrected=%+v fallback=%d err=%v", store.corrected, store.fallbackRuntime, err)
	}
}

func TestReviewServiceCorrectionStopsBeforeProviderOnConflict(t *testing.T) {
	store := &reviewStoreStub{correctionErr: ErrReviewConflict}
	provider := &reviewProviderStub{}
	err := NewReviewService(store, provider, nil).Correct(context.Background(), SourceUGC, "200", 99, time.Now())
	if !errors.Is(err, ErrReviewConflict) || provider.calls != 0 || store.correctedID != 0 {
		t.Fatalf("calls=%d corrected=%d err=%v", provider.calls, store.correctedID, err)
	}
}

func TestReviewServiceCorrectionFailsClosedAndRejectsProviderMismatch(t *testing.T) {
	expected := time.Now()
	store := &reviewStoreStub{}
	if err := NewReviewService(store, nil, nil).Correct(context.Background(), SourceUGC, "200", 99, expected); !errors.Is(err, ErrReviewUnavailable) || store.correctionID != 0 {
		t.Fatalf("nil provider preflight=%d err=%v", store.correctionID, err)
	}
	provider := &reviewProviderStub{details: tmdb.Details{ID: 100, Title: "Wrong", OriginalTitle: "Wrong"}}
	err := NewReviewService(store, provider, nil).Correct(context.Background(), SourceUGC, "200", 99, expected)
	if err == nil || provider.calls != 1 || store.correctedID != 0 {
		t.Fatalf("calls=%d corrected=%d err=%v", provider.calls, store.correctedID, err)
	}
}

func TestValidReviewCandidateAssignmentAndRejectionRules(t *testing.T) {
	raw := []byte(`[{"id":42,"title":"Film","score":0.91}]`)
	tests := []struct {
		name        string
		status      string
		title       string
		runtime     int
		raw         []byte
		candidateID int64
		wantOK      bool
		wantScore   float64
	}{
		{name: "stored review candidate", status: StatusReviewRequired, title: "Film", runtime: 100, raw: raw, candidateID: 42, wantOK: true, wantScore: .91},
		{name: "manual review candidate", status: StatusReviewRequired, title: "Film", runtime: 100, raw: raw, candidateID: 99, wantOK: true, wantScore: 1},
		{name: "stored unmatched candidate", status: StatusUnmatched, title: "Film", runtime: 100, raw: raw, candidateID: 42, wantOK: true, wantScore: .91},
		{name: "manual unmatched candidate", status: StatusUnmatched, title: "Film", runtime: 100, raw: raw, candidateID: 99, wantOK: true, wantScore: 1},
		{name: "review rejection", status: StatusReviewRequired, title: "Film", runtime: 100, raw: raw, candidateID: 0, wantOK: true},
		{name: "unmatched rejection", status: StatusUnmatched, title: "Film", runtime: 100, raw: raw, candidateID: 0, wantOK: true},
		{name: "matched", status: StatusMatched, title: "Film", runtime: 100, raw: raw, candidateID: 99},
		{name: "rejected", status: StatusRejected, title: "Film", runtime: 100, raw: raw, candidateID: 99},
		{name: "changed title", status: StatusReviewRequired, title: "Autre", runtime: 100, raw: raw, candidateID: 99},
		{name: "changed runtime", status: StatusReviewRequired, title: "Film", runtime: 99, raw: raw, candidateID: 99},
		{name: "malformed candidates", status: StatusReviewRequired, title: "Film", runtime: 100, raw: []byte(`{`), candidateID: 99},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate, ok := validReviewCandidate(test.status, NormalizeTitle("Film"), 100, test.raw, test.title, test.runtime, test.candidateID)
			if ok != test.wantOK || candidate.Score != test.wantScore {
				t.Fatalf("candidate=%+v ok=%v", candidate, ok)
			}
		})
	}
	if candidate, ok := validReviewCandidate(StatusReviewRequired, NormalizeTitle("Film"), 0, raw, "Film", 0, 42); !ok || candidate.Score != .91 {
		t.Fatalf("zero-runtime candidate=%+v ok=%v", candidate, ok)
	}
	if candidate, ok := validReviewCandidate(StatusReviewRequired, NormalizeTitle("Film"), -1, raw, "Film", -1, 42); ok {
		t.Fatalf("negative-runtime candidate=%+v accepted", candidate)
	}
}

func TestReviewServiceDecoratesAndSanitizesPendingWithoutProviderCalls(t *testing.T) {
	store := &reviewStoreStub{items: []PendingMatch{
		{
			SourceProvider: SourceUGC, SourceMovieID: "200", SourceTitle: "Film", SourcePosterURL: "https://static.ugc.fr/posters/200.jpg", SourceDetailURL: "https://evil.example/source", Status: StatusReviewRequired,
			Candidates: []Candidate{{ID: 42, Title: "Film", PosterURL: "https://image.tmdb.org/t/p/w500/poster.jpg", DetailURL: "https://evil.example/candidate"}},
		},
		{
			SourceProvider: SourceUGC, SourceMovieID: "201", SourceTitle: "Autre", SourcePosterURL: "https://evil.example/poster.jpg", Status: StatusUnmatched,
			Candidates: []Candidate{{ID: 43, Title: "Autre", PosterURL: "http://image.tmdb.org/t/p/w500/poster.jpg"}},
		},
		{
			SourceProvider: SourceKinepolis, SourceMovieID: "K200", SourceTitle: "Associé", Status: StatusMatched,
			CurrentMatch: &Candidate{ID: 99, Title: "Associé TMDB", PosterURL: "https://image.tmdb.org/t/p/w500/99.jpg", DetailURL: "https://evil.example/current"},
		},
	}}
	provider := &reviewProviderStub{}
	items, err := NewReviewService(store, provider, nil).Pending(context.Background(), PendingMatchFilterRejected, "", 20, 40)
	if err != nil || provider.calls != 0 {
		t.Fatalf("items=%+v calls=%d err=%v", items, provider.calls, err)
	}
	if store.filter != PendingMatchFilterRejected || store.limit != 20 || store.offset != 40 {
		t.Fatalf("pending query=%q/%d/%d", store.filter, store.limit, store.offset)
	}
	if items[0].SourcePosterURL != "https://static.ugc.fr/posters/200.jpg" || items[0].SourceDetailURL != "https://www.ugc.fr/film.html?id=200" || items[0].Candidates[0].PosterURL != "https://image.tmdb.org/t/p/w500/poster.jpg" || items[0].Candidates[0].DetailURL != "https://www.themoviedb.org/movie/42?language=fr-FR" {
		t.Fatalf("decorated item=%+v", items[0])
	}
	if items[1].SourcePosterURL != "" || items[1].Candidates[0].PosterURL != "" {
		t.Fatalf("unsafe posters retained: %+v", items[1])
	}
	if items[2].CurrentMatch == nil || items[2].CurrentMatch.DetailURL != "https://www.themoviedb.org/movie/99?language=fr-FR" || items[2].CurrentMatch.PosterURL != "https://image.tmdb.org/t/p/w500/99.jpg" {
		t.Fatalf("decorated current match=%+v", items[2].CurrentMatch)
	}
	if items[0].Status != StatusReviewRequired || items[1].Status != StatusUnmatched {
		t.Fatalf("statuses changed: %+v", items)
	}
}

func TestReviewServiceForwardsMatchedSearch(t *testing.T) {
	store := &reviewStoreStub{}
	items, err := NewReviewService(store, nil, nil).Pending(context.Background(), PendingMatchFilterMatched, "Alien", 20, 40)
	if err != nil || len(items) != 0 || store.filter != PendingMatchFilterMatched || store.search != "Alien" || store.limit != 20 || store.offset != 40 {
		t.Fatalf("items=%+v filter=%q search=%q pagination=%d/%d err=%v", items, store.filter, store.search, store.limit, store.offset, err)
	}
}

func TestExactTMDBSearchID(t *testing.T) {
	tests := []struct {
		search string
		want   int64
		ok     bool
	}{
		{search: "1", want: 1, ok: true},
		{search: "9223372036854775807", want: 9223372036854775807, ok: true},
		{search: ""},
		{search: "0"},
		{search: "00"},
		{search: "01"},
		{search: "+1"},
		{search: "-1"},
		{search: "9223372036854775808"},
		{search: "１２"},
		{search: "1a"},
	}
	for _, test := range tests {
		t.Run(test.search, func(t *testing.T) {
			got, ok := exactTMDBSearchID(test.search)
			if got != test.want || ok != test.ok {
				t.Fatalf("exactTMDBSearchID(%q)=(%d,%v) want=(%d,%v)", test.search, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestValidPatheSourcePosterURL(t *testing.T) {
	for _, raw := range []string{
		"https://www.pathe.fr/media/poster.jpg",
		"https://media.pathe.fr/posters/a.webp",
	} {
		if !validSourcePosterURL(SourcePathe, raw) {
			t.Fatalf("valid Pathé poster rejected: %q", raw)
		}
	}
	for _, raw := range []string{
		"http://www.pathe.fr/media/poster.jpg",
		"https://evil.example/media/poster.jpg",
		"https://www.pathe.fr:443/media/poster.jpg",
		"https://www.pathe.fr/media/../poster.jpg",
		"https://www.pathe.fr/media/poster.jpg?x=1",
		"https://www.pathe.fr/media/poster.jpg?",
		"https://www.pathe.fr/media/./poster.jpg",
		"https://www.pathe.fr/",
	} {
		if validSourcePosterURL(SourcePathe, raw) {
			t.Fatalf("unsafe Pathé poster accepted: %q", raw)
		}
	}
}

func TestValidCGRSourcePosterURL(t *testing.T) {
	if !validSourcePosterURL(SourceCGR, "https://images.acsta.net/posters/1001.jpg") {
		t.Fatal("valid CGR poster rejected")
	}
	for _, raw := range []string{"http://images.acsta.net/posters/1001.jpg", "https://evil.example/poster.jpg", "https://images.acsta.net/posters/../secret", "https://images.acsta.net/poster.jpg?x=1"} {
		if validSourcePosterURL(SourceCGR, raw) {
			t.Fatalf("unsafe CGR poster accepted: %q", raw)
		}
	}
}
