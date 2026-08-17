package enrichment

import (
	"context"
	"errors"
	"testing"
	"time"

	"movieflow/api/internal/tmdb"
)

type reviewStoreStub struct {
	items           []PendingMatch
	candidate       Candidate
	candidateErr    error
	sourceRuntime   int
	reviewedID      int64
	approved        Metadata
	approvedID      int64
	fallbackRuntime int
	approvedAt      time.Time
	rejectedAt      time.Time
}

func (s *reviewStoreStub) PendingMatches(context.Context, int, int) ([]PendingMatch, error) {
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
		providerRuntime int
		wantRuntime     int
		wantFallback    int
	}{
		{name: "missing provider runtime", providerRuntime: 0, wantRuntime: 98, wantFallback: 98},
		{name: "positive provider runtime", providerRuntime: 101, wantRuntime: 101},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &reviewStoreStub{candidate: Candidate{ID: 42, Title: "Film"}, sourceRuntime: 98}
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
		{name: "unmatched rejection", status: StatusUnmatched, title: "Film", runtime: 100, raw: raw, candidateID: 0},
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
	}}
	provider := &reviewProviderStub{}
	items, err := NewReviewService(store, provider, nil).Pending(context.Background(), 20, 0)
	if err != nil || provider.calls != 0 {
		t.Fatalf("items=%+v calls=%d err=%v", items, provider.calls, err)
	}
	if items[0].SourcePosterURL != "https://static.ugc.fr/posters/200.jpg" || items[0].SourceDetailURL != "https://www.ugc.fr/film.html?id=200" || items[0].Candidates[0].PosterURL != "https://image.tmdb.org/t/p/w500/poster.jpg" || items[0].Candidates[0].DetailURL != "https://www.themoviedb.org/movie/42?language=fr-FR" {
		t.Fatalf("decorated item=%+v", items[0])
	}
	if items[1].SourcePosterURL != "" || items[1].Candidates[0].PosterURL != "" {
		t.Fatalf("unsafe posters retained: %+v", items[1])
	}
	if items[0].Status != StatusReviewRequired || items[1].Status != StatusUnmatched {
		t.Fatalf("statuses changed: %+v", items)
	}
}
