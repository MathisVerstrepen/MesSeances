package syncschedule

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/synccontrol"
)

type Store interface {
	List(context.Context) ([]Schedule, error)
	Get(context.Context, synccontrol.Target) (Schedule, error)
	Upsert(context.Context, Schedule) (Schedule, error)
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) List(ctx context.Context) ([]Schedule, error) {
	rows, err := s.pool.Query(ctx, `SELECT provider,revision,enabled,schedule_kind,local_time,weekdays,cron_expression,updated_at
		FROM sync_schedules ORDER BY CASE provider WHEN 'ugc' THEN 1 WHEN 'kinepolis' THEN 2 WHEN 'pathe' THEN 3 ELSE 4 END`)
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

func (s *PostgresStore) Get(ctx context.Context, provider synccontrol.Target) (Schedule, error) {
	schedule, err := scanSchedule(s.pool.QueryRow(ctx, `SELECT provider,revision,enabled,schedule_kind,local_time,weekdays,cron_expression,updated_at
		FROM sync_schedules WHERE provider=$1`, provider))
	if errors.Is(err, pgx.ErrNoRows) {
		return Schedule{}, ErrScheduleMissing
	}
	if err != nil {
		return Schedule{}, fmt.Errorf("get sync schedule failed")
	}
	return schedule, nil
}

func (s *PostgresStore) Upsert(ctx context.Context, schedule Schedule) (Schedule, error) {
	var localTime any
	var weekdays any
	var expression any
	switch schedule.Definition.Kind {
	case KindDaily:
		localTime = schedule.Definition.Time
	case KindWeekly:
		localTime = schedule.Definition.Time
		weekdays = schedule.Definition.Weekdays
	case KindCron:
		expression = schedule.Definition.Expression
	}
	committed, err := scanSchedule(s.pool.QueryRow(ctx, `INSERT INTO sync_schedules
		(provider,enabled,schedule_kind,local_time,weekdays,cron_expression)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (provider) DO UPDATE SET
			revision=sync_schedules.revision+1,
			enabled=EXCLUDED.enabled,
			schedule_kind=EXCLUDED.schedule_kind,
			local_time=EXCLUDED.local_time,
			weekdays=EXCLUDED.weekdays,
			cron_expression=EXCLUDED.cron_expression,
			updated_at=now()
		RETURNING provider,revision,enabled,schedule_kind,local_time,weekdays,cron_expression,updated_at`,
		schedule.Provider, schedule.Enabled, schedule.Definition.Kind, localTime, weekdays, expression))
	if err != nil {
		return Schedule{}, fmt.Errorf("save sync schedule failed")
	}
	return committed, nil
}

type scheduleScanner interface {
	Scan(...any) error
}

func scanSchedule(row scheduleScanner) (Schedule, error) {
	var schedule Schedule
	var localTime *string
	var expression *string
	if err := row.Scan(&schedule.Provider, &schedule.Revision, &schedule.Enabled, &schedule.Definition.Kind, &localTime, &schedule.Definition.Weekdays, &expression, &schedule.UpdatedAt); err != nil {
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
