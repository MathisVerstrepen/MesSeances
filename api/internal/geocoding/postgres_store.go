package geocoding

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) Select(ctx context.Context) ([]Theater, error) {
	rows, err := s.pool.Query(ctx, `SELECT t.provider, t.provider_id, t.address, t.postal_code, t.city,
       l.latitude, l.longitude, l.source, l.matched_label, l.match_score, l.address_hash, l.status, l.updated_at,
       l.candidate_latitude, l.candidate_longitude, l.candidate_postal_code, l.candidate_city, l.candidate_type
FROM schedule_snapshot snapshot
JOIN theaters t ON t.generation_id=snapshot.version
LEFT JOIN theater_locations l ON l.provider=t.provider AND l.provider_theater_id=t.provider_id
WHERE snapshot.singleton=true
ORDER BY t.provider, t.provider_id, t.id`)
	if err != nil {
		return nil, fmt.Errorf("select theaters for geocoding failed")
	}
	defer rows.Close()
	result := []Theater{}
	for rows.Next() {
		var theater Theater
		var latitude, longitude, score *float64
		var source, label, hash, status *string
		var candidateLatitude, candidateLongitude *float64
		var candidatePostalCode, candidateCity, candidateType *string
		var updatedAt *time.Time
		if err := rows.Scan(&theater.Provider, &theater.ProviderID, &theater.Address, &theater.PostalCode, &theater.City, &latitude, &longitude, &source, &label, &score, &hash, &status, &updatedAt, &candidateLatitude, &candidateLongitude, &candidatePostalCode, &candidateCity, &candidateType); err != nil {
			return nil, fmt.Errorf("select theaters for geocoding failed")
		}
		if status != nil {
			theater.Location = &Location{Provider: theater.Provider, ProviderTheaterID: theater.ProviderID, Latitude: latitude, Longitude: longitude, Source: value(source), MatchedLabel: value(label), MatchScore: score, AddressHash: value(hash), Status: Status(*status), UpdatedAt: *updatedAt}
			if *status == string(StatusAmbiguous) {
				theater.Location.Suggestion = &CandidateSuggestion{Latitude: candidateLatitude, Longitude: candidateLongitude, PostalCode: value(candidatePostalCode), City: value(candidateCity), Type: value(candidateType)}
			}
		}
		result = append(result, theater)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("select theaters for geocoding failed")
	}
	return result, nil
}

func value(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s *PostgresStore) Save(ctx context.Context, expected *Location, location Location) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin theater location write failed")
	}
	defer rollback(tx)
	var version int64
	if err := tx.QueryRow(ctx, "SELECT version FROM theater_location_state WHERE singleton=true FOR UPDATE").Scan(&version); err != nil || version < 0 || version == math.MaxInt64 {
		return false, fmt.Errorf("read theater location version failed")
	}
	current, err := selectLocationForUpdate(ctx, tx, location.Provider, location.ProviderTheaterID)
	if err != nil {
		return false, err
	}
	if !sameLocationVersion(expected, current) || current != nil && current.Status == StatusManual {
		return false, nil
	}
	var latitude, longitude, label, score, hash any
	var candidateLatitude, candidateLongitude, candidatePostalCode, candidateCity, candidateType any
	if location.Latitude != nil {
		latitude = *location.Latitude
		longitude = *location.Longitude
	}
	if location.MatchedLabel != "" {
		label = location.MatchedLabel
	}
	if location.MatchScore != nil {
		score = *location.MatchScore
	}
	if location.AddressHash != "" {
		hash = location.AddressHash
	}
	if location.Status == StatusAmbiguous && location.Suggestion != nil {
		candidateLatitude, candidateLongitude, candidatePostalCode, candidateCity, candidateType = suggestionValues(location.Suggestion)
	}
	_, err = tx.Exec(ctx, `INSERT INTO theater_locations (provider,provider_theater_id,latitude,longitude,source,matched_label,match_score,address_hash,status,updated_at,candidate_latitude,candidate_longitude,candidate_postal_code,candidate_city,candidate_type)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
ON CONFLICT (provider,provider_theater_id) DO UPDATE SET latitude=EXCLUDED.latitude,longitude=EXCLUDED.longitude,source=EXCLUDED.source,matched_label=EXCLUDED.matched_label,match_score=EXCLUDED.match_score,address_hash=EXCLUDED.address_hash,status=EXCLUDED.status,updated_at=EXCLUDED.updated_at,candidate_latitude=EXCLUDED.candidate_latitude,candidate_longitude=EXCLUDED.candidate_longitude,candidate_postal_code=EXCLUDED.candidate_postal_code,candidate_city=EXCLUDED.candidate_city,candidate_type=EXCLUDED.candidate_type`, location.Provider, location.ProviderTheaterID, latitude, longitude, location.Source, label, score, hash, string(location.Status), location.UpdatedAt, candidateLatitude, candidateLongitude, candidatePostalCode, candidateCity, candidateType)
	if err != nil {
		return false, fmt.Errorf("write theater location failed")
	}
	if _, err := tx.Exec(ctx, "UPDATE theater_location_state SET version=$1 WHERE singleton=true", version+1); err != nil {
		return false, fmt.Errorf("advance theater location version failed")
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit theater location write failed")
	}
	return true, nil
}

func suggestionValues(suggestion *CandidateSuggestion) (latitude, longitude, postalCode, city, candidateType any) {
	if suggestion.Latitude != nil && suggestion.Longitude != nil && validCoordinates(*suggestion.Latitude, *suggestion.Longitude) {
		latitude, longitude = *suggestion.Latitude, *suggestion.Longitude
	}
	if value := strings.TrimSpace(suggestion.PostalCode); value != "" {
		postalCode = value
	}
	if value := strings.TrimSpace(suggestion.City); value != "" {
		city = value
	}
	if value := strings.TrimSpace(suggestion.Type); value != "" {
		candidateType = value
	}
	return
}

func validCoordinates(latitude, longitude float64) bool {
	return !math.IsNaN(latitude) && !math.IsNaN(longitude) && !math.IsInf(latitude, 0) && !math.IsInf(longitude, 0) && latitude >= -90 && latitude <= 90 && longitude >= -180 && longitude <= 180
}

func selectLocationForUpdate(ctx context.Context, tx pgx.Tx, provider, providerID string) (*Location, error) {
	var result Location
	err := tx.QueryRow(ctx, `SELECT status,updated_at FROM theater_locations WHERE provider=$1 AND provider_theater_id=$2 FOR UPDATE`, provider, providerID).Scan(&result.Status, &result.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read theater location before write failed")
	}
	return &result, nil
}

func sameLocationVersion(expected, current *Location) bool {
	if expected == nil || current == nil {
		return expected == nil && current == nil
	}
	return expected.Status == current.Status && expected.UpdatedAt.Equal(current.UpdatedAt)
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
