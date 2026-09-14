-- Preserve legacy provider checks and add the three namespaced Cinewest sources.
CREATE FUNCTION cinewest_identity_valid(kind text, value text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT CASE kind
        WHEN 'theater' THEN value IN (
            'cineoffice-cognaclegalaxy', 'cineoffice-neverscinemazarin', 'cineoffice-mouanssartouxlastrada',
            'cineoffice-vitreaurore', 'cineoffice-mouginslesbalcons', 'cineoffice-royanlelido',
            'cineoffice-ploermelcinelac', 'cineoffice-saintesatlanticcine', 'cineoffice-aurillaclecristal',
            'ticketingcine-EMS1185', 'ticketingcine-EMS1317', 'ticketingcine-EMS0042', 'webediamovies-W8400')
        WHEN 'movie' THEN octet_length(value) <= 114 AND (
            value ~ '^(cineoffice-[1-9][0-9]*|ticketingcine-[A-Z0-9]{5}|webediamovies-[1-9][0-9]*)$'
            OR (value ~ '^ticketingcine-EMS[0-9]{4}-emsx[0-9]{4}HC[0-9]+$'
                AND split_part(value, '-', 2) IN ('EMS1185', 'EMS1317', 'EMS0042')
                AND substring(split_part(value, '-', 2) FROM 4) = substring(split_part(value, '-', 3) FROM 5 FOR 4)))
        WHEN 'showing' THEN value ~ '^(cineoffice|ticketingcine|webediamovies)-[a-f0-9]{64}$'
        ELSE false END
$$;

DO $$
DECLARE
    item record;
    previous_expression text;
BEGIN
    FOR item IN SELECT * FROM (VALUES
        ('schedule_snapshot', 'schedule_snapshot_provider_check', 'provider = ''cinewest'''),
        ('provider_snapshots', 'provider_snapshots_provider_check', 'provider = ''cinewest'''),
        ('theaters', 'theaters_provider_check', 'provider = ''cinewest'''),
        ('movies', 'movies_provider_check', 'provider = ''cinewest'''),
        ('showtimes', 'showtimes_provider_check', 'provider = ''cinewest'''),
        ('movie_matches', 'movie_matches_source_provider_check', 'source_provider = ''cinewest'''),
        ('local_movie_groups', 'local_movie_groups_primary_source_provider_check', 'primary_source_provider = ''cinewest'''),
        ('local_movie_group_members', 'local_movie_group_members_source_provider_check', 'source_provider = ''cinewest'''),
        ('public_movies', 'public_movies_identity_anchor_provider_check', 'identity_anchor_provider = ''cinewest'''),
        ('public_movie_sources', 'public_movie_sources_source_provider_check', 'source_provider = ''cinewest'''),
        ('theater_locations', 'theater_locations_provider_check', 'provider = ''cinewest'''),
        ('sync_runs', 'sync_runs_target_check', 'target = ''cinewest'''),
        ('sync_schedules', 'sync_schedules_target_check', 'target = ''cinewest'''),
        ('theaters', 'theaters_provider_identity_check', 'provider = ''cinewest'' AND cinewest_identity_valid(''theater'', provider_id)'),
        ('showtimes', 'showtimes_provider_identity_check', 'provider = ''cinewest'' AND cinewest_identity_valid(''showing'', provider_showing_id)'),
        ('showtimes', 'showtimes_language_check', 'provider = ''cinewest'' AND provider_showing_id LIKE ''cineoffice-%'' AND language = '''' AND provider_version = ''VERSION_MUET'''),
        ('showtimes', 'showtimes_check', 'provider = ''cinewest'' AND provider_showing_id LIKE ''ticketingcine-%'' AND first_part_duration_minutes >= 0'),
        ('movie_slug_aliases', 'movie_slug_aliases_check', 'alias_kind = ''source'' AND source_provider = ''cinewest'' AND source_movie_id IS NOT NULL'),
        ('sync_runs', 'sync_runs_trigger_occurrence_check', 'trigger_source = ''scheduled'' AND target = ''cinewest'' AND schedule_id IS NOT NULL AND schedule_id > 0 AND schedule_revision IS NOT NULL AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2')
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
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((%s) OR (%I = ''cinewest'' AND cinewest_identity_valid(''movie'', %I)))',
            item.table_name, item.constraint_name, previous_expression, item.provider_column, item.id_column);
    END LOOP;

    SELECT pg_get_expr(conbin, conrelid) INTO STRICT previous_expression
    FROM pg_constraint WHERE conrelid = 'showtimes'::regclass AND conname = 'showtimes_time_check' AND contype = 'c';
    ALTER TABLE showtimes DROP CONSTRAINT showtimes_time_check;
    EXECUTE format('ALTER TABLE showtimes ADD CONSTRAINT showtimes_time_check CHECK ((provider <> ''cinewest'' AND (%s)) OR (provider = ''cinewest'' AND CASE split_part(provider_showing_id, ''-'', 1) WHEN ''cineoffice'' THEN end_time > start_time WHEN ''ticketingcine'' THEN end_time >= start_time WHEN ''webediamovies'' THEN end_time = start_time ELSE false END))', previous_expression);
END $$;

ALTER TABLE showtimes ADD CONSTRAINT showtimes_cinewest_shape_check CHECK (
    provider <> 'cinewest' OR (
        btrim(room) <> '' AND split_part(theater_id, '-', 2) = split_part(provider_showing_id, '-', 1)
        AND split_part(movie_provider_id, '-', 1) = split_part(provider_showing_id, '-', 1)
        AND (provider_showing_id LIKE 'ticketingcine-%' OR first_part_duration_minutes = 0)
        AND (provider_version <> 'VERSION_MUET' OR (provider_showing_id LIKE 'cineoffice-%' AND language = '')))
);
ALTER TABLE movie_slug_aliases ADD CONSTRAINT movie_slug_aliases_cinewest_shape_check CHECK (
    source_provider IS DISTINCT FROM 'cinewest' OR (alias_kind = 'source' AND source_movie_id IS NOT NULL AND slug = 'cinewest-film-' || source_movie_id)
);
ALTER TABLE theater_locations ADD CONSTRAINT theater_locations_cinewest_identity_check CHECK (
    provider <> 'cinewest' OR cinewest_identity_valid('theater', provider_theater_id)
);
