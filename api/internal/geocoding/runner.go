package geocoding

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
)

type RunOptions struct {
	Filters         Filters
	Limit           int
	RetryAmbiguous  bool
	PreserveMatched bool
	DryRun          bool
}

type Summary struct {
	DryRun    bool
	Selected  int
	Skipped   int
	Matched   int
	Ambiguous int
	NotFound  int
	Failed    int
	Written   int
}

type Runner struct {
	store    Store
	provider Provider
	now      func() time.Time
}

func NewRunner(store Store, provider Provider, now func() time.Time) (*Runner, error) {
	if store == nil || provider == nil {
		return nil, fmt.Errorf("invalid geocoding runner configuration")
	}
	if now == nil {
		now = time.Now
	}
	return &Runner{store: store, provider: provider, now: now}, nil
}

func (r *Runner) Run(ctx context.Context, options RunOptions) (Summary, error) {
	summary := Summary{DryRun: options.DryRun}
	if options.Limit < 0 {
		return summary, fmt.Errorf("invalid geocoding limit")
	}
	theaters, err := r.store.Select(ctx, options.Filters)
	if err != nil {
		return summary, err
	}
	for _, theater := range theaters {
		hash := AddressHash(theater.Address, theater.PostalCode, theater.City)
		if !processable(theater.Location, hash, options.RetryAmbiguous, options.PreserveMatched) {
			summary.Skipped++
			continue
		}
		if options.Limit > 0 && summary.Selected >= options.Limit {
			break
		}
		summary.Selected++
		location := Location{Provider: theater.Provider, ProviderTheaterID: theater.ProviderID, Source: SourceIGN, AddressHash: hash, UpdatedAt: r.now().UTC()}
		if strings.TrimSpace(theater.Address) == "" || strings.TrimSpace(theater.PostalCode) == "" || strings.TrimSpace(theater.City) == "" {
			location.Status = StatusNotFound
		} else {
			candidates, searchErr := r.provider.Search(ctx, Query{Address: theater.Address, PostalCode: theater.PostalCode, City: theater.City})
			if searchErr != nil {
				summary.Failed++
				continue
			}
			location, err = evaluate(theater, hash, candidates, location.UpdatedAt)
			if err != nil {
				summary.Failed++
				continue
			}
		}
		incrementStatus(&summary, location.Status)
		if options.DryRun {
			continue
		}
		written, saveErr := r.store.Save(ctx, theater.Location, location)
		if saveErr != nil {
			summary.Failed++
			continue
		}
		if written {
			summary.Written++
		} else {
			summary.Skipped++
		}
	}
	if summary.Failed > 0 {
		return summary, fmt.Errorf("one or more theater geocoding operations failed")
	}
	return summary, nil
}

func processable(location *Location, hash string, retryAmbiguous, preserveMatched bool) bool {
	if location == nil {
		return true
	}
	if location.Status == StatusManual {
		return false
	}
	if preserveMatched && location.Status == StatusMatched {
		return false
	}
	if location.AddressHash != hash {
		return true
	}
	return location.Status == StatusAmbiguous && retryAmbiguous
}

func evaluate(theater Theater, hash string, candidates []Candidate, updatedAt time.Time) (Location, error) {
	result := Location{Provider: theater.Provider, ProviderTheaterID: theater.ProviderID, Source: SourceIGN, AddressHash: hash, UpdatedAt: updatedAt}
	if len(candidates) == 0 {
		result.Status = StatusNotFound
		return result, nil
	}
	bestMatch := -1
	bestAmbiguous := -1
	for index, candidate := range candidates {
		parseable := strings.TrimSpace(candidate.Label) != "" && candidate.HasScore && !math.IsNaN(candidate.Score) && !math.IsInf(candidate.Score, 0) && candidate.Score >= 0 && candidate.Score <= 1
		if parseable && (bestAmbiguous < 0 || candidate.Score > candidates[bestAmbiguous].Score) {
			bestAmbiguous = index
		}
		validCoordinates := validCandidateCoordinates(candidate)
		acceptedType := candidate.Type == "housenumber" || candidate.Type == "street" && acceptsStreetCandidate(theater)
		accepted := parseable && validCoordinates && strings.TrimSpace(candidate.PostalCode) == strings.TrimSpace(theater.PostalCode) && cityKey(candidate.City) == cityKey(theater.City) && acceptedType && candidate.Score >= 0.70
		if accepted && (bestMatch < 0 || candidate.Score > candidates[bestMatch].Score) {
			bestMatch = index
		}
	}
	if bestMatch >= 0 {
		candidate := candidates[bestMatch]
		latitude, longitude, score := candidate.Latitude, candidate.Longitude, candidate.Score
		result.Status, result.Latitude, result.Longitude = StatusMatched, &latitude, &longitude
		result.MatchedLabel, result.MatchScore = strings.TrimSpace(candidate.Label), &score
		return result, nil
	}
	if bestAmbiguous < 0 {
		return Location{}, fmt.Errorf("geocoding response has no reviewable candidate")
	}
	candidate := candidates[bestAmbiguous]
	score := candidate.Score
	result.Status, result.MatchedLabel, result.MatchScore = StatusAmbiguous, strings.TrimSpace(candidate.Label), &score
	result.Suggestion = candidateSuggestion(candidate)
	return result, nil
}

func acceptsStreetCandidate(theater Theater) bool {
	if theater.Provider != "cgr" && theater.Provider != "pathe" && theater.Provider != "kinepolis" {
		return false
	}
	return strings.IndexFunc(theater.Address, unicode.IsDigit) < 0
}

func candidateSuggestion(candidate Candidate) *CandidateSuggestion {
	result := &CandidateSuggestion{
		PostalCode: strings.TrimSpace(candidate.PostalCode),
		City:       strings.TrimSpace(candidate.City),
		Type:       strings.TrimSpace(candidate.Type),
	}
	if validCandidateCoordinates(candidate) {
		latitude, longitude := candidate.Latitude, candidate.Longitude
		result.Latitude, result.Longitude = &latitude, &longitude
	}
	return result
}

func validCandidateCoordinates(candidate Candidate) bool {
	return candidate.HasCoordinates && validCoordinates(candidate.Latitude, candidate.Longitude)
}

func incrementStatus(summary *Summary, status Status) {
	switch status {
	case StatusMatched:
		summary.Matched++
	case StatusAmbiguous:
		summary.Ambiguous++
	case StatusNotFound:
		summary.NotFound++
	}
}
