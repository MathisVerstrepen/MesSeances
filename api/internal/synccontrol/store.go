package synccontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const historyLimit = 50

type RunStore interface {
	Create(context.Context, Status) (Status, error)
	Update(context.Context, Status) error
	List(context.Context, int) ([]Status, error)
	ReconcileRunning(context.Context, time.Time) error
}

type PostgresRunStore struct{ pool *pgxpool.Pool }

func NewPostgresRunStore(pool *pgxpool.Pool) *PostgresRunStore {
	return &PostgresRunStore{pool: pool}
}

func (s *PostgresRunStore) Create(ctx context.Context, status Status) (Status, error) {
	providers, err := json.Marshal(status.Providers)
	if err != nil {
		return Status{}, fmt.Errorf("encode sync run failed")
	}
	var id int64
	err = s.pool.QueryRow(ctx, `INSERT INTO sync_runs (target,state,started_at,finished_at,window_from,window_through,providers) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, status.Target, status.State, status.StartedAt, status.FinishedAt, status.From, status.Through, providers).Scan(&id)
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
	tag, err := s.pool.Exec(ctx, `UPDATE sync_runs SET state=$2, finished_at=$3, providers=$4 WHERE id=$1`, id, status.State, status.FinishedAt, providers)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("update sync run failed")
	}
	return nil
}

func (s *PostgresRunStore) List(ctx context.Context, limit int) ([]Status, error) {
	if limit <= 0 || limit > historyLimit {
		limit = historyLimit
	}
	rows, err := s.pool.Query(ctx, `SELECT id,target,state,started_at,finished_at,window_from,window_through,providers FROM sync_runs ORDER BY started_at DESC,id DESC LIMIT $1`, limit)
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
	rows, err := tx.Query(ctx, `SELECT id,target,state,started_at,finished_at,window_from,window_through,providers FROM sync_runs WHERE state='running' FOR UPDATE`)
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
				status.Providers[provider] = ProviderStatus{State: ProviderFailed, ErrorCode: FailureCanceled}
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
		if _, err := tx.Exec(ctx, `UPDATE sync_runs SET state=$2,finished_at=$3,providers=$4 WHERE id=$1`, id, status.State, status.FinishedAt, providers); err != nil {
			return fmt.Errorf("reconcile sync runs failed")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("reconcile sync runs failed")
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

func scanStatus(row rowScanner) (Status, error) {
	var status Status
	var id int64
	var providers []byte
	var from, through time.Time
	if err := row.Scan(&id, &status.Target, &status.State, &status.StartedAt, &status.FinishedAt, &from, &through, &providers); err != nil {
		return Status{}, fmt.Errorf("read sync run failed")
	}
	if err := json.Unmarshal(providers, &status.Providers); err != nil {
		return Status{}, fmt.Errorf("decode sync run failed")
	}
	status.ID = strconv.FormatInt(id, 10)
	status.From = from.Format("2006-01-02")
	status.Through = through.Format("2006-01-02")
	return status, nil
}

func persistenceValues(status Status) (int64, []byte, error) {
	id, err := strconv.ParseInt(status.ID, 10, 64)
	if err != nil || id <= 0 {
		return 0, nil, fmt.Errorf("invalid sync run id")
	}
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
