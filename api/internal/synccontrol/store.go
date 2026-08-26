package synccontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const historyLimit = 50

type Snapshot struct {
	Job  *Status  `json:"job"`
	Runs []Status `json:"runs"`
}

type RunStore interface {
	Create(context.Context, Status) (Status, error)
	Update(context.Context, Status) error
	Snapshot(context.Context) (Snapshot, error)
	ReconcileRunning(context.Context, time.Time) error
}

type PostgresRunStore struct{ pool *pgxpool.Pool }

func NewPostgresRunStore(pool *pgxpool.Pool) *PostgresRunStore {
	return &PostgresRunStore{pool: pool}
}

func (s *PostgresRunStore) Create(ctx context.Context, status Status) (Status, error) {
	status = sanitizeStatusLogs(status)
	providers, err := json.Marshal(status.Providers)
	if err != nil {
		return Status{}, fmt.Errorf("encode sync run failed")
	}
	trigger := status.Trigger
	if trigger == "" {
		trigger = TriggerManual
		status.Trigger = trigger
	}
	var revision any
	var scheduledFor any
	var attempt any
	if status.Occurrence != nil {
		revision = status.Occurrence.Revision
		scheduledFor = status.Occurrence.ScheduledFor
		attempt = status.Occurrence.Attempt
	}
	var id int64
	err = s.pool.QueryRow(ctx, `INSERT INTO sync_runs
        (target,state,started_at,finished_at,window_from,window_through,providers,trigger_source,schedule_revision,scheduled_for,schedule_attempt)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
        ON CONFLICT DO NOTHING
        RETURNING id`, status.Target, status.State, status.StartedAt, status.FinishedAt, status.From, status.Through, providers, trigger, revision, scheduledFor, attempt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) && trigger == TriggerScheduled {
		return Status{}, ErrOccurrenceClaimed
	}
	if err != nil {
		return Status{}, fmt.Errorf("create sync run failed")
	}
	status.ID = strconv.FormatInt(id, 10)
	return status, nil
}

func (s *PostgresRunStore) Update(ctx context.Context, status Status) error {
	id, providers, err := persistenceValues(status)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE sync_runs SET state=$2, finished_at=$3, providers=$4, window_through=$5 WHERE id=$1`, id, status.State, status.FinishedAt, providers, status.Through)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("update sync run failed")
	}
	return nil
}

func (s *PostgresRunStore) Snapshot(ctx context.Context) (Snapshot, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Snapshot{}, fmt.Errorf("read sync run snapshot failed")
	}
	defer rollbackRunTx(tx)
	result := Snapshot{Runs: []Status{}}
	row := tx.QueryRow(ctx, `SELECT `+runColumns+` FROM sync_runs WHERE state='running' ORDER BY started_at DESC,id DESC LIMIT 1`)
	job, err := scanStatus(row)
	if err == nil {
		result.Job = &job
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, fmt.Errorf("read sync run snapshot failed")
	}

	runs, err := listTerminal(ctx, tx, historyLimit)
	if err != nil {
		return Snapshot{}, err
	}
	result.Runs = runs
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, fmt.Errorf("read sync run snapshot failed")
	}
	return result, nil
}

// List remains available for persistence-focused callers. It returns terminal history only.
func (s *PostgresRunStore) List(ctx context.Context, limit int) ([]Status, error) {
	return listTerminal(ctx, s.pool, limit)
}

type runQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func listTerminal(ctx context.Context, querier runQuerier, limit int) ([]Status, error) {
	if limit <= 0 || limit > historyLimit {
		limit = historyLimit
	}
	rows, err := querier.Query(ctx, `SELECT `+runColumns+` FROM sync_runs WHERE state<>'running' ORDER BY started_at DESC,id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list sync runs failed")
	}
	defer rows.Close()
	runs := make([]Status, 0, limit)
	for rows.Next() {
		status, err := scanStatus(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, status)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("list sync runs failed")
	}
	return runs, nil
}

func (s *PostgresRunStore) ReconcileRunning(ctx context.Context, finishedAt time.Time) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("reconcile sync runs failed")
	}
	defer rollbackRunTx(tx)
	rows, err := tx.Query(ctx, `SELECT `+runColumns+` FROM sync_runs WHERE state='running' FOR UPDATE`)
	if err != nil {
		return fmt.Errorf("reconcile sync runs failed")
	}
	var stale []Status
	for rows.Next() {
		status, scanErr := scanStatus(rows)
		if scanErr != nil {
			rows.Close()
			return scanErr
		}
		stale = append(stale, status)
	}
	if rows.Err() != nil {
		rows.Close()
		return fmt.Errorf("reconcile sync runs failed")
	}
	rows.Close()
	for _, status := range stale {
		for provider, providerStatus := range status.Providers {
			switch providerStatus.State {
			case ProviderRunning:
				target := Target(provider)
				line := failureLog(finishedAt.UTC(), target, StageOrchestration, logFailure{Operation: operationOrchestration, Category: categoryCanceled})
				status.Providers[provider] = ProviderStatus{State: ProviderFailed, ErrorCode: FailureCanceled, Log: []string{line}}
			case ProviderPending:
				status.Providers[provider] = ProviderStatus{State: ProviderSkipped}
			}
		}
		status.State = StateFailed
		finished := finishedAt.UTC()
		status.FinishedAt = &finished
		id, providers, valueErr := persistenceValues(status)
		if valueErr != nil {
			return valueErr
		}
		tag, err := tx.Exec(ctx, `UPDATE sync_runs SET state=$2,finished_at=$3,providers=$4 WHERE id=$1`, id, status.State, status.FinishedAt, providers)
		if err != nil || tag.RowsAffected() != 1 {
			return fmt.Errorf("reconcile sync runs failed")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("reconcile sync runs failed")
	}
	return nil
}

const runColumns = `id,target,state,started_at,finished_at,window_from,window_through,providers,trigger_source,schedule_revision,scheduled_for,schedule_attempt`

type rowScanner interface{ Scan(...any) error }

func scanStatus(row rowScanner) (Status, error) {
	var status Status
	var id int64
	var providers []byte
	var from, through time.Time
	var revision *int64
	var scheduledFor *time.Time
	var attempt *int16
	if err := row.Scan(&id, &status.Target, &status.State, &status.StartedAt, &status.FinishedAt, &from, &through, &providers, &status.Trigger, &revision, &scheduledFor, &attempt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Status{}, pgx.ErrNoRows
		}
		return Status{}, fmt.Errorf("read sync run failed")
	}
	if err := json.Unmarshal(providers, &status.Providers); err != nil {
		return Status{}, fmt.Errorf("decode sync run failed")
	}
	status.ID = strconv.FormatInt(id, 10)
	status.From = from.Format("2006-01-02")
	status.Through = through.Format("2006-01-02")
	if revision != nil && scheduledFor != nil && attempt != nil {
		status.Occurrence = &Occurrence{Provider: status.Target, Revision: *revision, ScheduledFor: scheduledFor.UTC(), Attempt: int(*attempt)}
	}
	return sanitizeStatusLogs(status), nil
}

func persistenceValues(status Status) (int64, []byte, error) {
	id, err := strconv.ParseInt(status.ID, 10, 64)
	if err != nil || id <= 0 {
		return 0, nil, fmt.Errorf("invalid sync run id")
	}
	status = sanitizeStatusLogs(status)
	providers, err := json.Marshal(status.Providers)
	if err != nil {
		return 0, nil, fmt.Errorf("encode sync run failed")
	}
	return id, providers, nil
}

func rollbackRunTx(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
