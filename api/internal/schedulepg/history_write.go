package schedulepg

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"messeances/api/internal/schedule"
)

func retainHistory(ctx context.Context, tx pgx.Tx, generation int64, providers []string, received time.Time) error {
	queries := []string{
		`INSERT INTO screening_history_theaters (id,provider,provider_id,slug,name,address,city,postal_code,city_slug,city_name,passes,last_observed_at)
		SELECT t.id,t.provider,t.provider_id,t.slug,t.name,t.address,t.city,t.postal_code,'',t.city,
		ARRAY(SELECT p.pass_code FROM theater_passes p WHERE p.generation_id=t.generation_id AND p.theater_id=t.id ORDER BY p.pass_code),$3
		FROM theaters t WHERE generation_id=$1 AND provider=ANY($2)
		ON CONFLICT (id) DO UPDATE SET slug=excluded.slug,name=excluded.name,address=excluded.address,city=excluded.city,postal_code=excluded.postal_code,passes=excluded.passes,last_observed_at=excluded.last_observed_at`,
		`INSERT INTO screening_history_showtimes (id,provider,provider_showing_id,theater_id,movie_provider_id,service_date,start_time,end_time,first_part_duration_minutes,language,provider_version,format,room,booking_url,first_seen_at,last_seen_at,source_generated_at,last_generation)
		SELECT s.id,s.provider,s.provider_showing_id,s.theater_id,s.movie_provider_id,s.service_date,s.start_time,s.end_time,s.first_part_duration_minutes,s.language,s.provider_version,s.format,s.room,s.booking_url,$3,$3,p.generated_at,$1
		FROM showtimes s JOIN provider_snapshots p USING (generation_id,provider) WHERE s.generation_id=$1 AND s.provider=ANY($2)
		ON CONFLICT (provider,provider_showing_id,theater_id,service_date) DO UPDATE SET id=excluded.id,movie_provider_id=excluded.movie_provider_id,start_time=excluded.start_time,end_time=excluded.end_time,first_part_duration_minutes=excluded.first_part_duration_minutes,language=excluded.language,provider_version=excluded.provider_version,format=excluded.format,room=excluded.room,booking_url=excluded.booking_url,last_seen_at=excluded.last_seen_at,source_generated_at=excluded.source_generated_at,last_generation=excluded.last_generation`,
		`INSERT INTO screening_history_providers (provider,collection_started_at,last_publication_at,last_generation,source_generated_at)
		SELECT provider,$3,$3,$1,generated_at FROM provider_snapshots WHERE generation_id=$1 AND provider=ANY($2)
		ON CONFLICT (provider) DO UPDATE SET last_publication_at=excluded.last_publication_at,last_generation=excluded.last_generation,source_generated_at=excluded.source_generated_at`,
	}
	for _, query := range queries {
		if _, err := tx.Exec(ctx, query, generation, providers, received); err != nil {
			return fmt.Errorf("retain screening history failed: %w", err)
		}
	}
	// Only cinema metadata enters Go. Historical screenings never enter snapshots
	// or a Go analytics inventory, regardless of retained history size.
	rows, err := tx.Query(ctx, `SELECT id,city FROM screening_history_theaters ORDER BY id`)
	if err != nil {
		return fmt.Errorf("read history cities: %w", err)
	}
	theaters, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (schedule.TheaterRecord, error) {
		var theater schedule.TheaterRecord
		err := row.Scan(&theater.ID, &theater.City)
		return theater, err
	})
	if err != nil {
		return fmt.Errorf("read history cities: %w", err)
	}
	ids, names, slugs := make([]string, len(theaters)), make([]string, len(theaters)), make([]string, len(theaters))
	for i, city := range schedule.TheaterCityIdentities(theaters) {
		ids[i], names[i], slugs[i] = theaters[i].ID, city.Name, city.Slug
	}
	_, err = tx.Exec(ctx, `UPDATE screening_history_theaters t SET city_name=u.name,city_slug=u.slug FROM unnest($1::text[],$2::text[],$3::text[]) u(id,name,slug) WHERE t.id=u.id AND (t.city_name,t.city_slug) IS DISTINCT FROM (u.name,u.slug)`, ids, names, slugs)
	if err != nil {
		return fmt.Errorf("update history cities: %w", err)
	}
	return nil
}
