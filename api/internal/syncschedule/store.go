package syncschedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	List(context.Context) ([]Schedule, error)
	Get(context.Context, Target, int64) (Schedule, error)
	Create(context.Context, Schedule) (Schedule, error)
	Update(context.Context, Schedule) (Schedule, error)
	Delete(context.Context, Target, int64) error
	ClaimOccurrence(context.Context, Occurrence) (bool, error)
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

const scheduleColumns = `id,target,revision,enabled,schedule_kind,local_time,weekdays,cron_expression,updated_at`

func (s *PostgresStore) List(ctx context.Context) ([]Schedule, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+scheduleColumns+` FROM sync_schedules
		ORDER BY CASE target WHEN 'ugc' THEN 1 WHEN 'kinepolis' THEN 2 WHEN 'pathe' THEN 3 WHEN 'cgr' THEN 4 WHEN 'tmdb_metadata_refresh' THEN 5 ELSE 6 END,id`)
	if err != nil {
		return nil, fmt.Errorf("list sync schedules failed")
	}
	defer rows.Close()
	schedules := []Schedule{}
	for rows.Next() {
		schedule, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, schedule)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("list sync schedules failed")
	}
	return schedules, nil
}

func (s *PostgresStore) Get(ctx context.Context, target Target, id int64) (Schedule, error) {
	schedule, err := scanSchedule(s.pool.QueryRow(ctx, `SELECT `+scheduleColumns+` FROM sync_schedules WHERE target=$1 AND id=$2`, target, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Schedule{}, ErrScheduleMissing
	}
	if err != nil {
		return Schedule{}, fmt.Errorf("get sync schedule failed")
	}
	return schedule, nil
}

func (s *PostgresStore) Create(ctx context.Context, schedule Schedule) (Schedule, error) {
	localTime, weekdays, expression := definitionValues(schedule.Definition)
	committed, err := scanSchedule(s.pool.QueryRow(ctx, `INSERT INTO sync_schedules
		(target,enabled,schedule_kind,local_time,weekdays,cron_expression)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING `+scheduleColumns,
		schedule.Target, schedule.Enabled, schedule.Definition.Kind, localTime, weekdays, expression))
	if err != nil {
		return Schedule{}, fmt.Errorf("create sync schedule failed")
	}
	return committed, nil
}

func (s *PostgresStore) Update(ctx context.Context, schedule Schedule) (Schedule, error) {
	localTime, weekdays, expression := definitionValues(schedule.Definition)
	committed, err := scanSchedule(s.pool.QueryRow(ctx, `UPDATE sync_schedules SET
		revision=revision+1,enabled=$3,schedule_kind=$4,local_time=$5,weekdays=$6,cron_expression=$7,updated_at=now()
		WHERE target=$1 AND id=$2 RETURNING `+scheduleColumns,
		schedule.Target, schedule.ID, schedule.Enabled, schedule.Definition.Kind, localTime, weekdays, expression))
	if errors.Is(err, pgx.ErrNoRows) {
		return Schedule{}, ErrScheduleMissing
	}
	if err != nil {
		return Schedule{}, fmt.Errorf("update sync schedule failed")
	}
	return committed, nil
}

func (s *PostgresStore) Delete(ctx context.Context, target Target, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sync_schedules WHERE target=$1 AND id=$2`, target, id)
	if err != nil {
		return fmt.Errorf("delete sync schedule failed")
	}
	if tag.RowsAffected() != 1 {
		return ErrScheduleMissing
	}
	return nil
}

func (s *PostgresStore) ClaimOccurrence(ctx context.Context, occurrence Occurrence) (bool, error) {
	var scheduleID int64
	err := s.pool.QueryRow(ctx, `INSERT INTO sync_schedule_occurrence_claims
		(schedule_id,schedule_revision,scheduled_for,updated_at)
		SELECT id,$2,$3,now() FROM sync_schedules
		WHERE id=$1 AND target=$4 AND revision=$2 AND enabled
		ON CONFLICT (schedule_id) DO UPDATE SET
			schedule_revision=EXCLUDED.schedule_revision,
			scheduled_for=EXCLUDED.scheduled_for,
			updated_at=now()
		WHERE (sync_schedule_occurrence_claims.schedule_revision,sync_schedule_occurrence_claims.scheduled_for)
			< (EXCLUDED.schedule_revision,EXCLUDED.scheduled_for)
		RETURNING schedule_id`, occurrence.ScheduleID, occurrence.Revision, occurrence.ScheduledFor.UTC().Truncate(time.Minute), occurrence.Target).Scan(&scheduleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim sync schedule occurrence failed")
	}
	return scheduleID == occurrence.ScheduleID, nil
}

func definitionValues(definition Definition) (localTime, weekdays, expression any) {
	switch definition.Kind {
	case KindDaily:
		localTime = definition.Time
	case KindWeekly:
		localTime = definition.Time
		weekdays = definition.Weekdays
	case KindCron:
		expression = definition.Expression
	}
	return localTime, weekdays, expression
}

type scheduleScanner interface {
	Scan(...any) error
}

func scanSchedule(row scheduleScanner) (Schedule, error) {
	var schedule Schedule
	var localTime *string
	var expression *string
	if err := row.Scan(&schedule.ID, &schedule.Target, &schedule.Revision, &schedule.Enabled, &schedule.Definition.Kind, &localTime, &schedule.Definition.Weekdays, &expression, &schedule.UpdatedAt); err != nil {
		return Schedule{}, err
	}
	if localTime != nil {
		schedule.Definition.Time = *localTime
	}
	if expression != nil {
		schedule.Definition.Expression = *expression
	}
	return schedule, nil
}
