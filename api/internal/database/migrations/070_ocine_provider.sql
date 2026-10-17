-- Append Ociné without changing existing provider predicates or enabling schedules.
CREATE FUNCTION ocine_slug(value text) RETURNS text
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT CASE value WHEN '309' THEN 'maubeuge' WHEN '310' THEN 'st-omer' WHEN '461' THEN 'dunkerque' ELSE '' END
$$;

CREATE FUNCTION ocine_identity_valid(kind text, value text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT CASE kind
        WHEN 'theater' THEN ocine_slug(value) <> ''
        WHEN 'movie' THEN length(value) <= 117 AND value ~ '^([1-9][0-9]*|[a-z][a-z0-9]*(-[a-z0-9]+)*)$'
        WHEN 'showing' THEN length(value) <= 114
            AND value ~ '^(309|310|461)-([1-9][0-9]*|[a-z][a-z0-9]*(-[a-z0-9]+)*)-[1-9][0-9]*-(past|[1-9][0-9]*)$'
            AND CASE WHEN substring(value from '-([0-9]+)-(past|[1-9][0-9]*)$') ~ '^[1-9][0-9]{0,18}$'
                THEN substring(value from '-([0-9]+)-(past|[1-9][0-9]*)$')::numeric <= 9223372036854775807 ELSE false END
        ELSE false END
$$;

CREATE FUNCTION ocine_poster_valid(value text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT value = '' OR (length(value) <= 4096
        AND value ~ '^https://fr\.web\.img[0-9]*\.acsta\.net/(img|medias|pictures)/[A-Za-z0-9_./-]+\.(jpg|jpeg|png|webp)$'
        AND value !~ '/\.\.?(/|$)' AND substring(value,9) NOT LIKE '%//%')
$$;

CREATE FUNCTION ocine_showing_valid(showing text, theater text, movie text, start_at timestamptz, end_at timestamptz, day date, lang text, version text, checkout text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT ocine_identity_valid('showing',showing) AND ocine_identity_valid('theater',theater)
        AND ocine_identity_valid('movie',movie) AND start_at = date_trunc('second',start_at)
        AND day = (start_at AT TIME ZONE 'Europe/Paris')::date
        AND showing = theater || '-' || movie || '-' || trunc(extract(epoch from start_at))::text || '-' || substring(showing from '-(past|[1-9][0-9]*)$')
        AND CASE WHEN right(showing,5) = '-past' THEN
            checkout = '' AND end_at = start_at AND ((lang = '' AND version = '') OR (lang = 'VF' AND version = 'VF') OR (lang = 'VOSTFR' AND version = 'VOSTF'))
        ELSE end_at >= start_at AND length(checkout) <= 4096
            AND checkout ~ ('^https://www\.ocine\.fr/' || ocine_slug(theater) || '/reserver/F[1-9][0-9]*/D' || trunc(extract(epoch from start_at))::text || '/' || CASE WHEN lang = 'VF' AND version = 'VF' THEN 'VF' WHEN lang = 'VOSTFR' AND version = 'VOSTF' THEN 'VO' ELSE 'INVALID' END || '/' || substring(showing from '-([1-9][0-9]*)$') || '/$')
            AND (movie !~ '^[0-9]+$' OR substring(checkout from '/reserver/F([1-9][0-9]*)/') = movie)
        END
$$;

DO $$
DECLARE item record; previous_expression text; target_table text;
BEGIN
    FOR item IN SELECT * FROM (VALUES
        ('schedule_snapshot','schedule_snapshot_provider_check','provider = ''ocine'''),
        ('provider_snapshots','provider_snapshots_provider_check','provider = ''ocine'''),
        ('theaters','theaters_provider_check','provider = ''ocine'''),
        ('movies','movies_provider_check','provider = ''ocine'''),
        ('showtimes','showtimes_provider_check','provider = ''ocine'''),
        ('movie_matches','movie_matches_source_provider_check','source_provider = ''ocine'''),
        ('local_movie_groups','local_movie_groups_primary_source_provider_check','primary_source_provider = ''ocine'''),
        ('local_movie_group_members','local_movie_group_members_source_provider_check','source_provider = ''ocine'''),
        ('public_movies','public_movies_identity_anchor_provider_check','identity_anchor_provider = ''ocine'''),
        ('public_movie_sources','public_movie_sources_source_provider_check','source_provider = ''ocine'''),
        ('theater_locations','theater_locations_provider_check','provider = ''ocine'''),
        ('sync_runs','sync_runs_target_check','target = ''ocine'''),
        ('sync_schedules','sync_schedules_target_check','target = ''ocine'''),
        ('theaters','theaters_provider_identity_check','provider = ''ocine'' AND ocine_identity_valid(''theater'',provider_id)'),
        ('theaters','theaters_address_check','provider = ''ocine'''),
        ('theaters','theaters_postal_code_check','provider = ''ocine'''),
        ('showtimes','showtimes_provider_identity_check','provider = ''ocine'' AND ocine_identity_valid(''showing'',provider_showing_id) AND theater_id = ''ocine-'' || split_part(provider_showing_id,''-'',1)'),
        ('showtimes','showtimes_language_check','provider = ''ocine'' AND language = '''' AND provider_version = '''' AND booking_url = '''' AND right(provider_showing_id,5) = ''-past'''),
        ('showtimes','showtimes_provider_version_check','provider = ''ocine'' AND language = '''' AND provider_version = '''' AND booking_url = '''' AND right(provider_showing_id,5) = ''-past'''),
        ('showtimes','showtimes_booking_url_check','provider = ''ocine'' AND booking_url = '''' AND right(provider_showing_id,5) = ''-past'''),
        ('movie_slug_aliases','movie_slug_aliases_check','alias_kind = ''source'' AND source_provider = ''ocine'' AND source_movie_id IS NOT NULL'),
        ('sync_runs','sync_runs_trigger_occurrence_check','trigger_source = ''scheduled'' AND target = ''ocine'' AND schedule_id IS NOT NULL AND schedule_id > 0 AND schedule_revision IS NOT NULL AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2'),
        ('screening_history_providers','screening_history_providers_provider_check','provider = ''ocine'''),
        ('theater_images','theater_images_provider_check','provider = ''ocine'''),
        ('cinema_activity_coverage','cinema_activity_coverage_provider_check','provider = ''ocine''')
    ) AS checks(table_name,constraint_name,addition)
    LOOP
        FOR target_table IN SELECT item.table_name UNION ALL SELECT 'screening_history_' || item.table_name WHERE item.table_name IN ('theaters','showtimes') LOOP
            SELECT pg_get_expr(conbin,conrelid) INTO STRICT previous_expression FROM pg_constraint WHERE conrelid = target_table::regclass AND conname = item.constraint_name AND contype = 'c';
            EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I',target_table,item.constraint_name);
            EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((%s) OR (%s))',target_table,item.constraint_name,previous_expression,item.addition);
        END LOOP;
    END LOOP;
    FOR item IN SELECT * FROM (VALUES
        ('movies','movies_provider_identity_check','provider','provider_id'),
        ('movie_matches','movie_matches_source_movie_id_check','source_provider','source_movie_id'),
        ('local_movie_groups','local_movie_groups_check','primary_source_provider','primary_source_movie_id'),
        ('local_movie_group_members','local_movie_group_members_check','source_provider','source_movie_id'),
        ('public_movies','public_movies_check1','identity_anchor_provider','identity_anchor_source_movie_id'),
        ('public_movie_sources','public_movie_sources_check','source_provider','source_movie_id'),
        ('movie_slug_aliases','movie_slug_aliases_check1','source_provider','source_movie_id')
    ) AS checks(table_name,constraint_name,provider_column,id_column)
    LOOP
        SELECT pg_get_expr(conbin,conrelid) INTO STRICT previous_expression FROM pg_constraint WHERE conrelid = item.table_name::regclass AND conname = item.constraint_name AND contype = 'c';
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I',item.table_name,item.constraint_name);
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((%s) OR (%I = ''ocine'' AND ocine_identity_valid(''movie'',%I)))',item.table_name,item.constraint_name,previous_expression,item.provider_column,item.id_column);
    END LOOP;
    FOREACH target_table IN ARRAY ARRAY['showtimes','screening_history_showtimes'] LOOP
        SELECT pg_get_expr(conbin,conrelid) INTO STRICT previous_expression FROM pg_constraint WHERE conrelid = target_table::regclass AND conname = 'showtimes_time_check' AND contype = 'c';
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_time_check',target_table);
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT showtimes_time_check CHECK ((provider <> ''ocine'' AND (%s)) OR (provider = ''ocine'' AND end_time >= start_time))',target_table,previous_expression);
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT showtimes_ocine_shape_check CHECK (provider <> ''ocine'' OR (first_part_duration_minutes = 0 AND ocine_showing_valid(provider_showing_id,substring(theater_id,7),movie_provider_id,start_time,end_time,service_date,language,provider_version,booking_url)))',target_table);
    END LOOP;
END $$;

ALTER TABLE movie_slug_aliases ADD CONSTRAINT movie_slug_aliases_ocine_shape_check CHECK (
    source_provider IS DISTINCT FROM 'ocine' OR (alias_kind = 'source' AND source_movie_id IS NOT NULL AND slug = 'ocine-film-' || source_movie_id));
ALTER TABLE public_movie_sources ADD CONSTRAINT public_movie_sources_ocine_shape_check CHECK (
    source_provider <> 'ocine' OR (source_slug = 'ocine-film-' || source_movie_id AND ocine_poster_valid(coalesce(poster_url,''))));
ALTER TABLE movies ADD CONSTRAINT movies_ocine_poster_check CHECK (
    provider <> 'ocine' OR ocine_poster_valid(coalesce(poster_url,'')));
ALTER TABLE theater_locations ADD CONSTRAINT theater_locations_ocine_identity_check CHECK (
    provider <> 'ocine' OR ocine_identity_valid('theater',provider_theater_id));
ALTER TABLE theater_images ADD CONSTRAINT theater_images_ocine_identity_check CHECK (
    provider <> 'ocine' OR ocine_identity_valid('theater',provider_theater_id));
