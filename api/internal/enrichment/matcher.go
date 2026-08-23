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
	ConfirmedMatches(context.Context, string, string, int, int) ([]ReusableMetadataMatch, error)
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
	return m.run(ctx, movies, false)
}

// ForceRun retries unresolved movies regardless of their retry-after time. All
// other matcher safeguards, including sticky rejections, remain unchanged.
func (m *Matcher) ForceRun(ctx context.Context, movies []Movie) (Summary, error) {
	return m.run(ctx, movies, true)
}

func (m *Matcher) run(ctx context.Context, movies []Movie, force bool) (Summary, error) {
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
		stop, err := m.process(ctx, unique[id], &summary, force)
		if err != nil {
			summary.Failed++
			if stop {
				return summary, err
			}
			var retryable *retryableProviderError
			if force && !errors.As(err, &retryable) {
				return summary, err
			}
		}
	}
	return summary, nil
}

func (m *Matcher) process(ctx context.Context, movie Movie, summary *Summary, force bool) (bool, error) {
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
	sameFingerprint := found && existing.NormalizedSourceTitle == normalizedTitle && existing.SourceRuntimeMinutes == movie.RuntimeMinutes
	if sameFingerprint {
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
			metadata := metadataFromDetails(details, movie.RuntimeMinutes, now)
			existing.EvaluatedAt, existing.RetryAfter = now, now.Add(metadataTTL)
			if err := m.store.Publish(ctx, existing, metadata); err != nil {
				return false, err
			}
			summary.Matched++
			return false, nil
		}
	}
	base := Match{SourceProvider: provider, SourceMovieID: movie.ProviderID, MetadataProvider: ProviderTMDB, NormalizedSourceTitle: normalizedTitle, SourceRuntimeMinutes: movie.RuntimeMinutes, EvaluatedAt: now, RetryAfter: now.Add(decisionTTL), Candidates: []Candidate{}}
	reusable, err := m.reusableMatch(ctx, movie, provider)
	if err != nil {
		return false, err
	}
	if reusable != nil {
		return m.publishReusable(ctx, base, *reusable, movie.RuntimeMinutes, now, summary)
	}
	if !force && sameFingerprint && now.Before(existing.RetryAfter) {
		summary.Reused++
		return false, nil
	}

	queries, exactTitles := searchQueries(movie.Title, provider)
	collected := make([]tmdb.Candidate, 0, len(queries)*20)
	seen := make(map[int64]struct{}, len(queries)*20)
	for _, query := range queries {
		candidates, searchErr := m.provider.Search(ctx, query)
		for _, candidate := range candidates {
			if candidate.ID <= 0 {
				continue
			}
			if _, duplicate := seen[candidate.ID]; duplicate {
				continue
			}
			seen[candidate.ID] = struct{}{}
			collected = append(collected, candidate)
		}
		if searchErr != nil {
			if errors.Is(searchErr, tmdb.ErrStop) {
				return true, searchErr
			}
			base.Candidates = finalizeCandidates(candidateRecords(collected))
			return false, m.saveRetryableFailure(ctx, base, searchErr)
		}
	}
	if len(collected) == 0 {
		base.Status = StatusUnmatched
		base.RetryAfter = unmatchedRetryAfter(now, movie.FirstShowingAt)
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
	records := candidateRecords(collected)
	scores := make([]scored, 0, len(collected))
	for index, candidate := range collected {
		stored := records[index]
		if !candidateTitleMatches(candidate.Title, exactTitles) {
			if !candidateTitleMatches(candidate.OriginalTitle, exactTitles) {
				continue
			}
		}
		details, detailErr := m.provider.Details(ctx, candidate.ID)
		if detailErr != nil {
			if errors.Is(detailErr, tmdb.ErrStop) {
				return true, detailErr
			}
			base.Candidates = finalizeCandidates(records)
			return false, m.saveRetryableFailure(ctx, base, detailErr)
		}
		if details.Runtime == 0 {
			stored.Score = .90
			records[index] = stored
			scores = append(scores, scored{candidate: stored, details: details})
			continue
		}
		difference := math.Abs(float64(movie.RuntimeMinutes - details.Runtime))
		score := 0.90 + 0.10*math.Max(0, 1-difference/10)
		stored.Runtime, stored.Score = details.Runtime, score
		records[index] = stored
		scores = append(scores, scored{candidate: stored, details: details})
	}
	if len(scores) == 1 && scores[0].details.Runtime == 0 {
		scores[0].candidate.Score = .95
		for index := range records {
			if records[index].ID == scores[0].candidate.ID {
				records[index].Score = .95
				break
			}
		}
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].candidate.Score != scores[j].candidate.Score {
			return scores[i].candidate.Score > scores[j].candidate.Score
		}
		return scores[i].candidate.ID < scores[j].candidate.ID
	})
	base.Candidates = finalizeCandidates(records)
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
	if err := m.store.Publish(ctx, base, metadataFromDetails(scores[0].details, movie.RuntimeMinutes, now)); err != nil {
		return false, err
	}
	summary.Matched++
	return false, nil
}

