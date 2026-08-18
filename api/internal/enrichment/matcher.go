package enrichment

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"messeances/api/internal/tmdb"
)

const (
	metadataTTL = 30 * 24 * time.Hour
	decisionTTL = 7 * 24 * time.Hour
)

type Store interface {
	IsLocallyMerged(context.Context, string, string) (bool, error)
	Match(context.Context, string, string, string) (Match, bool, error)
	Metadata(context.Context, string, int64, string) (Metadata, bool, error)
	SaveDecision(context.Context, Match) error
	Publish(context.Context, Match, Metadata) error
}

type Provider interface {
	Search(context.Context, string) ([]tmdb.Candidate, error)
	Details(context.Context, int64) (tmdb.Details, error)
}

type Summary struct{ Reused, Matched, ReviewRequired, Unmatched, Failed int }

type Matcher struct {
	store    Store
	provider Provider
	now      func() time.Time
}

func NewMatcher(store Store, provider Provider, now func() time.Time) *Matcher {
	if now == nil {
		now = time.Now
	}
	return &Matcher{store: store, provider: provider, now: now}
}

func (m *Matcher) Run(ctx context.Context, movies []Movie) (Summary, error) {
	var summary Summary
	unique := map[string]Movie{}
	for _, movie := range movies {
		if movie.SourceProvider == "" {
			movie.SourceProvider = SourceUGC
		}
		key := movie.SourceProvider + "\x00" + movie.ProviderID
		if _, exists := unique[key]; !exists {
			unique[key] = movie
		}
	}
	ids := make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		stop, err := m.process(ctx, unique[id], &summary)
		if err != nil {
			summary.Failed++
			if stop {
				return summary, err
			}
		}
	}
	return summary, nil
}

func (m *Matcher) process(ctx context.Context, movie Movie, summary *Summary) (bool, error) {
	now := m.now().UTC()
	normalizedTitle := NormalizeTitle(movie.Title)
	provider := movie.SourceProvider
	if provider == "" {
		provider = SourceUGC
	}
	merged, err := m.store.IsLocallyMerged(ctx, provider, movie.ProviderID)
	if err != nil {
		return false, err
	}
	if merged {
		summary.Reused++
		return false, nil
	}
	existing, found, err := m.store.Match(ctx, provider, movie.ProviderID, ProviderTMDB)
	if err != nil {
		return false, err
	}
	if found && existing.NormalizedSourceTitle == normalizedTitle && existing.SourceRuntimeMinutes == movie.RuntimeMinutes {
		if existing.Status == StatusRejected {
			summary.Reused++
			return false, nil
		}
		if existing.Status == StatusMatched {
			cached, cachedFound, err := m.store.Metadata(ctx, ProviderTMDB, existing.MetadataMovieID, LocaleFrench)
			if err != nil {
				return false, err
			}
			if cachedFound && now.Before(cached.RefreshAfter) {
				summary.Reused++
				return false, nil
			}
			details, err := m.provider.Details(ctx, existing.MetadataMovieID)
			if err != nil {
				return errors.Is(err, tmdb.ErrStop), err
			}
			metadata := metadataFromDetails(details, now)
			existing.EvaluatedAt, existing.RetryAfter = now, now.Add(metadataTTL)
			if err := m.store.Publish(ctx, existing, metadata); err != nil {
				return false, err
			}
			summary.Matched++
			return false, nil
		}
		if now.Before(existing.RetryAfter) {
			summary.Reused++
			return false, nil
		}
	}
	base := Match{SourceProvider: provider, SourceMovieID: movie.ProviderID, MetadataProvider: ProviderTMDB, NormalizedSourceTitle: normalizedTitle, SourceRuntimeMinutes: movie.RuntimeMinutes, EvaluatedAt: now, RetryAfter: now.Add(decisionTTL), Candidates: []Candidate{}}
	candidates, err := m.provider.Search(ctx, movie.Title)
	if err != nil {
		if errors.Is(err, tmdb.ErrStop) {
			return true, err
		}
		return false, m.saveRetryableFailure(ctx, base, err)
	}
	if len(candidates) == 0 {
		base.Status = StatusUnmatched
		if err := m.store.SaveDecision(ctx, base); err != nil {
			return false, err
		}
		summary.Unmatched++
		return false, nil
	}
	type scored struct {
		candidate Candidate
		details   tmdb.Details
	}
	scores := []scored{}
	for index, candidate := range candidates {
		if index == 5 {
			break
		}
		stored := Candidate{ID: candidate.ID, Title: candidate.Title, OriginalTitle: candidate.OriginalTitle, PosterURL: candidate.PosterURL}
		base.Candidates = append(base.Candidates, stored)
		if NormalizeTitle(candidate.Title) != normalizedTitle && NormalizeTitle(candidate.OriginalTitle) != normalizedTitle {
			continue
		}
		details, err := m.provider.Details(ctx, candidate.ID)
		if err != nil {
			if errors.Is(err, tmdb.ErrStop) {
				return true, err
			}
			return false, m.saveRetryableFailure(ctx, base, err)
		}
		if details.Runtime == 0 {
			base.Candidates[len(base.Candidates)-1] = stored
			continue
		}
		difference := math.Abs(float64(movie.RuntimeMinutes - details.Runtime))
		score := 0.90 + 0.10*math.Max(0, 1-difference/10)
		stored.Runtime, stored.Score = details.Runtime, score
		base.Candidates[len(base.Candidates)-1] = stored
		scores = append(scores, scored{stored, details})
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].candidate.Score != scores[j].candidate.Score {
			return scores[i].candidate.Score > scores[j].candidate.Score
		}
		return scores[i].candidate.ID < scores[j].candidate.ID
	})
	accepted := len(scores) > 0 && scores[0].candidate.Score+1e-9 >= .95 && (len(scores) == 1 || scores[0].candidate.Score-scores[1].candidate.Score+1e-9 >= .05)
	if !accepted {
		base.Status = StatusReviewRequired
		if err := m.store.SaveDecision(ctx, base); err != nil {
			return false, err
		}
		summary.ReviewRequired++
		return false, nil
	}
	base.Status, base.MetadataMovieID, base.Score, base.RetryAfter = StatusMatched, scores[0].candidate.ID, scores[0].candidate.Score, now.Add(metadataTTL)
	if err := m.store.Publish(ctx, base, metadataFromDetails(scores[0].details, now)); err != nil {
		return false, err
	}
	summary.Matched++
	return false, nil
}

func (m *Matcher) saveRetryableFailure(ctx context.Context, match Match, cause error) error {
	match.Status = StatusReviewRequired
	match.RetryAfter = match.EvaluatedAt
	if err := m.store.SaveDecision(ctx, match); err != nil {
		return err
	}
	return cause
}

func metadataFromDetails(details tmdb.Details, now time.Time) Metadata {
	return Metadata{Provider: ProviderTMDB, ProviderMovieID: details.ID, Locale: LocaleFrench, ProviderTitle: details.OriginalTitle, LocalizedTitle: details.Title, Overview: details.Overview, ReleaseDate: details.ReleaseDate, PosterURL: details.PosterURL, BackdropURL: details.BackdropURL, RuntimeMinutes: details.Runtime, Genres: append([]string(nil), details.Genres...), FetchedAt: now, RefreshAfter: now.Add(metadataTTL)}
}

func NormalizeTitle(value string) string {
	decomposed := norm.NFKD.String(strings.ToLower(value))
	var builder strings.Builder
	space := true
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			space = false
			continue
		}
		if !space {
			builder.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(builder.String())
}
