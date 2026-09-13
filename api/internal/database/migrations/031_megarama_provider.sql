-- Extend only the named provider checks. Preserve each existing expression,
-- including legacy provider identities and migration 029 occurrence semantics.
DO $$
DECLARE
    item record;
    previous_expression text;
BEGIN
    FOR item IN SELECT * FROM (VALUES
        ('schedule_snapshot', 'schedule_snapshot_provider_check', 'provider = ''megarama'''),
        ('provider_snapshots', 'provider_snapshots_provider_check', 'provider = ''megarama'''),
        ('theaters', 'theaters_provider_check', 'provider = ''megarama'''),
        ('movies', 'movies_provider_check', 'provider = ''megarama'''),
        ('showtimes', 'showtimes_provider_check', 'provider = ''megarama'''),
        ('movie_matches', 'movie_matches_source_provider_check', 'source_provider = ''megarama'''),
        ('local_movie_groups', 'local_movie_groups_primary_source_provider_check', 'primary_source_provider = ''megarama'''),
        ('local_movie_group_members', 'local_movie_group_members_source_provider_check', 'source_provider = ''megarama'''),
        ('public_movies', 'public_movies_identity_anchor_provider_check', 'identity_anchor_provider = ''megarama'''),
        ('public_movie_sources', 'public_movie_sources_source_provider_check', 'source_provider = ''megarama'''),
        ('theater_locations', 'theater_locations_provider_check', 'provider = ''megarama'''),
        ('sync_runs', 'sync_runs_target_check', 'target = ''megarama'''),
        ('sync_schedules', 'sync_schedules_target_check', 'target = ''megarama'''),
        ('theaters', 'theaters_provider_identity_check', 'provider = ''megarama'' AND provider_id ~ ''^EMS[0-9]{4}$'''),
        ('showtimes', 'showtimes_provider_identity_check', 'provider = ''megarama'' AND provider_showing_id ~ ''^emsx[0-9]{12}$'' AND id = ''megarama-showing-'' || provider_showing_id AND theater_id = ''megarama-EMS'' || substring(provider_showing_id from 5 for 4)'),
        ('showtimes', 'showtimes_time_check', 'provider = ''megarama'' AND end_time = start_time'),
        ('movie_slug_aliases', 'movie_slug_aliases_check', 'alias_kind = ''source'' AND source_provider = ''megarama'' AND source_movie_id IS NOT NULL'),
        ('sync_runs', 'sync_runs_trigger_occurrence_check', 'trigger_source = ''scheduled'' AND target = ''megarama'' AND schedule_id > 0 AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2')
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
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((%s) OR (%I = ''megarama'' AND length(%I) <= 114 AND (%I ~ ''^[A-Z0-9]{5}$'' OR (%I ~ ''^EMS[0-9]{4}-emsx[0-9]{4}HC[0-9]+$'' AND substring(%I from 4 for 4) = substring(%I from 13 for 4)))))',
            item.table_name, item.constraint_name, previous_expression, item.provider_column, item.id_column, item.id_column, item.id_column, item.id_column, item.id_column);
    END LOOP;
END $$;

ALTER TABLE showtimes ADD COLUMN first_part_duration_minutes integer NOT NULL DEFAULT 0
    CHECK (first_part_duration_minutes >= 0 AND (provider = 'megarama' OR first_part_duration_minutes = 0));
