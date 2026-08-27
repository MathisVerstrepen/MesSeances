package geocoding

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

type resolutionMemoryStore struct {
	items          []PendingLocation
	err            error
	acceptCalls    int
	manualCalls    int
	lastProvider   string
	lastTheaterID  string
	lastExpectedAt time.Time
	lastUpdatedAt  time.Time
	lastLatitude   float64
	lastLongitude  float64
}

func (s *resolutionMemoryStore) Pending(context.Context, int, int) ([]PendingLocation, error) {
	return append([]PendingLocation(nil), s.items...), s.err
}

func (s *resolutionMemoryStore) AcceptSuggestion(_ context.Context, provider, theaterID string, expectedAt, updatedAt time.Time) error {
	s.acceptCalls++
	s.lastProvider, s.lastTheaterID, s.lastExpectedAt, s.lastUpdatedAt = provider, theaterID, expectedAt, updatedAt
	return s.err
}

func (s *resolutionMemoryStore) SetManual(_ context.Context, provider, theaterID string, expectedAt time.Time, latitude, longitude float64, updatedAt time.Time) error {
	s.manualCalls++
	s.lastProvider, s.lastTheaterID, s.lastExpectedAt, s.lastUpdatedAt = provider, theaterID, expectedAt, updatedAt
	s.lastLatitude, s.lastLongitude = latitude, longitude
	return s.err
}

func TestResolutionPendingComputesSuggestionAvailabilityFromCurrentAddress(t *testing.T) {
	latitude, longitude := 50.6, 3.1
	base := PendingLocation{Provider: "cgr", ProviderTheaterID: "A1234", Address: "Rue du cinéma", PostalCode: "59000", City: "Lille", Status: StatusAmbiguous, Suggestion: &ResolutionSuggestion{Latitude: &latitude, Longitude: &longitude}}
	base.addressHash = AddressHash(base.Address, base.PostalCode, base.City)
	wrongHash := base
	wrongHash.ProviderTheaterID, wrongHash.addressHash = "B1234", AddressHash("old", base.PostalCode, base.City)
	missingCoordinates := base
	missingCoordinates.ProviderTheaterID = "C1234"
	missingCoordinates.Suggestion = &ResolutionSuggestion{}
	notFound := base
	notFound.ProviderTheaterID, notFound.Status, notFound.Suggestion = "D1234", StatusNotFound, nil
	store := &resolutionMemoryStore{items: []PendingLocation{base, wrongHash, missingCoordinates, notFound}}
	service := NewResolutionService(store, nil)
	items, err := service.Pending(context.Background(), 20, 0)
	if err != nil || len(items) != 4 || !items[0].CanAcceptSuggestion || items[1].CanAcceptSuggestion || items[2].CanAcceptSuggestion || items[3].CanAcceptSuggestion {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}

func TestResolutionServiceValidatesKeysCoordinatesAndVersionToken(t *testing.T) {
	now := time.Date(2026, 8, 26, 14, 0, 0, 0, time.FixedZone("test", 2*60*60))
	expected := time.Date(2026, 8, 26, 11, 0, 0, 0, time.UTC)
	store := &resolutionMemoryStore{}
	service := NewResolutionService(store, func() time.Time { return now })

	if err := service.AcceptSuggestion(context.Background(), "cgr", "A1234", expected); err != nil || store.acceptCalls != 1 || !store.lastUpdatedAt.Equal(now.UTC()) || store.lastUpdatedAt.Location() != time.UTC {
		t.Fatalf("accept calls=%d updated=%s err=%v", store.acceptCalls, store.lastUpdatedAt, err)
	}
	for _, input := range []struct{ provider, id string }{{"other", "A1234"}, {"ugc", "0"}, {"cgr", "12345"}, {"pathe", "bad/id"}} {
		if err := service.AcceptSuggestion(context.Background(), input.provider, input.id, expected); !errors.Is(err, ErrResolutionInvalid) {
			t.Fatalf("key=%+v err=%v", input, err)
		}
	}
	if err := service.AcceptSuggestion(context.Background(), "ugc", "25", time.Time{}); !errors.Is(err, ErrResolutionInvalid) {
		t.Fatalf("zero timestamp err=%v", err)
	}
	for _, coordinates := range [][2]float64{{-90, -180}, {90, 180}, {0, 0}} {
		if err := service.SetManual(context.Background(), "ugc", "25", expected, coordinates[0], coordinates[1]); err != nil {
			t.Fatalf("coordinates=%v err=%v", coordinates, err)
		}
	}
	for _, coordinates := range [][2]float64{{-90.1, 0}, {90.1, 0}, {0, -180.1}, {0, 180.1}, {math.NaN(), 0}, {0, math.Inf(1)}} {
		if err := service.SetManual(context.Background(), "ugc", "25", expected, coordinates[0], coordinates[1]); !errors.Is(err, ErrResolutionInvalid) {
			t.Fatalf("invalid coordinates=%v err=%v", coordinates, err)
		}
	}
}

func TestValidProviderTheaterIDMirrorsCurrentProviderIdentities(t *testing.T) {
	for _, input := range []struct{ provider, id string }{{"ugc", "25"}, {"kinepolis", "FR-Lomme_1"}, {"pathe", "lille"}, {"cgr", "A1234"}} {
		if !ValidProviderTheaterID(input.provider, input.id) {
			t.Fatalf("valid key rejected: %+v", input)
		}
	}
}
