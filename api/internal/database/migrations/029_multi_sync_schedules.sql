ALTER TABLE sync_schedules DROP CONSTRAINT sync_schedules_provider_check;
ALTER TABLE sync_schedules DROP CONSTRAINT sync_schedules_pkey;
ALTER TABLE sync_schedules RENAME COLUMN provider TO target;
ALTER TABLE sync_schedules
    ADD COLUMN id bigint GENERATED ALWAYS AS IDENTITY,
    ADD CONSTRAINT sync_schedules_pkey PRIMARY KEY (id),
    ADD CONSTRAINT sync_schedules_target_check CHECK (target IN ('ugc', 'kinepolis', 'pathe', 'cgr', 'tmdb_metadata_refresh'));

ALTER TABLE sync_runs ADD COLUMN schedule_id bigint;

UPDATE sync_runs AS runs
SET schedule_id = schedules.id
FROM sync_schedules AS schedules
WHERE runs.trigger_source = 'scheduled'
  AND runs.target = schedules.target;

DROP INDEX sync_runs_scheduled_occurrence_attempt_idx;
ALTER TABLE sync_runs DROP CONSTRAINT sync_runs_trigger_occurrence_check;
ALTER TABLE sync_runs ADD CONSTRAINT sync_runs_trigger_occurrence_check CHECK (
    (trigger_source = 'manual'
        AND schedule_id IS NULL
        AND schedule_revision IS NULL
        AND scheduled_for IS NULL
        AND schedule_attempt IS NULL)
    OR
    (trigger_source = 'scheduled'
        AND target IN ('ugc', 'kinepolis', 'pathe', 'cgr')
        AND schedule_id > 0
        AND schedule_revision > 0
        AND scheduled_for IS NOT NULL
        AND schedule_attempt BETWEEN 0 AND 2)
);

CREATE UNIQUE INDEX sync_runs_scheduled_occurrence_attempt_idx
    ON sync_runs (schedule_id, schedule_revision, scheduled_for, schedule_attempt)
    WHERE trigger_source = 'scheduled';

CREATE TABLE sync_schedule_occurrence_claims (
    schedule_id bigint PRIMARY KEY REFERENCES sync_schedules(id) ON DELETE CASCADE,
    schedule_revision bigint NOT NULL CHECK (schedule_revision > 0),
    scheduled_for timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
