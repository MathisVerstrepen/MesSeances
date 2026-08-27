package geocoding

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"time"
)

var (
	ErrResolutionInvalid     = errors.New("invalid theater location resolution")
	ErrResolutionNotFound    = errors.New("theater location not found")
	ErrResolutionConflict    = errors.New("theater location conflict")
	ErrResolutionUnavailable = errors.New("theater location resolution unavailable")
)

var (
	ugcTheaterID   = regexp.MustCompile(`^[1-9][0-9]*$`)
	chainTheaterID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
	cgrTheaterID   = regexp.MustCompile(`^[A-Z][0-9]{4}$`)
)

type ResolutionSuggestion struct {
	Label      string
	Score      float64
	Latitude   *float64
	Longitude  *float64
	PostalCode *string
	City       *string
	Type       *string
}

type PendingLocation struct {
	Provider            string
	ProviderTheaterID   string
	TheaterID           string
	Name                string
	Address             string
	PostalCode          string
	City                string
	Status              Status
	UpdatedAt           time.Time
	Suggestion          *ResolutionSuggestion
	CanAcceptSuggestion bool
	addressHash         string
}

type ResolutionStore interface {
	Pending(context.Context, int, int) ([]PendingLocation, error)
	AcceptSuggestion(context.Context, string, string, time.Time, time.Time) error
	SetManual(context.Context, string, string, time.Time, float64, float64, time.Time) error
}

type ResolutionService struct {
	store ResolutionStore
	now   func() time.Time
}

func NewResolutionService(store ResolutionStore, now func() time.Time) *ResolutionService {
	if now == nil {
		now = time.Now
	}
	return &ResolutionService{store: store, now: now}
}

func (s *ResolutionService) Pending(ctx context.Context, limit, offset int) ([]PendingLocation, error) {
	if s == nil || s.store == nil {
		return nil, ErrResolutionUnavailable
	}
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, ErrResolutionInvalid
	}
	items, err := s.store.Pending(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	for index := range items {
		item := &items[index]
		item.CanAcceptSuggestion = item.Status == StatusAmbiguous && validResolutionSuggestion(item.Suggestion) && item.addressHash == AddressHash(item.Address, item.PostalCode, item.City)
	}
	return items, nil
}

func (s *ResolutionService) AcceptSuggestion(ctx context.Context, provider, providerTheaterID string, expectedUpdatedAt time.Time) error {
	if s == nil || s.store == nil {
		return ErrResolutionUnavailable
	}
	if !ValidProviderTheaterID(provider, providerTheaterID) || expectedUpdatedAt.IsZero() {
		return ErrResolutionInvalid
	}
	now := s.now().UTC()
	if now.IsZero() {
		return fmt.Errorf("invalid theater location resolution clock")
	}
	return s.store.AcceptSuggestion(ctx, provider, providerTheaterID, expectedUpdatedAt, now)
}

func (s *ResolutionService) SetManual(ctx context.Context, provider, providerTheaterID string, expectedUpdatedAt time.Time, latitude, longitude float64) error {
	if s == nil || s.store == nil {
		return ErrResolutionUnavailable
	}
	if !ValidProviderTheaterID(provider, providerTheaterID) || expectedUpdatedAt.IsZero() || !validCoordinates(latitude, longitude) {
		return ErrResolutionInvalid
	}
	now := s.now().UTC()
	if now.IsZero() {
		return fmt.Errorf("invalid theater location resolution clock")
	}
	return s.store.SetManual(ctx, provider, providerTheaterID, expectedUpdatedAt, latitude, longitude, now)
}

func ValidProviderTheaterID(provider, providerTheaterID string) bool {
	switch provider {
	case "ugc":
		return len(providerTheaterID) <= 128 && ugcTheaterID.MatchString(providerTheaterID)
	case "kinepolis", "pathe":
		return chainTheaterID.MatchString(providerTheaterID)
	case "cgr":
		return cgrTheaterID.MatchString(providerTheaterID)
	default:
		return false
	}
}

func validResolutionSuggestion(suggestion *ResolutionSuggestion) bool {
	return suggestion != nil && suggestion.Latitude != nil && suggestion.Longitude != nil && !math.IsNaN(*suggestion.Latitude) && !math.IsNaN(*suggestion.Longitude) && !math.IsInf(*suggestion.Latitude, 0) && !math.IsInf(*suggestion.Longitude, 0) && validCoordinates(*suggestion.Latitude, *suggestion.Longitude)
}