func (m *Matcher) reusableMatch(ctx context.Context, movie Movie, provider string) (*ReusableMetadataMatch, error) {
	matches, err := m.store.ConfirmedMatches(ctx, provider, ProviderTMDB, max(1, movie.RuntimeMinutes-2), movie.RuntimeMinutes+2)
	if err != nil {
		return nil, err
	}
	canonical := CanonicalTitle(provider, movie.Title)
	byID := make(map[int64]ReusableMetadataMatch)
	for _, match := range matches {
		if match.MetadataMovieID <= 0 || abs(match.SourceRuntimeMinutes-movie.RuntimeMinutes) > 2 || CanonicalTitle(match.SourceProvider, match.NormalizedSourceTitle) != canonical {
			continue
		}
		if current, found := byID[match.MetadataMovieID]; !found || match.Score > current.Score {
			byID[match.MetadataMovieID] = match
		}
	}
	if len(byID) != 1 {
		return nil, nil
	}
	for _, match := range byID {
		return &match, nil
	}
	return nil, nil
}

func (m *Matcher) publishReusable(ctx context.Context, base Match, reusable ReusableMetadataMatch, sourceRuntime int, now time.Time, summary *Summary) (bool, error) {
	metadata, found, err := m.store.Metadata(ctx, ProviderTMDB, reusable.MetadataMovieID, LocaleFrench)
	if err != nil {
		return false, err
	}
	var candidate Candidate
	if found && now.Before(metadata.RefreshAfter) {
		candidate = Candidate{ID: reusable.MetadataMovieID, Title: metadata.LocalizedTitle, OriginalTitle: metadata.ProviderTitle, Runtime: metadata.RuntimeMinutes, Score: reusable.Score, PosterURL: metadata.PosterURL}
	} else {
		details, detailErr := m.provider.Details(ctx, reusable.MetadataMovieID)
		if detailErr != nil {
			return errors.Is(detailErr, tmdb.ErrStop), detailErr
		}
		metadata = metadataFromDetails(details, sourceRuntime, now)
		candidate = Candidate{ID: reusable.MetadataMovieID, Title: details.Title, OriginalTitle: details.OriginalTitle, Runtime: details.Runtime, Score: reusable.Score, PosterURL: details.PosterURL}
	}
	base.Status, base.MetadataMovieID, base.Score, base.RetryAfter = StatusMatched, reusable.MetadataMovieID, reusable.Score, now.Add(metadataTTL)
	base.Candidates = []Candidate{candidate}
	if err := m.store.Publish(ctx, base, metadata); err != nil {
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
	return &retryableProviderError{cause: cause}
}

type retryableProviderError struct {
	cause error
}

func (e *retryableProviderError) Error() string { return e.cause.Error() }
func (e *retryableProviderError) Unwrap() error { return e.cause }

func metadataFromDetails(details tmdb.Details, sourceRuntime int, now time.Time) Metadata {
	runtime := details.Runtime
	if runtime == 0 {
		runtime = sourceRuntime
	}
	genres := make([]string, len(details.Genres))
	copy(genres, details.Genres)
	return Metadata{Provider: ProviderTMDB, ProviderMovieID: details.ID, Locale: LocaleFrench, ProviderTitle: details.OriginalTitle, LocalizedTitle: details.Title, Overview: details.Overview, ReleaseDate: details.ReleaseDate, PosterURL: details.PosterURL, BackdropURL: details.BackdropURL, RuntimeMinutes: runtime, Genres: genres, FetchedAt: now, RefreshAfter: now.Add(metadataTTL)}
}

func candidateRecords(candidates []tmdb.Candidate) []Candidate {
	records := make([]Candidate, len(candidates))
	for index, candidate := range candidates {
		records[index] = Candidate{ID: candidate.ID, Title: candidate.Title, OriginalTitle: candidate.OriginalTitle, PosterURL: candidate.PosterURL}
	}
	return records
}

func finalizeCandidates(candidates []Candidate) []Candidate {
	result := append([]Candidate(nil), candidates...)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].ID < result[j].ID
	})
	if len(result) > 5 {
		result = result[:5]
	}
	return result
}

func searchQueries(rawTitle, provider string) ([]string, map[string]struct{}) {
	rawNormalized := NormalizeTitle(rawTitle)
	canonical := CanonicalTitle(provider, rawTitle)
	queries := []string{rawTitle}
	if canonical != "" && canonical != rawNormalized {
		queries = append(queries, canonical)
	}
	exact := map[string]struct{}{rawNormalized: {}}
	if canonical != "" {
		exact[canonical] = struct{}{}
	}
	return queries, exact
}

