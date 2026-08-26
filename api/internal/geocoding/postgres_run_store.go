package geocoding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RunState string
type RunFailureCode string

const (
	RunStateRunning   RunState = "running"
	RunStateSucceeded RunState = "succeeded"
	RunStateFailed    RunState = "failed"

	RunFailureFailed   RunFailureCode = "run_failed"
	RunFailureCanceled RunFailureCode = "canceled"
	RunFailureInternal RunFailureCode = "internal_failure"
)

type RunSummary struct {
	Selected  int `json:"selected"`
	Skipped   int `json:"skipped"`
	Matched   int `json:"matched"`
	Ambiguous int `json:"ambiguous"`
	NotFound  int `json:"not_found"`
	Failed    int `json:"failed"`
	Written   int `json:"written"`
}

type RunStatus struct {
	ID         string          `json:"id"`
	State      RunState        `json:"state"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
	Summary    *RunSummary     `json:"summary"`
	ErrorCode  *RunFailureCode `json:"error_code"`
}

type RunStore interface {
	Create(context.Context, RunStatus) (RunStatus, error)
	Finish(context.Context, RunStatus) error
	Snapshot(context.Context) (*RunStatus, error)
	ReconcileRunning(context.Context, time.Time) error
}

type PostgresRunStore struct{ pool *pgxpool.Pool }

func NewPostgresRunStore(pool *pgxpool.Pool) *PostgresRunStore {
	return &PostgresRunStore{pool: pool}
}

func (s *PostgresRunStore) Create(ctx context.Context, status RunStatus) (RunStatus, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO theater_geocoding_runs (state,started_at)
VALUES ('running',$1) ON CONFLICT DO NOTHING RETURNING id`, status.StartedAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return RunStatus{}, ErrRunInProgress
	}
	if err != nil {
		return RunStatus{}, fmt.Errorf("create theater geocoding run failed")
	}
	return RunStatus{ID: strconv.FormatInt(id, 10), State: RunStateRunning, StartedAt: status.StartedAt.UTC()}, nil
}

func (s *PostgresRunStore) Finish(ctx context.Context, status RunStatus) error {
	id, err := strconv.ParseInt(status.ID, 10, 64)
	if err != nil || id <= 0 {
		return fmt.Errorf("finish theater geocoding run failed")
	}
	var summary any
	if status.Summary != nil {
		summary, err = json.Marshal(status.Summary)
		if err != nil {
			return fmt.Errorf("finish theater geocoding run failed")
		}
	}
	var errorCode any
	if status.ErrorCode != nil {
		errorCode = *status.ErrorCode
	}
	tag, err := s.pool.Exec(ctx, `UPDATE theater_geocoding_runs
SET state=$2,finished_at=$3,summary=$4,error_code=$5
WHERE id=$1 AND state='running'`, id, status.State, status.FinishedAt, summary, errorCode)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("finish theater geocoding run failed")
	}
	return nil
}

func (s *PostgresRunStore) Snapshot(ctx context.Context) (*RunStatus, error) {
	row := s.pool.QueryRow(ctx, `SELECT id,state,started_at,finished_at,summary,error_code
FROM theater_geocoding_runs
ORDER BY (state='running') DESC,started_at DESC,id DESC
LIMIT 1`)
	status, err := scanRunStatus(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read theater geocoding run failed")
	}
	return &status, nil
}

func (s *PostgresRunStore) ReconcileRunning(ctx context.Context, finishedAt time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE theater_geocoding_runs
SET state='failed',finished_at=$1,summary=NULL,error_code='canceled'
WHERE state='running'`, finishedAt.UTC())
	if err != nil {
		return fmt.Errorf("reconcile theater geocoding runs failed")
	}
	return nil
}

type runRowScanner interface{ Scan(...any) error }

func scanRunStatus(row runRowScanner) (RunStatus, error) {
	var status RunStatus
	var id int64
	var summary []byte
	var errorCode *string
	if err := row.Scan(&id, &status.State, &status.StartedAt, &status.FinishedAt, &summary, &errorCode); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RunStatus{}, pgx.ErrNoRows
		}
		return RunStatus{}, fmt.Errorf("read theater geocoding run failed")
	}
	status.ID = strconv.FormatInt(id, 10)
	status.StartedAt = status.StartedAt.UTC()
	if status.FinishedAt != nil {
		finished := status.FinishedAt.UTC()
		status.FinishedAt = &finished
	}
	if summary != nil {
		decoded, err := decodeRunSummary(summary)
		if err != nil {
			return RunStatus{}, err
		}
		status.Summary = &decoded
	}
	if errorCode != nil {
		code := RunFailureCode(*errorCode)
		status.ErrorCode = &code
	}
	return status, nil
}

func decodeRunSummary(value []byte) (RunSummary, error) {
	var summary RunSummary
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&summary); err != nil {
		return RunSummary{}, fmt.Errorf("decode theater geocoding run failed")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return RunSummary{}, fmt.Errorf("decode theater geocoding run failed")
	}
	return summary, nil
}

func cloneRunStatus(status RunStatus) RunStatus {
	if status.FinishedAt != nil {
		finished := *status.FinishedAt
		status.FinishedAt = &finished
	}
	if status.Summary != nil {
		summary := *status.Summary
		status.Summary = &summary
	}
	if status.ErrorCode != nil {
		code := *status.ErrorCode
		status.ErrorCode = &code
	}
	return status
}
