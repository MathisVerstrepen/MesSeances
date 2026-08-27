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

type PostgresResolutionStore struct{ pool *pgxpool.Pool }

func NewPostgresResolutionStore(pool *pgxpool.Pool) *PostgresResolutionStore {
	return &PostgresResolutionStore{pool: pool}
}

func (s *PostgresResolutionStore) Pending(ctx context.Context, limit, offset int) ([]PendingLocation, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, ErrResolutionInvalid
	}
	rows, err := s.pool.Query(ctx, `SELECT t.provider,t.provider_id,t.id,t.name,t.address,t.postal_code,t.city,
	       l.status,l.updated_at,l.address_hash,l.matched_label,l.match_score,
	       l.candidate_latitude,l.candidate_longitude,l.candidate_postal_code,l.candidate_city,l.candidate_type
	FROM schedule_snapshot snapshot
	JOIN theaters t ON t.generation_id=snapshot.version
	JOIN theater_locations l ON l.provider=t.provider AND l.provider_theater_id=t.provider_id
	WHERE snapshot.singleton=true AND l.status IN ('ambiguous','not_found')
	ORDER BY t.provider,t.provider_id,t.id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("read pending theater locations failed")
	}
	defer rows.Close()
	items := make([]PendingLocation, 0)
	for rows.Next() {
		var item PendingLocation
		var label, candidatePostalCode, candidateCity, candidateType *string
		var score, candidateLatitude, candidateLongitude *float64
		if err := rows.Scan(&item.Provider, &item.ProviderTheaterID, &item.TheaterID, &item.Name, &item.Address, &item.PostalCode, &item.City, &item.Status, &item.UpdatedAt, &item.addressHash, &label, &score, &candidateLatitude, &candidateLongitude, &candidatePostalCode, &candidateCity, &candidateType); err != nil {
			return nil, fmt.Errorf("read pending theater locations failed")
		}
		if label != nil && score != nil {
			item.Suggestion = &ResolutionSuggestion{
				Label:      strings.TrimSpace(*label),
				Score:      *score,
				Latitude:   candidateLatitude,
				Longitude:  candidateLongitude,
				PostalCode: trimmedString(candidatePostalCode),
				City:       trimmedString(candidateCity),
				Type:       trimmedString(candidateType),
			}
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read pending theater locations failed")
	}
	return items, nil
}

func (s *PostgresResolutionStore) AcceptSuggestion(ctx context.Context, provider, providerTheaterID string, expectedUpdatedAt, updatedAt time.Time) error {
	return s.resolve(ctx, provider, providerTheaterID, expectedUpdatedAt, 0, 0, updatedAt, true)
}

func (s *PostgresResolutionStore) SetManual(ctx context.Context, provider, providerTheaterID string, expectedUpdatedAt time.Time, latitude, longitude float64, updatedAt time.Time) error {
	if !validCoordinates(latitude, longitude) {
		return ErrResolutionInvalid
	}
	return s.resolve(ctx, provider, providerTheaterID, expectedUpdatedAt, latitude, longitude, updatedAt, false)
}

func (s *PostgresResolutionStore) resolve(ctx context.Context, provider, providerTheaterID string, expectedUpdatedAt time.Time, latitude, longitude float64, updatedAt time.Time, acceptSuggestion bool) error {
	if !ValidProviderTheaterID(provider, providerTheaterID) || expectedUpdatedAt.IsZero() || updatedAt.IsZero() {
		return ErrResolutionInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin theater location resolution failed")
	}
	defer rollback(tx)
	var version int64
	if err := tx.QueryRow(ctx, "SELECT version FROM theater_location_state WHERE singleton=true FOR UPDATE").Scan(&version); err != nil || version < 0 || version == math.MaxInt64 {
		return fmt.Errorf("read theater location version failed")
	}
	current, err := lockResolutionLocation(ctx, tx, provider, providerTheaterID)
	if err != nil {
		return err
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return ErrResolutionConflict
	}
	if current.Status != StatusAmbiguous && current.Status != StatusNotFound {
		return ErrResolutionNotFound
	}
	var address, postalCode, city string
	err = tx.QueryRow(ctx, `SELECT t.address,t.postal_code,t.city
		FROM schedule_snapshot snapshot
		JOIN theaters t ON t.generation_id=snapshot.version
		WHERE snapshot.singleton=true AND t.provider=$1 AND t.provider_id=$2
		FOR SHARE OF snapshot,t`, provider, providerTheaterID).Scan(&address, &postalCode, &city)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrResolutionNotFound
	}
	if err != nil {
		return fmt.Errorf("read current theater before location resolution failed")
	}
	if acceptSuggestion {
		if current.Status != StatusAmbiguous || current.Suggestion == nil || current.Suggestion.Latitude == nil || current.Suggestion.Longitude == nil || !validCoordinates(*current.Suggestion.Latitude, *current.Suggestion.Longitude) || current.AddressHash != AddressHash(address, postalCode, city) {
			return ErrResolutionConflict
		}
		latitude, longitude = *current.Suggestion.Latitude, *current.Suggestion.Longitude
	}
	command, err := tx.Exec(ctx, `UPDATE theater_locations SET latitude=$3,longitude=$4,source='manual',matched_label=NULL,match_score=NULL,address_hash=NULL,status='manual',updated_at=$5,
		candidate_latitude=NULL,candidate_longitude=NULL,candidate_postal_code=NULL,candidate_city=NULL,candidate_type=NULL
		WHERE provider=$1 AND provider_theater_id=$2`, provider, providerTheaterID, latitude, longitude, updatedAt.UTC())
	if err != nil || command.RowsAffected() != 1 {
		return fmt.Errorf("write resolved theater location failed")
	}
	if _, err := tx.Exec(ctx, "UPDATE theater_location_state SET version=$1 WHERE singleton=true", version+1); err != nil {
		return fmt.Errorf("advance theater location version failed")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit theater location resolution failed")
	}
	return nil
}

func lockResolutionLocation(ctx context.Context, tx pgx.Tx, provider, providerTheaterID string) (Location, error) {
	var result Location
	var candidateLatitude, candidateLongitude *float64
	var addressHash *string
	err := tx.QueryRow(ctx, `SELECT status,updated_at,address_hash,candidate_latitude,candidate_longitude
		FROM theater_locations WHERE provider=$1 AND provider_theater_id=$2 FOR UPDATE`, provider, providerTheaterID).Scan(&result.Status, &result.UpdatedAt, &addressHash, &candidateLatitude, &candidateLongitude)
	if errors.Is(err, pgx.ErrNoRows) {
		return Location{}, ErrResolutionNotFound
	}
	if err != nil {
		return Location{}, fmt.Errorf("lock theater location for resolution failed")
	}
	result.AddressHash = value(addressHash)
	if result.Status == StatusAmbiguous {
		result.Suggestion = &CandidateSuggestion{Latitude: candidateLatitude, Longitude: candidateLongitude}
	}
	return result, nil
}

func trimmedString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}
