-- Coupled to https://raw.githubusercontent.com/umami-software/umami/v3.3.1/prisma/schema.prisma.
-- Review this guard and every deletion before changing UMAMI_IMAGE.
BEGIN;
SET LOCAL lock_timeout = '30s';
SET LOCAL statement_timeout = '30min';
SET LOCAL search_path = pg_catalog, public;

DO $retention_guard$
DECLARE
    expected_tables constant text[] := ARRAY[
        'app_setting',
        'board',
        'event_data',
        'heatmap_event',
        'link',
        'pixel',
        'report',
        'revenue',
        'segment',
        'session',
        'session_data',
        'session_link',
        'session_replay',
        'session_replay_saved',
        'share',
        'team',
        'team_user',
        'two_factor_auth',
        'two_factor_backup_code',
        'two_factor_otp_used',
        'two_factor_rate_limit',
        'user',
        'website',
        'website_event'
    ];
    present_tables text[];
    invalid_columns integer;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('messeances:umami-retention:v3.3.1', 0));

    SELECT array_agg(table_name ORDER BY table_name)
    INTO present_tables
    FROM information_schema.tables
    WHERE table_schema = 'public'
      AND table_type = 'BASE TABLE'
      AND table_name <> '_prisma_migrations';

    IF present_tables IS DISTINCT FROM expected_tables THEN
        RAISE EXCEPTION 'Umami retention schema guard failed; expected v3.3.1';
    END IF;

    WITH expected(table_name, column_name, data_type, is_nullable) AS (
        VALUES
            ('session', 'session_id', 'uuid', 'NO'),
            ('session', 'created_at', 'timestamp with time zone', 'YES'),
            ('session_link', 'session_id', 'uuid', 'NO'),
            ('session_link', 'created_at', 'timestamp with time zone', 'YES'),
            ('website_event', 'event_id', 'uuid', 'NO'),
            ('website_event', 'session_id', 'uuid', 'NO'),
            ('website_event', 'created_at', 'timestamp with time zone', 'YES'),
            ('event_data', 'event_data_id', 'uuid', 'NO'),
            ('event_data', 'website_event_id', 'uuid', 'NO'),
            ('event_data', 'created_at', 'timestamp with time zone', 'YES'),
            ('session_data', 'session_data_id', 'uuid', 'NO'),
            ('session_data', 'session_id', 'uuid', 'NO'),
            ('session_data', 'created_at', 'timestamp with time zone', 'YES'),
            ('revenue', 'revenue_id', 'uuid', 'NO'),
            ('revenue', 'session_id', 'uuid', 'NO'),
            ('revenue', 'created_at', 'timestamp with time zone', 'YES'),
            ('session_replay', 'replay_id', 'uuid', 'NO'),
            ('session_replay', 'session_id', 'uuid', 'NO'),
            ('session_replay', 'created_at', 'timestamp with time zone', 'YES'),
            ('session_replay_saved', 'saved_replay_id', 'uuid', 'NO'),
            ('session_replay_saved', 'created_at', 'timestamp with time zone', 'YES'),
            ('heatmap_event', 'heatmap_event_id', 'uuid', 'NO'),
            ('heatmap_event', 'session_id', 'uuid', 'NO'),
            ('heatmap_event', 'created_at', 'timestamp with time zone', 'NO')
    )
    SELECT count(*)
    INTO invalid_columns
    FROM expected
    LEFT JOIN information_schema.columns actual
      ON actual.table_schema = 'public'
     AND actual.table_name = expected.table_name
     AND actual.column_name = expected.column_name
     AND actual.data_type = expected.data_type
     AND actual.is_nullable = expected.is_nullable
    WHERE actual.column_name IS NULL;

    IF invalid_columns <> 0 THEN
        RAISE EXCEPTION 'Umami retention schema guard failed; expected v3.3.1';
    END IF;
END
$retention_guard$;

