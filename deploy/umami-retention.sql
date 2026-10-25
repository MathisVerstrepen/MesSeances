-- Compatibility is defined by the columns used below, not an Umami release.
-- Every public base table must have a reviewed retention classification.
BEGIN;
SET LOCAL lock_timeout = '30s';
SET LOCAL statement_timeout = '30min';
SET LOCAL search_path = pg_catalog, public;

DO $retention_guard$
DECLARE
    required_tables constant text[] := ARRAY[
        'event_data',
        'heatmap_event',
        'revenue',
        'session',
        'session_data',
        'session_link',
        'session_replay',
        'session_replay_saved',
        'website_event'
    ];
    -- These tables are preserved when present; cleanup does not depend on their
    -- existence or column layout. New tables must be classified before purging.
    preserved_tables constant text[] := ARRAY[
        '_prisma_migrations',
        'annotation',
        'api_key',
        'app_setting',
        'board',
        'link',
        'pixel',
        'report',
        'segment',
        'share',
        'team',
        'team_user',
        'two_factor_auth',
        'two_factor_backup_code',
        'two_factor_otp_used',
        'two_factor_rate_limit',
        'user',
        'website'
    ];
    present_tables text[];
    missing_tables text[];
    unclassified_tables text[];
    invalid_columns text[];
BEGIN
    -- Keep this key stable across compatible releases of both Umami and this SQL.
    PERFORM pg_advisory_xact_lock(hashtextextended('messeances:umami-retention', 0));

    SELECT coalesce(array_agg(table_name ORDER BY table_name), ARRAY[]::text[])
    INTO present_tables
    FROM information_schema.tables
    WHERE table_schema = 'public'
      AND table_type = 'BASE TABLE';

    SELECT array_agg(name ORDER BY name)
    INTO missing_tables
    FROM unnest(required_tables) AS required(name)
    WHERE NOT (name = ANY(present_tables));

    IF missing_tables IS NOT NULL THEN
        RAISE EXCEPTION 'Umami retention schema contract failed: missing required tables: %',
            array_to_string(missing_tables, ', ');
    END IF;

    SELECT array_agg(name ORDER BY name)
    INTO unclassified_tables
    FROM unnest(present_tables) AS present(name)
    WHERE NOT (name = ANY(required_tables || preserved_tables));

    IF unclassified_tables IS NOT NULL THEN
        RAISE EXCEPTION 'Umami retention schema contract failed: unclassified tables: %; review their retention policy',
            array_to_string(unclassified_tables, ', ');
    END IF;

    -- Prevent concurrent ALTER/DROP of the required tables between validation
    -- and deletion, while allowing normal analytics writes.
    LOCK TABLE public.event_data, public.heatmap_event, public.revenue,
        public.session, public.session_data, public.session_link,
        public.session_replay, public.session_replay_saved, public.website_event
        IN ROW EXCLUSIVE MODE;

    -- Only columns read by the purge are required. Extra columns and changes to
    -- unrelated columns are compatible. Both timestamp nullabilities are safe:
    -- every deletion explicitly handles a null retention timestamp.
    WITH expected(table_name, column_name, data_type) AS (
        VALUES
            ('session', 'session_id', 'uuid'),
            ('session', 'created_at', 'timestamp with time zone'),
            ('session_link', 'session_id', 'uuid'),
            ('session_link', 'created_at', 'timestamp with time zone'),
            ('website_event', 'event_id', 'uuid'),
            ('website_event', 'session_id', 'uuid'),
            ('website_event', 'created_at', 'timestamp with time zone'),
            ('event_data', 'website_event_id', 'uuid'),
            ('event_data', 'created_at', 'timestamp with time zone'),
            ('session_data', 'session_id', 'uuid'),
            ('session_data', 'created_at', 'timestamp with time zone'),
            ('revenue', 'session_id', 'uuid'),
            ('revenue', 'created_at', 'timestamp with time zone'),
            ('session_replay', 'session_id', 'uuid'),
            ('session_replay', 'created_at', 'timestamp with time zone'),
            ('session_replay_saved', 'created_at', 'timestamp with time zone'),
            ('heatmap_event', 'session_id', 'uuid'),
            ('heatmap_event', 'created_at', 'timestamp with time zone')
    )
    SELECT array_agg(
        format('%s.%s (expected %s, found %s)',
            expected.table_name, expected.column_name, expected.data_type,
            coalesce(actual.data_type, 'missing'))
        ORDER BY expected.table_name, expected.column_name
    )
    INTO invalid_columns
    FROM expected
    LEFT JOIN information_schema.columns actual
      ON actual.table_schema = 'public'
     AND actual.table_name = expected.table_name
     AND actual.column_name = expected.column_name
    WHERE actual.column_name IS NULL
       OR actual.data_type <> expected.data_type;

    IF invalid_columns IS NOT NULL THEN
        RAISE EXCEPTION 'Umami retention schema contract failed: incompatible columns: %',
            array_to_string(invalid_columns, ', ');
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
WHERE heatmap.created_at IS NULL
   OR heatmap.created_at <= cutoff.before
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
