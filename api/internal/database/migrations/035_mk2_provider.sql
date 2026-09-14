-- Extend existing expressions without changing the validity of legacy providers.
CREATE FUNCTION mk2_identity_valid(kind text, value text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT CASE kind
        WHEN 'theater' THEN length(value) <= 124 AND value ~ '^[0-9]+$' AND value ~ '[1-9]'
        WHEN 'movie' THEN length(value) <= 119 AND value ~ '^HO[0-9]+$'
        WHEN 'showing' THEN length(value) <= 116 AND value ~ '^[0-9]+-[1-9][0-9]*$' AND split_part(value, '-', 1) ~ '[1-9]'
        ELSE false END
$$;

DO $$
DECLARE
    item record;
    previous_expression text;
BEGIN
    FOR item IN SELECT * FROM (VALUES
        ('schedule_snapshot', 'schedule_snapshot_provider_check', 'provider = ''mk2'''),
        ('provider_snapshots', 'provider_snapshots_provider_check', 'provider = ''mk2'''),
        ('theaters', 'theaters_provider_check', 'provider = ''mk2'''),
        ('movies', 'movies_provider_check', 'provider = ''mk2'''),
        ('showtimes', 'showtimes_provider_check', 'provider = ''mk2'''),
        ('movie_matches', 'movie_matches_source_provider_check', 'source_provider = ''mk2'''),
        ('local_movie_groups', 'local_movie_groups_primary_source_provider_check', 'primary_source_provider = ''mk2'''),
        ('local_movie_group_members', 'local_movie_group_members_source_provider_check', 'source_provider = ''mk2'''),
        ('public_movies', 'public_movies_identity_anchor_provider_check', 'identity_anchor_provider = ''mk2'''),
        ('public_movie_sources', 'public_movie_sources_source_provider_check', 'source_provider = ''mk2'''),
        ('theater_locations', 'theater_locations_provider_check', 'provider = ''mk2'''),
        ('sync_runs', 'sync_runs_target_check', 'target = ''mk2'''),
        ('sync_schedules', 'sync_schedules_target_check', 'target = ''mk2'''),
        ('theaters', 'theaters_provider_identity_check', 'provider = ''mk2'' AND mk2_identity_valid(''theater'', provider_id)'),
        ('showtimes', 'showtimes_provider_identity_check', 'provider = ''mk2'' AND mk2_identity_valid(''showing'', provider_showing_id) AND theater_id = ''mk2-'' || split_part(provider_showing_id, ''-'', 1)'),
        ('showtimes', 'showtimes_language_check', 'provider = ''mk2'' AND language = '''' AND provider_version = ''Muet'''),
        ('movie_slug_aliases', 'movie_slug_aliases_check', 'alias_kind = ''source'' AND source_provider = ''mk2'' AND source_movie_id IS NOT NULL'),
        ('sync_runs', 'sync_runs_trigger_occurrence_check', 'trigger_source = ''scheduled'' AND target = ''mk2'' AND schedule_id IS NOT NULL AND schedule_id > 0 AND schedule_revision IS NOT NULL AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2')
    ) AS checks(table_name, constraint_name, addition)
    LOOP
        SELECT pg_get_expr(conbin, conrelid) INTO STRICT previous_expression
        FROM pg_constraint WHERE conrelid = item.table_name::regclass AND conname = item.constraint_name AND contype = 'c';
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I', item.table_name, item.constraint_name);
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((%s) OR (%s))', item.table_name, item.constraint_name, previous_expression, item.addition);
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
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((%s) OR (%I = ''mk2'' AND mk2_identity_valid(''movie'', %I)))',
            item.table_name, item.constraint_name, previous_expression, item.provider_column, item.id_column);
    END LOOP;

    SELECT pg_get_expr(conbin, conrelid) INTO STRICT previous_expression
    FROM pg_constraint WHERE conrelid = 'showtimes'::regclass AND conname = 'showtimes_time_check' AND contype = 'c';
    ALTER TABLE showtimes DROP CONSTRAINT showtimes_time_check;
    EXECUTE format('ALTER TABLE showtimes ADD CONSTRAINT showtimes_time_check CHECK ((provider <> ''mk2'' AND (%s)) OR (provider = ''mk2'' AND end_time = start_time))', previous_expression);
END $$;

ALTER TABLE showtimes ADD CONSTRAINT showtimes_mk2_shape_check CHECK (
    provider <> 'mk2' OR (room = '' AND first_part_duration_minutes = 0 AND (provider_version <> 'Muet' OR language = ''))
);
ALTER TABLE movie_slug_aliases ADD CONSTRAINT movie_slug_aliases_mk2_shape_check CHECK (
    source_provider IS DISTINCT FROM 'mk2' OR (alias_kind = 'source' AND source_movie_id IS NOT NULL AND slug = 'mk2-film-' || source_movie_id)
);
ALTER TABLE theater_locations ADD CONSTRAINT theater_locations_mk2_identity_check CHECK (
    provider <> 'mk2' OR mk2_identity_valid('theater', provider_theater_id)
);
