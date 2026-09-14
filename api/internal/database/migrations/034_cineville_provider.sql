-- Keep legacy provider expressions intact while adding canonical int64 identities.
CREATE FUNCTION cineville_decimal_valid(value text, signed boolean) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT CASE WHEN length(value) <= 20 AND value ~ CASE WHEN signed THEN '^-?[1-9][0-9]*$' ELSE '^[1-9][0-9]*$' END
        THEN value::numeric BETWEEN CASE WHEN signed THEN -9223372036854775808::numeric ELSE 1::numeric END AND 9223372036854775807::numeric
        ELSE false END
$$;

DO $$
DECLARE
    item record;
    previous_expression text;
BEGIN
    FOR item IN SELECT * FROM (VALUES
        ('schedule_snapshot', 'schedule_snapshot_provider_check', 'provider = ''cineville'''),
        ('provider_snapshots', 'provider_snapshots_provider_check', 'provider = ''cineville'''),
        ('theaters', 'theaters_provider_check', 'provider = ''cineville'''),
        ('movies', 'movies_provider_check', 'provider = ''cineville'''),
        ('showtimes', 'showtimes_provider_check', 'provider = ''cineville'''),
        ('movie_matches', 'movie_matches_source_provider_check', 'source_provider = ''cineville'''),
        ('local_movie_groups', 'local_movie_groups_primary_source_provider_check', 'primary_source_provider = ''cineville'''),
        ('local_movie_group_members', 'local_movie_group_members_source_provider_check', 'source_provider = ''cineville'''),
        ('public_movies', 'public_movies_identity_anchor_provider_check', 'identity_anchor_provider = ''cineville'''),
        ('public_movie_sources', 'public_movie_sources_source_provider_check', 'source_provider = ''cineville'''),
        ('theater_locations', 'theater_locations_provider_check', 'provider = ''cineville'''),
        ('sync_runs', 'sync_runs_target_check', 'target = ''cineville'''),
        ('sync_schedules', 'sync_schedules_target_check', 'target = ''cineville'''),
        ('theaters', 'theaters_address_check', 'provider = ''cineville'''),
        ('theaters', 'theaters_provider_identity_check', 'provider = ''cineville'' AND cineville_decimal_valid(provider_id, false)'),
        ('showtimes', 'showtimes_provider_identity_check', 'provider = ''cineville'' AND provider_showing_id ~ ''^[1-9][0-9]*-[1-9][0-9]*$'' AND cineville_decimal_valid(split_part(provider_showing_id, ''-'', 1), false) AND cineville_decimal_valid(split_part(provider_showing_id, ''-'', 2), false) AND theater_id = ''cineville-'' || split_part(provider_showing_id, ''-'', 1)'),
        ('movie_slug_aliases', 'movie_slug_aliases_check', 'alias_kind = ''source'' AND source_provider = ''cineville'' AND source_movie_id IS NOT NULL'),
        ('sync_runs', 'sync_runs_trigger_occurrence_check', 'trigger_source = ''scheduled'' AND target = ''cineville'' AND schedule_id IS NOT NULL AND schedule_id > 0 AND schedule_revision IS NOT NULL AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2')
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
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((%s) OR (%I = ''cineville'' AND cineville_decimal_valid(%I, true)))',
            item.table_name, item.constraint_name, previous_expression, item.provider_column, item.id_column);
    END LOOP;

    SELECT pg_get_expr(conbin, conrelid) INTO STRICT previous_expression
    FROM pg_constraint WHERE conrelid = 'showtimes'::regclass AND conname = 'showtimes_time_check' AND contype = 'c';
    ALTER TABLE showtimes DROP CONSTRAINT showtimes_time_check;
    EXECUTE format('ALTER TABLE showtimes ADD CONSTRAINT showtimes_time_check CHECK ((provider <> ''cineville'' AND (%s)) OR (provider = ''cineville'' AND end_time = start_time))', previous_expression);
END $$;

ALTER TABLE movie_slug_aliases ADD CONSTRAINT movie_slug_aliases_cineville_shape_check CHECK (
    source_provider IS DISTINCT FROM 'cineville' OR (alias_kind = 'source' AND source_movie_id IS NOT NULL AND slug = 'cineville-film-' || source_movie_id)
);
ALTER TABLE theater_locations ADD CONSTRAINT theater_locations_cineville_identity_check CHECK (
    provider <> 'cineville' OR cineville_decimal_valid(provider_theater_id, false)
);
