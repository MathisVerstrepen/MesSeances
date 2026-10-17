-- Append Cineode without replacing legacy provider predicates or seeding schedules.
CREATE FUNCTION cineode_slug(value text) RETURNS text
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT CASE value
        WHEN '4640' THEN 'allevard-bel-donne' WHEN '597' THEN 'arletty'
        WHEN '4077' THEN 'benodet-cinemarine' WHEN '593' THEN 'casino'
        WHEN '4626' THEN 'chatillon-chalaronne-etoile' WHEN '592' THEN 'cine-lumieres'
        WHEN '4388' THEN 'cine7' WHEN '4582' THEN 'cinema-casino'
        WHEN '4622' THEN 'doullens-tivoli' WHEN '3524' THEN 'joigny'
        WHEN '981' THEN 'l-arc-en-ciel' WHEN '3955' THEN 'la-grange'
        WHEN '4426' THEN 'le-7eme-art' WHEN '991' THEN 'le-don-camillo'
        WHEN '600' THEN 'le-moderne' WHEN '595' THEN 'le-palace'
        WHEN '636' THEN 'le-paris' WHEN '4420' THEN 'le-raimu'
        WHEN '603' THEN 'le-riounet' WHEN '601' THEN 'le-roc'
        WHEN '594' THEN 'les-clubs' WHEN '3859' THEN 'le-vigan-le-palace'
        WHEN '4444' THEN 'le-zoetrope' WHEN '4624' THEN 'marly-fontenelle'
        WHEN '4638' THEN 'mediatheque-de-nay' WHEN '4277' THEN 'metabief'
        WHEN '4386' THEN 'novelty' WHEN '4384' THEN 'pirou-le-cotentin'
        WHEN '3064' THEN 'poligny' WHEN '4075' THEN 'reville-richelieu'
        WHEN '3074' THEN 'saint-cyr-sur-mer' WHEN '4592' THEN 'saint-marcellin'
        WHEN '4181' THEN 'salle-michel-legrand' WHEN '4281' THEN 'salles-le-7eme-art'
        WHEN '3857' THEN 'sunset' WHEN '3945' THEN 'tour-du-pin-equinoxe'
        WHEN '3104' THEN 'villars-les-dombes' ELSE '' END
$$;

