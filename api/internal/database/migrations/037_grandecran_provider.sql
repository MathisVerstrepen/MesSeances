-- Preserve legacy-provider predicates, including Cinewest, while admitting Grand Ecran identities.
CREATE FUNCTION grandecran_identity_valid(kind text, value text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT CASE kind
        WHEN 'theater' THEN value ~ '^[A-Z0-9]{5}$'
        WHEN 'movie' THEN value ~ '^([1-9][0-9]{0,111}|c[A-Za-z0-9_-]{1,111})$'
        WHEN 'showing' THEN value ~ '^[A-Z0-9]{5}-[a-f0-9]{64}$'
        ELSE false END
$$;

DO $$
DECLARE
    item record;
    previous_expression text;
BEGIN
    FOR item IN SELECT * FROM (VALUES
        ('schedule_snapshot', 'schedule_snapshot_provider_check', 'provider = ''grandecran'''),
        ('provider_snapshots', 'provider_snapshots_provider_check', 'provider = ''grandecran'''),
        ('theaters', 'theaters_provider_check', 'provider = ''grandecran'''),
        ('movies', 'movies_provider_check', 'provider = ''grandecran'''),
        ('showtimes', 'showtimes_provider_check', 'provider = ''grandecran'''),
        ('movie_matches', 'movie_matches_source_provider_check', 'source_provider = ''grandecran'''),
        ('local_movie_groups', 'local_movie_groups_primary_source_provider_check', 'primary_source_provider = ''grandecran'''),
        ('local_movie_group_members', 'local_movie_group_members_source_provider_check', 'source_provider = ''grandecran'''),
        ('public_movies', 'public_movies_identity_anchor_provider_check', 'identity_anchor_provider = ''grandecran'''),
        ('public_movie_sources', 'public_movie_sources_source_provider_check', 'source_provider = ''grandecran'''),
        ('theater_locations', 'theater_locations_provider_check', 'provider = ''grandecran'''),
        ('sync_runs', 'sync_runs_target_check', 'target = ''grandecran'''),
        ('sync_schedules', 'sync_schedules_target_check', 'target = ''grandecran'''),
        ('theaters', 'theaters_provider_identity_check', 'provider = ''grandecran'' AND grandecran_identity_valid(''theater'', provider_id)'),
        ('showtimes', 'showtimes_provider_identity_check', 'provider = ''grandecran'' AND grandecran_identity_valid(''showing'', provider_showing_id) AND theater_id = ''grandecran-'' || split_part(provider_showing_id, ''-'', 1)'),
        ('movie_slug_aliases', 'movie_slug_aliases_check', 'alias_kind = ''source'' AND source_provider = ''grandecran'' AND source_movie_id IS NOT NULL'),
        ('sync_runs', 'sync_runs_trigger_occurrence_check', 'trigger_source = ''scheduled'' AND target = ''grandecran'' AND schedule_id IS NOT NULL AND schedule_id > 0 AND schedule_revision IS NOT NULL AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2')
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
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((%s) OR (%I = ''grandecran'' AND grandecran_identity_valid(''movie'', %I)))',
            item.table_name, item.constraint_name, previous_expression, item.provider_column, item.id_column);
    END LOOP;

    SELECT pg_get_expr(conbin, conrelid) INTO STRICT previous_expression
    FROM pg_constraint WHERE conrelid = 'showtimes'::regclass AND conname = 'showtimes_time_check' AND contype = 'c';
    ALTER TABLE showtimes DROP CONSTRAINT showtimes_time_check;
    EXECUTE format('ALTER TABLE showtimes ADD CONSTRAINT showtimes_time_check CHECK ((provider <> ''grandecran'' AND (%s)) OR (provider = ''grandecran'' AND end_time = start_time))', previous_expression);
END $$;

ALTER TABLE showtimes ADD CONSTRAINT showtimes_grandecran_shape_check CHECK (
    provider <> 'grandecran' OR (first_part_duration_minutes = 0 AND theater_id = 'grandecran-' || split_part(provider_showing_id, '-', 1))
);
ALTER TABLE movie_slug_aliases ADD CONSTRAINT movie_slug_aliases_grandecran_shape_check CHECK (
    source_provider IS DISTINCT FROM 'grandecran' OR (alias_kind = 'source' AND source_movie_id IS NOT NULL AND slug = 'grandecran-film-' || source_movie_id)
);
ALTER TABLE public_movie_sources ADD CONSTRAINT public_movie_sources_grandecran_slug_check CHECK (
    source_provider <> 'grandecran' OR source_slug = 'grandecran-film-' || source_movie_id
);
ALTER TABLE theater_locations ADD CONSTRAINT theater_locations_grandecran_identity_check CHECK (
    provider <> 'grandecran' OR grandecran_identity_valid('theater', provider_theater_id)
);
