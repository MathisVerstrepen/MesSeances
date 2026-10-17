-- Extend provider checks without rewriting prior predicates, data or schedules.
CREATE FUNCTION majestic_identity_valid(kind text, value text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT CASE kind
        WHEN 'theater' THEN value IN ('EMS0700','EMS0654','EMS1333','EMS1212','EMS1299','EMS1211','EMS0957')
        WHEN 'movie' THEN value ~ '^[A-Z0-9]{5}$' OR (
            length(value) <= 114 AND value ~ '^EMS[0-9]{4}-emsx[0-9]{4}HC[0-9]+$'
            AND substring(value,4,4) = substring(value,13,4)
            AND substring(value,1,7) IN ('EMS0700','EMS0654','EMS1333','EMS1212','EMS1299','EMS1211','EMS0957'))
        WHEN 'showing' THEN value ~ '^emsx[0-9]{12}$'
            AND 'EMS' || substring(value,5,4) IN ('EMS0700','EMS0654','EMS1333','EMS1212','EMS1299','EMS1211','EMS0957')
        ELSE false END
$$;

CREATE FUNCTION majestic_website(theater text) RETURNS text
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT CASE theater
        WHEN 'EMS0700' THEN 'https://majestic-compiegne.fr/'
        WHEN 'EMS0654' THEN 'https://majestic-douai.com/'
        WHEN 'EMS1333' THEN 'https://dole.majestic-cinemas.com/'
        WHEN 'EMS1212' THEN 'https://vesoul.majestic-cinemas.com/'
        WHEN 'EMS1299' THEN 'https://cinelaon.fr/'
        WHEN 'EMS1211' THEN 'https://remiremont.majestic-cinemas.com/'
        WHEN 'EMS0957' THEN 'https://cinevendome.fr/' ELSE '' END
$$;

CREATE FUNCTION majestic_booking_valid(value text, theater text, showing text, movie text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT length(value) <= 4096 AND majestic_identity_valid('theater',theater)
        AND majestic_identity_valid('showing',showing) AND substring(showing,5,4) = substring(theater,4,4)
        AND majestic_identity_valid('movie',movie)
        AND (movie ~ '^[A-Z0-9]{5}$' OR substring(movie,1,7) = theater)
        AND (value = majestic_website(theater) OR value IN (
            CASE WHEN movie ~ '^[A-Z0-9]{5}$' THEN 'https://www.ticketingcine.com/' || theater || '/FR' || movie
                ELSE 'https://www.ticketingcine.com?' || theater END || '#showsession?id=' || showing,
            CASE WHEN movie ~ '^[A-Z0-9]{5}$' THEN 'https://www.ticketingcine.com/' || theater || '/FR' || movie
                ELSE 'https://www.ticketingcine.com?' || theater END || '#showsession?id=' || showing || '&ps=ticketingcine'))
$$;

CREATE FUNCTION majestic_poster_valid(value text, movie text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT value = '' OR (length(value) <= 4096 AND majestic_identity_valid('movie',movie) AND
        CASE WHEN movie ~ '^[A-Z0-9]{5}$' THEN value ~ ('^https://images\.monnaie-services\.com/movie_poster/(120|600)/FR' || movie || '/[A-Z0-9]{8}\.webp$')
        ELSE value ~ ('^https://images\.monnaie-services\.com/ems_spectacle/120/' || substring(movie,4,4) || '/' || substring(movie,17) || '\.jpg(\?ts=[0-9]+)?$') END)
$$;

DO $$
DECLARE item record; previous_expression text; target_table text;
BEGIN
    FOR item IN SELECT * FROM (VALUES
        ('schedule_snapshot','schedule_snapshot_provider_check','provider = ''majestic'''),
        ('provider_snapshots','provider_snapshots_provider_check','provider = ''majestic'''),
        ('theaters','theaters_provider_check','provider = ''majestic'''),
        ('movies','movies_provider_check','provider = ''majestic'''),
        ('showtimes','showtimes_provider_check','provider = ''majestic'''),
        ('movie_matches','movie_matches_source_provider_check','source_provider = ''majestic'''),
        ('local_movie_groups','local_movie_groups_primary_source_provider_check','primary_source_provider = ''majestic'''),
        ('local_movie_group_members','local_movie_group_members_source_provider_check','source_provider = ''majestic'''),
        ('public_movies','public_movies_identity_anchor_provider_check','identity_anchor_provider = ''majestic'''),
        ('public_movie_sources','public_movie_sources_source_provider_check','source_provider = ''majestic'''),
        ('theater_locations','theater_locations_provider_check','provider = ''majestic'''),
        ('sync_runs','sync_runs_target_check','target = ''majestic'''),
        ('sync_schedules','sync_schedules_target_check','target = ''majestic'''),
        ('theaters','theaters_provider_identity_check','provider = ''majestic'' AND majestic_identity_valid(''theater'',provider_id)'),
        ('showtimes','showtimes_provider_identity_check','provider = ''majestic'' AND majestic_identity_valid(''showing'',provider_showing_id) AND theater_id = ''majestic-EMS'' || substring(provider_showing_id,5,4)'),
        ('showtimes','showtimes_check','provider = ''majestic'' AND first_part_duration_minutes >= 0'),
        ('movie_slug_aliases','movie_slug_aliases_check','alias_kind = ''source'' AND source_provider = ''majestic'' AND source_movie_id IS NOT NULL'),
        ('sync_runs','sync_runs_trigger_occurrence_check','trigger_source = ''scheduled'' AND target = ''majestic'' AND schedule_id IS NOT NULL AND schedule_id > 0 AND schedule_revision IS NOT NULL AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2'),
        ('screening_history_providers','screening_history_providers_provider_check','provider = ''majestic'''),
        ('theater_images','theater_images_provider_check','provider = ''majestic'''),
        ('cinema_activity_coverage','cinema_activity_coverage_provider_check','provider = ''majestic''')
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
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((%s) OR (%I = ''majestic'' AND majestic_identity_valid(''movie'',%I)))',item.table_name,item.constraint_name,previous_expression,item.provider_column,item.id_column);
    END LOOP;
    FOREACH target_table IN ARRAY ARRAY['showtimes','screening_history_showtimes'] LOOP
        SELECT pg_get_expr(conbin,conrelid) INTO STRICT previous_expression FROM pg_constraint WHERE conrelid = target_table::regclass AND conname = 'showtimes_time_check' AND contype = 'c';
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_time_check',target_table);
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT showtimes_time_check CHECK ((provider <> ''majestic'' AND (%s)) OR (provider = ''majestic'' AND end_time >= start_time))',target_table,previous_expression);
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT showtimes_majestic_shape_check CHECK (provider <> ''majestic'' OR (
            majestic_booking_valid(booking_url,substring(theater_id,10),provider_showing_id,movie_provider_id)
            AND btrim(room) <> '''' AND first_part_duration_minutes >= 0))',target_table);
    END LOOP;
END $$;

ALTER TABLE movie_slug_aliases ADD CONSTRAINT movie_slug_aliases_majestic_shape_check CHECK (
    source_provider IS DISTINCT FROM 'majestic' OR (alias_kind = 'source' AND source_movie_id IS NOT NULL AND slug = 'majestic-film-' || source_movie_id));
ALTER TABLE public_movie_sources ADD CONSTRAINT public_movie_sources_majestic_shape_check CHECK (
    source_provider <> 'majestic' OR (source_slug = 'majestic-film-' || source_movie_id AND majestic_poster_valid(coalesce(poster_url,''),source_movie_id)));
ALTER TABLE movies ADD CONSTRAINT movies_majestic_poster_check CHECK (
    provider <> 'majestic' OR majestic_poster_valid(coalesce(poster_url,''),provider_id));
ALTER TABLE theater_locations ADD CONSTRAINT theater_locations_majestic_identity_check CHECK (
    provider <> 'majestic' OR majestic_identity_valid('theater',provider_theater_id));
ALTER TABLE theater_images ADD CONSTRAINT theater_images_majestic_identity_check CHECK (
    provider <> 'majestic' OR majestic_identity_valid('theater',provider_theater_id));