CREATE FUNCTION cineode_identity_valid(kind text, value text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT CASE kind
        WHEN 'theater' THEN cineode_slug(value) <> ''
        WHEN 'movie' THEN length(value) <= 115 AND value ~ '^([1-9][0-9]*|[a-z][a-z0-9]*(-[a-z0-9]+)*)$'
        WHEN 'showing' THEN length(value) <= 112
            AND value ~ '^[1-9][0-9]*-([1-9][0-9]*|[a-z][a-z0-9]*(-[a-z0-9]+)*)-[1-9][0-9]*$'
            AND cineode_slug(split_part(value,'-',1)) <> ''
            AND CASE WHEN substring(value from '-([0-9]+)$') ~ '^[1-9][0-9]{0,18}$'
                THEN substring(value from '-([0-9]+)$')::numeric <= 9223372036854775807 ELSE false END
        ELSE false END
$$;

CREATE FUNCTION cineode_poster_valid(value text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT value = '' OR (length(value) <= 4096
        AND (value ~ '^https://fr\.web\.img[0-9]*\.acsta\.net/(img|medias|pictures)/[A-Za-z0-9_./-]+\.(jpg|jpeg|png|webp)$'
             OR value ~ '^https://static\.cotecine\.fr/tb/Affiches/180x240/[A-Za-z0-9_+-]+\.JPG$')
        AND value !~ '/\.\.?(/|$)' AND substring(value,9) NOT LIKE '%//%')
$$;

DO $$
DECLARE item record; previous_expression text; target_table text;
BEGIN
    FOR item IN SELECT * FROM (VALUES
        ('schedule_snapshot','schedule_snapshot_provider_check','provider = ''cineode'''),
        ('provider_snapshots','provider_snapshots_provider_check','provider = ''cineode'''),
        ('theaters','theaters_provider_check','provider = ''cineode'''),
        ('movies','movies_provider_check','provider = ''cineode'''),
        ('showtimes','showtimes_provider_check','provider = ''cineode'''),
        ('movie_matches','movie_matches_source_provider_check','source_provider = ''cineode'''),
        ('local_movie_groups','local_movie_groups_primary_source_provider_check','primary_source_provider = ''cineode'''),
        ('local_movie_group_members','local_movie_group_members_source_provider_check','source_provider = ''cineode'''),
        ('public_movies','public_movies_identity_anchor_provider_check','identity_anchor_provider = ''cineode'''),
        ('public_movie_sources','public_movie_sources_source_provider_check','source_provider = ''cineode'''),
        ('theater_locations','theater_locations_provider_check','provider = ''cineode'''),
        ('sync_runs','sync_runs_target_check','target = ''cineode'''),
        ('sync_schedules','sync_schedules_target_check','target = ''cineode'''),
        ('theaters','theaters_provider_identity_check','provider = ''cineode'' AND cineode_identity_valid(''theater'',provider_id)'),
        ('theaters','theaters_address_check','provider = ''cineode'''),
        ('theaters','theaters_postal_code_check','provider = ''cineode'''),
        ('showtimes','showtimes_provider_identity_check','provider = ''cineode'' AND cineode_identity_valid(''showing'',provider_showing_id) AND theater_id = ''cineode-'' || split_part(provider_showing_id,''-'',1)'),
        ('movie_slug_aliases','movie_slug_aliases_check','alias_kind = ''source'' AND source_provider = ''cineode'' AND source_movie_id IS NOT NULL'),
        ('sync_runs','sync_runs_trigger_occurrence_check','trigger_source = ''scheduled'' AND target = ''cineode'' AND schedule_id IS NOT NULL AND schedule_id > 0 AND schedule_revision IS NOT NULL AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2'),
        ('screening_history_providers','screening_history_providers_provider_check','provider = ''cineode'''),
        ('theater_images','theater_images_provider_check','provider = ''cineode'''),
        ('cinema_activity_coverage','cinema_activity_coverage_provider_check','provider = ''cineode''')
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
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK ((%s) OR (%I = ''cineode'' AND cineode_identity_valid(''movie'',%I)))',item.table_name,item.constraint_name,previous_expression,item.provider_column,item.id_column);
    END LOOP;
    FOREACH target_table IN ARRAY ARRAY['showtimes','screening_history_showtimes'] LOOP
        SELECT pg_get_expr(conbin,conrelid) INTO STRICT previous_expression FROM pg_constraint WHERE conrelid = target_table::regclass AND conname = 'showtimes_time_check' AND contype = 'c';
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_time_check',target_table);
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT showtimes_time_check CHECK ((provider <> ''cineode'' AND (%s)) OR (provider = ''cineode'' AND end_time = start_time))',target_table,previous_expression);
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT showtimes_cineode_shape_check CHECK (provider <> ''cineode'' OR (
            cineode_identity_valid(''movie'',movie_provider_id)
            AND provider_showing_id = substring(theater_id,9) || ''-'' || movie_provider_id || ''-'' || trunc(extract(epoch from start_time))::text
            AND start_time = date_trunc(''second'',start_time)
            AND service_date = (start_time AT TIME ZONE ''Europe/Paris'')::date
            AND booking_url = ''https://www.cineode.fr/'' || cineode_slug(substring(theater_id,9)) || ''/seances/''
            AND ((language = ''VF'' AND provider_version = ''VF'') OR (language = ''VOSTFR'' AND provider_version = ''VOSTF''))
            AND room = '''' AND first_part_duration_minutes = 0))',target_table);
    END LOOP;
END $$;

ALTER TABLE movie_slug_aliases ADD CONSTRAINT movie_slug_aliases_cineode_shape_check CHECK (
    source_provider IS DISTINCT FROM 'cineode' OR (alias_kind = 'source' AND source_movie_id IS NOT NULL AND slug = 'cineode-film-' || source_movie_id));
ALTER TABLE public_movie_sources ADD CONSTRAINT public_movie_sources_cineode_shape_check CHECK (
    source_provider <> 'cineode' OR (source_slug = 'cineode-film-' || source_movie_id AND cineode_poster_valid(coalesce(poster_url,''))));
ALTER TABLE movies ADD CONSTRAINT movies_cineode_poster_check CHECK (
    provider <> 'cineode' OR cineode_poster_valid(coalesce(poster_url,'')));
ALTER TABLE theater_locations ADD CONSTRAINT theater_locations_cineode_identity_check CHECK (
    provider <> 'cineode' OR cineode_identity_valid('theater',provider_theater_id));
ALTER TABLE theater_images ADD CONSTRAINT theater_images_cineode_identity_check CHECK (
    provider <> 'cineode' OR cineode_identity_valid('theater',provider_theater_id));