func CanonicalTitle(provider, value string) string {
	title := NormalizeTitle(value)
	for {
		previous := title
		switch provider {
		case SourceKinepolis:
			title = trimKinepolisPrefix(title)
		case SourceUGC:
			title = trimUGCSuffix(title)
		}
		title = trimEditionMarker(title)
		if title == previous {
			return title
		}
	}
}

var kinepolisPrefixes = []string{
	"visite d equipe", "matinee magique", "seance speciale", "avant premiere", "lumiere sur", "ap cine cool", "cine concert", "cine debat", "cine relax", "les classiques", "masterclass", "kultissime", "manga k",
}

func trimKinepolisPrefix(title string) string {
	for _, prefix := range kinepolisPrefixes {
		if remainder, ok := trimLeadingTokens(title, prefix); ok {
			return remainder
		}
	}
	parts := strings.Fields(title)
	if len(parts) > 3 && parts[0] == "saison" && twoDigits(parts[1]) && twoDigits(parts[2]) {
		return strings.Join(parts[3:], " ")
	}
	if len(parts) > 3 && parts[0] == "comedie" && parts[1] == "francaise" && fourDigits(parts[2]) {
		return strings.Join(parts[3:], " ")
	}
	return title
}

func trimUGCSuffix(title string) string {
	for _, suffix := range []string{"the royal opera", "the royal ballet", "opera de paris", "comedie francaise"} {
		if title != suffix && strings.HasSuffix(title, " "+suffix) {
			return strings.TrimSuffix(title, " "+suffix)
		}
	}
	return title
}

func trimLeadingTokens(title, prefix string) (string, bool) {
	if title == prefix || !strings.HasPrefix(title, prefix+" ") {
		return title, false
	}
	return strings.TrimPrefix(title, prefix+" "), true
}

func trimEditionMarker(title string) string {
	for _, marker := range []string{"40th anniversary", "rediffusion"} {
		if remainder, ok := trimLeadingTokens(title, marker); ok {
			return remainder
		}
		if title != marker && strings.HasSuffix(title, " "+marker) {
			return strings.TrimSuffix(title, " "+marker)
		}
	}
	return title
}

func twoDigits(value string) bool {
	return len(value) == 2 && value[0] >= '0' && value[0] <= '9' && value[1] >= '0' && value[1] <= '9'
}

func fourDigits(value string) bool {
	return len(value) == 4 && twoDigits(value[:2]) && twoDigits(value[2:])
}

func candidateTitleMatches(value string, exactTitles map[string]struct{}) bool {
	if _, exact := exactTitles[NormalizeTitle(value)]; exact {
		return true
	}
	_, exact := exactTitles[canonicalCandidateTitle(value)]
	return exact
}

func canonicalCandidateTitle(value string) string {
	title := strings.TrimSpace(value)
	for {
		previous := title
		title = trimRoyalBalletOperaPrefix(title)
		title = trimCandidateSuffix(title, "(Opéra Bastille)")
		title = trimCandidateSuffix(title, "[Opéra National de Paris]")
		if title == previous {
			return NormalizeTitle(title)
		}
	}
}

func trimRoyalBalletOperaPrefix(title string) string {
	colon := strings.IndexByte(title, ':')
	if colon < 0 {
		return title
	}
	prefix := strings.Fields(strings.TrimSpace(title[:colon]))
	if len(prefix) != 5 || !strings.EqualFold(prefix[0], "Royal") || !strings.EqualFold(prefix[1], "Ballet") || prefix[2] != "&" || !strings.EqualFold(prefix[3], "Opera") || !shortSeason(prefix[4]) {
		return title
	}
	remainder := strings.TrimSpace(title[colon+1:])
	if remainder == "" {
		return title
	}
	return remainder
}

func trimCandidateSuffix(title, suffix string) string {
	if len(title) <= len(suffix) || !strings.EqualFold(title[len(title)-len(suffix):], suffix) {
		return title
	}
	remainder := strings.TrimSpace(title[:len(title)-len(suffix)])
	if remainder == "" {
		return title
	}
	return remainder
}

func shortSeason(value string) bool {
	return len(value) == 7 && fourDigits(value[:4]) && value[4] == '/' && twoDigits(value[5:])
}

func unmatchedRetryAfter(now, firstShowing time.Time) time.Time {
	if firstShowing.IsZero() {
		return now.Add(decisionTTL)
	}
	location := firstShowing.Location()
	localNow := now.In(location)
	nowYear, nowMonth, nowDay := localNow.Date()
	showingYear, showingMonth, showingDay := firstShowing.Date()
	nowDate := time.Date(nowYear, nowMonth, nowDay, 0, 0, 0, 0, location)
	showingDate := time.Date(showingYear, showingMonth, showingDay, 0, 0, 0, 0, location)
	if showingDate.After(nowDate) {
		return now.Add(24 * time.Hour)
	}
	return now.Add(decisionTTL)
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
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
