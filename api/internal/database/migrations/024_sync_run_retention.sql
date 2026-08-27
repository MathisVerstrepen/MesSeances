CREATE INDEX sync_runs_terminal_retention_idx
    ON sync_runs (finished_at)
    WHERE state IN ('succeeded', 'failed') AND finished_at IS NOT NULL;

DELETE FROM sync_runs
WHERE state IN ('succeeded', 'failed')
  AND finished_at IS NOT NULL
  AND finished_at <= CURRENT_TIMESTAMP - INTERVAL '30 days';
