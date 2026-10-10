package schedulepg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"messeances/api/internal/cinemaimage"
	"messeances/api/internal/schedule"
)

// SitemapObserver configures observation independently of ordinary read-only schedule loads.
// PublicImages must match whether the HTTP public image controller is enabled.
type SitemapObserver struct {
	Store        *Store
	PublicImages bool
	Now          func() time.Time
}

func (s *Store) ObserveSitemapData(ctx context.Context) (schedule.SitemapData, error) {
	return (&SitemapObserver{Store: s}).ObserveSitemapData(ctx)
}

func (o *SitemapObserver) ObserveSitemapData(ctx context.Context) (schedule.SitemapData, error) {
	clock := o.Now
	if clock == nil {
		clock = time.Now
	}
	for attempt := 0; attempt < 2; attempt++ {
		result, err := o.observe(ctx, clock)
		if err == nil {
			return result, nil
		}
		var pgError *pgconn.PgError
		if !errors.As(err, &pgError) || pgError.Code != "40001" || ctx.Err() != nil {
			return schedule.SitemapData{}, err
		}
	}
	return schedule.SitemapData{}, fmt.Errorf("public content observation unavailable")
}

type observedPage struct {
	fingerprint []byte
	present     bool
	changedAt   *time.Time
}

func (o *SitemapObserver) observe(ctx context.Context, clock func() time.Time) (schedule.SitemapData, error) {
	tx, err := o.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return schedule.SitemapData{}, err
	}
	defer rollbackScheduleTx(tx)
	var previous *time.Time
	if err := tx.QueryRow(ctx, `SELECT observed_at FROM public_page_content_state WHERE singleton FOR UPDATE`).Scan(&previous); err != nil {
		return schedule.SitemapData{}, err
	}
	// PostgreSQL stores microsecond precision; use exactly the same instant in storage and JSON.
	now := clock().UTC().Truncate(time.Microsecond)
	if now.IsZero() || previous != nil && now.Before(*previous) {
		return schedule.SitemapData{}, fmt.Errorf("public content clock regressed")
	}
	dataset, revision, err := loadDataset(ctx, tx)
	if err != nil {
		return schedule.SitemapData{}, err
	}
	images := make(map[string]schedule.PublicPageImage)
	if o.PublicImages && revision.ScheduleVersion > 0 {
		images, err = loadPublicPageImages(ctx, tx, revision.ScheduleVersion)
		if err != nil {
			return schedule.SitemapData{}, err
		}
	}
	projection, err := schedule.BuildSitemapProjection(schedule.NewSnapshotView(dataset, revision), now, images)
	if err != nil {
		return schedule.SitemapData{}, err
	}
	projection.Data.Revision = fmt.Sprintf("schedule:%d;enrichment:%d;location:%d", revision.ScheduleVersion, revision.EnrichmentVersion, revision.TheaterLocationVersion)
	stored := make(map[string]observedPage)
	rows, err := tx.Query(ctx, `SELECT path,fingerprint,present,changed_at FROM public_page_content`)
	if err != nil {
		return schedule.SitemapData{}, err
	}
	for rows.Next() {
		var path string
		var page observedPage
		if err := rows.Scan(&path, &page.fingerprint, &page.present, &page.changedAt); err != nil {
			rows.Close()
			return schedule.SitemapData{}, err
		}
		if page.changedAt != nil {
			utc := page.changedAt.UTC()
			page.changedAt = &utc
		}
		if page.changedAt != nil && page.changedAt.After(now) {
			rows.Close()
			return schedule.SitemapData{}, fmt.Errorf("public content clock regressed")
		}
		// A missing schedule is not evidence of cancellation or removal of local pages.
		if revision.ScheduleVersion == 0 && page.present && !strings.HasPrefix(path, "/film/") {
			rows.Close()
			return schedule.SitemapData{}, schedule.ErrNoCompleteSnapshot
		}
		stored[path] = page
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return schedule.SitemapData{}, err
	}
	rows.Close()
	paths := make([]string, 0, len(projection.Fingerprints))
	for path := range projection.Fingerprints {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	batch := &pgx.Batch{}
	for _, path := range paths {
		fingerprint := projection.Fingerprints[path]
		old, exists := stored[path]
		changed := !exists || !old.present || !bytes.Equal(old.fingerprint, fingerprint[:])
		date := old.changedAt
		if changed && previous != nil {
			value := now
			date = &value
		}
		projection.Data.LastmodByPath[path] = date
		if changed {
			batch.Queue(`INSERT INTO public_page_content(path,fingerprint,present,changed_at) VALUES($1,$2,true,$3) ON CONFLICT(path) DO UPDATE SET fingerprint=excluded.fingerprint,present=true,changed_at=excluded.changed_at`, path, fingerprint[:], date)
		}
	}
	batch.Queue(`UPDATE public_page_content SET present=false,changed_at=$2 WHERE present AND NOT(path=ANY($1::text[]))`, paths, now)
	// Always write the singleton, even for an equal observation. A waiter with an old
	// repeatable-read snapshot must serialize-fail, never overwrite with stale content.
	batch.Queue(`UPDATE public_page_content_state SET observed_at=$1 WHERE singleton`, now)
	results := tx.SendBatch(ctx, batch)
	if err := results.Close(); err != nil {
		return schedule.SitemapData{}, err
	}
	if err := ctx.Err(); err != nil {
		return schedule.SitemapData{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return schedule.SitemapData{}, err
	}
	return projection.Data, nil
}

func loadPublicPageImages(ctx context.Context, tx pgx.Tx, version int64) (map[string]schedule.PublicPageImage, error) {
	rows, err := tx.Query(ctx, `SELECT t.id,i.provider,i.provider_theater_id,i.image_revision,i.file_key,i.width,i.height,i.size_bytes FROM theater_images i JOIN theaters t ON t.generation_id=$1 AND t.provider=i.provider AND t.provider_id=i.provider_theater_id WHERE i.file_key IS NOT NULL`, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	images := make(map[string]schedule.PublicPageImage)
	for rows.Next() {
		var theaterID string
		var id cinemaimage.Identity
		var record cinemaimage.Record
		if err := rows.Scan(&theaterID, &id.Provider, &id.ProviderTheaterID, &record.Revision, &record.Key, &record.Width, &record.Height, &record.Size); err != nil {
			return nil, err
		}
		image, err := cinemaimage.MaterializePublicImage(id, record)
		if err != nil {
			return nil, err
		}
		if image != nil {
			images[theaterID] = schedule.PublicPageImage{URL: image.URL, Width: image.Width, Height: image.Height}
		}
	}
	return images, rows.Err()
}
