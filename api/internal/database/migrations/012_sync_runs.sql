CREATE TABLE sync_runs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    target text NOT NULL CHECK (target IN ('all', 'ugc', 'kinepolis')),
    state text NOT NULL CHECK (state IN ('running', 'succeeded', 'failed')),
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    window_from date NOT NULL,
    window_through date NOT NULL,
    providers jsonb NOT NULL CHECK (jsonb_typeof(providers) = 'object'),
    CHECK (window_through >= window_from),
    CHECK ((state = 'running' AND finished_at IS NULL) OR (state <> 'running' AND finished_at IS NOT NULL))
);

CREATE INDEX sync_runs_latest_idx ON sync_runs (started_at DESC, id DESC);
