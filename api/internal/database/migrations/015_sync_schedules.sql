CREATE TABLE sync_schedules (
    provider text PRIMARY KEY CHECK (provider IN ('ugc', 'kinepolis')),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    enabled boolean NOT NULL,
    schedule_kind text NOT NULL CHECK (schedule_kind IN ('daily', 'weekly', 'cron')),
    local_time text,
    weekdays text[],
    cron_expression varchar(255),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sync_schedules_definition_check CHECK (
        (schedule_kind = 'daily'
            AND local_time IS NOT NULL
            AND local_time ~ '^(?:[01][0-9]|2[0-3]):[0-5][0-9]$'
            AND weekdays IS NULL
            AND cron_expression IS NULL)
        OR
        (schedule_kind = 'weekly'
            AND local_time IS NOT NULL
            AND local_time ~ '^(?:[01][0-9]|2[0-3]):[0-5][0-9]$'
            AND weekdays IS NOT NULL
            AND cardinality(weekdays) > 0
            AND weekdays <@ ARRAY['mon','tue','wed','thu','fri','sat','sun']::text[]
            AND cron_expression IS NULL)
        OR
        (schedule_kind = 'cron'
            AND local_time IS NULL
            AND weekdays IS NULL
            AND cron_expression IS NOT NULL
            AND btrim(cron_expression) <> '')
    )
);

ALTER TABLE sync_runs
    ADD COLUMN trigger_source text NOT NULL DEFAULT 'manual',
    ADD COLUMN schedule_revision bigint,
    ADD COLUMN scheduled_for timestamptz,
    ADD COLUMN schedule_attempt smallint,
    ADD CONSTRAINT sync_runs_trigger_occurrence_check CHECK (
        (trigger_source = 'manual'
            AND schedule_revision IS NULL
            AND scheduled_for IS NULL
            AND schedule_attempt IS NULL)
        OR
        (trigger_source = 'scheduled'
            AND target IN ('ugc', 'kinepolis')
            AND schedule_revision > 0
            AND scheduled_for IS NOT NULL
            AND schedule_attempt BETWEEN 0 AND 2)
    );

CREATE UNIQUE INDEX sync_runs_scheduled_occurrence_attempt_idx
    ON sync_runs (target, schedule_revision, scheduled_for, schedule_attempt)
    WHERE trigger_source = 'scheduled';