WITH cutoff AS (
    SELECT CURRENT_TIMESTAMP - INTERVAL '25 months' AS before
)
DELETE FROM event_data data
USING cutoff
WHERE data.created_at IS NULL
   OR data.created_at <= cutoff.before
   OR EXISTS (
       SELECT 1
       FROM website_event event
       WHERE event.event_id = data.website_event_id
         AND (
             event.created_at IS NULL
             OR event.created_at <= cutoff.before
             OR EXISTS (
                 SELECT 1
                 FROM session audience_session
                 WHERE audience_session.session_id = event.session_id
                   AND (audience_session.created_at IS NULL OR audience_session.created_at <= cutoff.before)
             )
         )
   );

WITH cutoff AS (
    SELECT CURRENT_TIMESTAMP - INTERVAL '25 months' AS before
)
DELETE FROM website_event event
USING cutoff
WHERE event.created_at IS NULL
   OR event.created_at <= cutoff.before
   OR EXISTS (
       SELECT 1
       FROM session audience_session
       WHERE audience_session.session_id = event.session_id
         AND (audience_session.created_at IS NULL OR audience_session.created_at <= cutoff.before)
   );

WITH cutoff AS (
    SELECT CURRENT_TIMESTAMP - INTERVAL '25 months' AS before
)
DELETE FROM session_data data
USING cutoff
WHERE data.created_at IS NULL
   OR data.created_at <= cutoff.before
   OR EXISTS (
       SELECT 1
       FROM session audience_session
       WHERE audience_session.session_id = data.session_id
         AND (audience_session.created_at IS NULL OR audience_session.created_at <= cutoff.before)
   );

WITH cutoff AS (
    SELECT CURRENT_TIMESTAMP - INTERVAL '25 months' AS before
)
DELETE FROM revenue audience_revenue
USING cutoff
WHERE audience_revenue.created_at IS NULL
   OR audience_revenue.created_at <= cutoff.before
   OR EXISTS (
       SELECT 1
       FROM session audience_session
       WHERE audience_session.session_id = audience_revenue.session_id
         AND (audience_session.created_at IS NULL OR audience_session.created_at <= cutoff.before)
   );

WITH cutoff AS (
    SELECT CURRENT_TIMESTAMP - INTERVAL '25 months' AS before
)
DELETE FROM session_replay replay
USING cutoff
WHERE replay.created_at IS NULL
   OR replay.created_at <= cutoff.before
   OR EXISTS (
       SELECT 1
       FROM session audience_session
       WHERE audience_session.session_id = replay.session_id
         AND (audience_session.created_at IS NULL OR audience_session.created_at <= cutoff.before)
   );

WITH cutoff AS (
    SELECT CURRENT_TIMESTAMP - INTERVAL '25 months' AS before
)
DELETE FROM session_replay_saved saved
USING cutoff
WHERE saved.created_at IS NULL
   OR saved.created_at <= cutoff.before;

WITH cutoff AS (
    SELECT CURRENT_TIMESTAMP - INTERVAL '25 months' AS before
)
DELETE FROM heatmap_event heatmap
USING cutoff
WHERE heatmap.created_at <= cutoff.before
   OR EXISTS (
       SELECT 1
       FROM session audience_session
       WHERE audience_session.session_id = heatmap.session_id
         AND (audience_session.created_at IS NULL OR audience_session.created_at <= cutoff.before)
   );

WITH cutoff AS (
    SELECT CURRENT_TIMESTAMP - INTERVAL '25 months' AS before
)
DELETE FROM session_link link
USING cutoff
WHERE link.created_at IS NULL
   OR link.created_at <= cutoff.before
   OR EXISTS (
       SELECT 1
       FROM session audience_session
       WHERE audience_session.session_id = link.session_id
         AND (audience_session.created_at IS NULL OR audience_session.created_at <= cutoff.before)
   );

WITH cutoff AS (
    SELECT CURRENT_TIMESTAMP - INTERVAL '25 months' AS before
)
DELETE FROM session audience_session
USING cutoff
WHERE audience_session.created_at IS NULL
   OR audience_session.created_at <= cutoff.before;

COMMIT;
