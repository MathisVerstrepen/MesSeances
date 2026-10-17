-- Add Véo without rewriting legacy rows, predicates, foreign keys or schedules.
CREATE FUNCTION veo_identity_valid(kind text, value text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT CASE kind
        WHEN 'theater' THEN value ~ '^([A-Z0-9]{5}|EMS1361)$'
        WHEN 'movie' THEN value ~ '^([1-9][0-9]{0,111}|c[A-Za-z0-9_-]{1,111}|ticketingcine-[A-Z0-9]{5})$'
        WHEN 'showing' THEN value ~ '^([A-Z0-9]{5}|EMS1361)-[a-f0-9]{64}$'
        ELSE false END
$$;

CREATE FUNCTION veo_booking_valid(value text, theater text, showing text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT length(value) <= 4096 AND veo_identity_valid('theater', theater)
        AND veo_identity_valid('showing', showing)
        AND split_part(showing, '-', 1) = theater
        AND CASE WHEN theater = 'EMS1361' THEN
            value IN ('https://castelginest.miracinemas.fr','https://castelginest.miracinemas.fr/')
            OR (value ~ '^https://castelginest\.miracinemas\.fr/?#showsession\?id=emsx1361[0-9]{8}$'
                AND showing = 'EMS1361-' || encode(sha256(convert_to('EMS1361','UTF8') || decode('00','hex') || convert_to(substring(value from '#showsession\?id=(emsx1361[0-9]{8})$'),'UTF8')), 'hex'))
        ELSE value ~ '^https://achat\.veocinemas\.fr/[a-z0-9]+(-[a-z0-9]+)*/r/[1-9][0-9]*$' END
$$;

DO $$
DECLARE
    item record;
    previous_expression text;
    target_table text;
BEGIN
    FOR item IN SELECT * FROM (VALUES
        ('schedule_snapshot', 'schedule_snapshot_provider_check', 'provider = ''veo'''),
        ('provider_snapshots', 'provider_snapshots_provider_check', 'provider = ''veo'''),
        ('theaters', 'theaters_provider_check', 'provider = ''veo'''),
        ('movies', 'movies_provider_check', 'provider = ''veo'''),
        ('showtimes', 'showtimes_provider_check', 'provider = ''veo'''),
        ('movie_matches', 'movie_matches_source_provider_check', 'source_provider = ''veo'''),
        ('local_movie_groups', 'local_movie_groups_primary_source_provider_check', 'primary_source_provider = ''veo'''),
        ('local_movie_group_members', 'local_movie_group_members_source_provider_check', 'source_provider = ''veo'''),
        ('public_movies', 'public_movies_identity_anchor_provider_check', 'identity_anchor_provider = ''veo'''),
        ('public_movie_sources', 'public_movie_sources_source_provider_check', 'source_provider = ''veo'''),
        ('theater_locations', 'theater_locations_provider_check', 'provider = ''veo'''),
        ('sync_runs', 'sync_runs_target_check', 'target = ''veo'''),
        ('sync_schedules', 'sync_schedules_target_check', 'target = ''veo'''),
        ('theaters', 'theaters_provider_identity_check', 'provider = ''veo'' AND veo_identity_valid(''theater'', provider_id)'),
        ('showtimes', 'showtimes_provider_identity_check', 'provider = ''veo'' AND veo_identity_valid(''showing'', provider_showing_id) AND theater_id = ''veo-'' || split_part(provider_showing_id, ''-'', 1)'),
        ('showtimes', 'showtimes_check', 'provider = ''veo'' AND theater_id = ''veo-EMS1361'' AND first_part_duration_minutes >= 0'),
        ('movie_slug_aliases', 'movie_slug_aliases_check', 'alias_kind = ''source'' AND source_provider = ''veo'' AND source_movie_id IS NOT NULL'),
        ('sync_runs', 'sync_runs_trigger_occurrence_check', 'trigger_source = ''scheduled'' AND target = ''veo'' AND schedule_id IS NOT NULL AND schedule_id > 0 AND schedule_revision IS NOT NULL AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2'),
        ('screening_history_providers', 'screening_history_providers_provider_check', 'provider = ''veo'''),
        ('theater_images', 'theater_images_provider_check', 'provider = ''veo'''),
        ('cinema_activity_coverage', 'cinema_activity_coverage_provider_check', 'provider = ''veo'''),
        ('cinema_activity_coverage', 'cinema_activity_coverage_check', 'provider = ''veo'' AND theater_id <> ''veo-EMS1361'' AND status = ''complete'' AND basis = ''date_response''')
    ) AS checks(table_name, constraint_name, addition)
    LOOP
        FOR target_table IN SELECT item.table_name UNION ALL
            SELECT 'screening_history_' || item.table_name WHERE item.table_name IN ('theaters','showtimes')
        LOOP
            SELECT pg_get_expr(conbin, conrelid) INTO STRICT previous_expression
            FROM pg_constraint WHERE conrelid = target_table::regclass AND conname = item.constraint_name AND contype = 'c';
            EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I', target_table, item.constraint_name);
            EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((%s) OR (%s))', target_table, item.constraint_name, previous_expression, item.addition);
        END LOOP;
    END LOOP;

    FOR item IN SELECT * FROM (VALUES
        ('movies', 'movies_provider_identity_check', 'provider', 'provider_id'),
        ('movie_matches', 'movie_matches_source_movie_id_check', 'source_provider', 'source_movie_id'),
        ('local_movie_groups', 'local_movie_groups_check', 'primary_source_provider', 'primary_source_movie_id'),
        ('local_movie_group_members', 'local_movie_group_members_check', 'source_provider', 'source_movie_id'),
        ('public_movies', 'public_movies_check1', 'identity_anchor_provider', 'identity_anchor_source_movie_id'),
        ('public_movie_sources', 'public_movie_sources_check', 'source_provider', 'source_movie_id'),
        ('movie_slug_aliases', 'movie_slug_aliases_check1', 'source_provider', 'source_movie_id')
    ) AS checks(table_name, constraint_name, provider_column, id_column)
    LOOP
        SELECT pg_get_expr(conbin, conrelid) INTO STRICT previous_expression
        FROM pg_constraint WHERE conrelid = item.table_name::regclass AND conname = item.constraint_name AND contype = 'c';
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I', item.table_name, item.constraint_name);
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((%s) OR (%I = ''veo'' AND veo_identity_valid(''movie'', %I)))',
            item.table_name, item.constraint_name, previous_expression, item.provider_column, item.id_column);
    END LOOP;

    FOREACH target_table IN ARRAY ARRAY['showtimes','screening_history_showtimes'] LOOP
        SELECT pg_get_expr(conbin, conrelid) INTO STRICT previous_expression
        FROM pg_constraint WHERE conrelid = target_table::regclass AND conname = 'showtimes_time_check' AND contype = 'c';
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_time_check', target_table);
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT showtimes_time_check CHECK ((provider <> ''veo'' AND (%s)) OR (provider = ''veo'' AND end_time >= start_time))', target_table, previous_expression);
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT showtimes_veo_shape_check CHECK (provider <> ''veo'' OR (
            theater_id = ''veo-'' || split_part(provider_showing_id, ''-'', 1)
            AND veo_identity_valid(''movie'', movie_provider_id)
            AND ((theater_id = ''veo-EMS1361'') = (movie_provider_id LIKE ''ticketingcine-%%''))
            AND (theater_id = ''veo-EMS1361'' OR first_part_duration_minutes = 0)
            AND veo_booking_valid(booking_url, split_part(provider_showing_id, ''-'', 1), provider_showing_id)))', target_table);
    END LOOP;
END $$;

ALTER TABLE movie_slug_aliases ADD CONSTRAINT movie_slug_aliases_veo_shape_check CHECK (
    source_provider IS DISTINCT FROM 'veo' OR (alias_kind = 'source' AND source_movie_id IS NOT NULL AND slug = 'veo-film-' || source_movie_id)
);
ALTER TABLE public_movie_sources ADD CONSTRAINT public_movie_sources_veo_slug_check CHECK (
    source_provider <> 'veo' OR source_slug = 'veo-film-' || source_movie_id
);
ALTER TABLE theater_locations ADD CONSTRAINT theater_locations_veo_identity_check CHECK (
    provider <> 'veo' OR veo_identity_valid('theater', provider_theater_id)
);
ALTER TABLE theater_images ADD CONSTRAINT theater_images_veo_identity_check CHECK (
    provider <> 'veo' OR veo_identity_valid('theater', provider_theater_id)
);
