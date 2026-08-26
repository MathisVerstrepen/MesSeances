CREATE TABLE theater_geocoding_runs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    state text NOT NULL CHECK (state IN ('running', 'succeeded', 'failed')),
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    summary jsonb,
    error_code text CHECK (error_code IS NULL OR error_code IN ('run_failed', 'canceled', 'internal_failure')),
    CHECK (
        (state = 'running' AND finished_at IS NULL AND summary IS NULL AND error_code IS NULL) OR
        (state = 'succeeded' AND finished_at IS NOT NULL AND jsonb_typeof(summary) = 'object' AND error_code IS NULL) OR
        (state = 'failed' AND finished_at IS NOT NULL AND (summary IS NULL OR jsonb_typeof(summary) = 'object') AND error_code IS NOT NULL)
    )
);

CREATE INDEX theater_geocoding_runs_latest_idx
    ON theater_geocoding_runs (started_at DESC, id DESC);

CREATE UNIQUE INDEX theater_geocoding_runs_one_running_idx
    ON theater_geocoding_runs ((state))
    WHERE state = 'running';
