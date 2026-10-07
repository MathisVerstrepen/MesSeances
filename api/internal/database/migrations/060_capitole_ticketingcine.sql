-- Re-key the cinema, not its historical films, sessions or immutable receipts.
ALTER TABLE showtimes DROP CONSTRAINT showtimes_cinewest_shape_check;
ALTER TABLE screening_history_showtimes DROP CONSTRAINT showtimes_cinewest_shape_check;

CREATE OR REPLACE FUNCTION cinewest_identity_valid(kind text, value text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT CASE kind
        WHEN 'theater' THEN value IN (
            'cineoffice-cognaclegalaxy', 'cineoffice-neverscinemazarin', 'cineoffice-mouanssartouxlastrada',
            'cineoffice-vitreaurore', 'cineoffice-mouginslesbalcons', 'cineoffice-royanlelido',
            'cineoffice-ploermelcinelac', 'cineoffice-saintesatlanticcine', 'cineoffice-aurillaclecristal',
            'ticketingcine-EMS1185', 'ticketingcine-EMS1317', 'ticketingcine-EMS0042', 'ticketingcine-EMS1378')
        WHEN 'movie' THEN octet_length(value) <= 114 AND (
            value ~ '^(cineoffice-[1-9][0-9]*|ticketingcine-[A-Z0-9]{5}|webediamovies-[1-9][0-9]*)$'
            OR (value ~ '^ticketingcine-EMS[0-9]{4}-emsx[0-9]{4}HC[0-9]+$'
                AND split_part(value, '-', 2) IN ('EMS1185', 'EMS1317', 'EMS0042', 'EMS1378')
                AND substring(split_part(value, '-', 2) FROM 4) = substring(split_part(value, '-', 3) FROM 5 FOR 4)))
        WHEN 'showing' THEN value ~ '^(cineoffice|ticketingcine|webediamovies)-[a-f0-9]{64}$'
        ELSE false END
$$;

DO $$
DECLARE
    item record;
    old_id constant text := 'cinewest-webediamovies-W8400';
    new_id constant text := 'cinewest-ticketingcine-EMS1378';
    changed boolean;
    old_present boolean;
    new_present boolean;
    source_present boolean := false;
    destination_present boolean := false;
    cinema_constraints text;
BEGIN
    -- Any independently populated destination namespace is an operator conflict.
    -- Account arrays alone may coalesce membership; relational rows never merge.
    FOR item IN SELECT * FROM (VALUES
        ('theaters', 'id'), ('theater_dates', 'theater_id'), ('theater_passes', 'theater_id'),
        ('showtimes', 'theater_id'), ('screening_history_theaters', 'id'),
        ('screening_history_showtimes', 'theater_id'), ('cinema_activity_state', 'theater_id'),
        ('cinema_activity_coverage', 'theater_id'), ('cinema_activity_episodes', 'theater_id'),
        ('cinema_activity_episode_sources', 'theater_id')
    ) AS keys(table_name, column_name)
    LOOP
        EXECUTE format('SELECT EXISTS (SELECT 1 FROM %I WHERE %I = $1), EXISTS (SELECT 1 FROM %I WHERE %I = $2)',
            item.table_name, item.column_name, item.table_name, item.column_name)
            INTO old_present, new_present USING old_id, new_id;
        source_present := source_present OR old_present;
        destination_present := destination_present OR new_present;
    END LOOP;
    FOR item IN SELECT unnest(ARRAY['theater_locations', 'theater_images']) AS table_name
    LOOP
        EXECUTE format('SELECT EXISTS (SELECT 1 FROM %I WHERE provider = ''cinewest'' AND provider_theater_id = ''webediamovies-W8400''), EXISTS (SELECT 1 FROM %I WHERE provider = ''cinewest'' AND provider_theater_id = ''ticketingcine-EMS1378'')', item.table_name, item.table_name)
            INTO old_present, new_present;
        source_present := source_present OR old_present;
        destination_present := destination_present OR new_present;
    END LOOP;
    IF source_present AND destination_present THEN RAISE EXCEPTION 'Capitole destination conflict'; END IF;

    SELECT EXISTS (SELECT 1 FROM theaters WHERE id = old_id)
        OR EXISTS (SELECT 1 FROM screening_history_theaters WHERE id = old_id)
        OR EXISTS (SELECT 1 FROM theater_locations WHERE provider = 'cinewest' AND provider_theater_id = 'webediamovies-W8400')
        OR EXISTS (SELECT 1 FROM theater_images WHERE provider = 'cinewest' AND provider_theater_id = 'webediamovies-W8400')
        INTO changed;

    -- Resolve actual FK names in this schema, and only defer cinema-key edges.
    FOR item IN SELECT c.conrelid::regclass AS table_name, c.conname, n.nspname
        FROM pg_constraint c JOIN pg_namespace n ON n.oid = c.connamespace
        WHERE c.contype = 'f'
          AND c.conrelid = ANY (ARRAY['theater_dates'::regclass, 'theater_passes'::regclass, 'showtimes'::regclass,
              'screening_history_showtimes'::regclass, 'cinema_activity_state'::regclass,
              'cinema_activity_coverage'::regclass, 'cinema_activity_episodes'::regclass,
              'cinema_activity_episode_sources'::regclass])
          AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.conrelid AND a.attnum = ANY(c.conkey) AND a.attname = 'theater_id')
    LOOP
        EXECUTE format('ALTER TABLE %s ALTER CONSTRAINT %I DEFERRABLE INITIALLY DEFERRED', item.table_name, item.conname);
        cinema_constraints := concat_ws(', ', cinema_constraints, format('%I.%I', item.nspname, item.conname));
    END LOOP;

    UPDATE theaters SET id = new_id, slug = new_id, provider_id = 'ticketingcine-EMS1378' WHERE id = old_id;
    UPDATE screening_history_theaters SET id = new_id, slug = new_id, provider_id = 'ticketingcine-EMS1378' WHERE id = old_id;
    FOR item IN SELECT unnest(ARRAY['theater_dates', 'theater_passes', 'showtimes', 'screening_history_showtimes',
        'cinema_activity_state', 'cinema_activity_coverage', 'cinema_activity_episodes', 'cinema_activity_episode_sources']) AS table_name
    LOOP
        EXECUTE format('UPDATE %I SET theater_id = $1 WHERE theater_id = $2', item.table_name) USING new_id, old_id;
    END LOOP;
    UPDATE theater_locations SET provider_theater_id = 'ticketingcine-EMS1378'
        WHERE provider = 'cinewest' AND provider_theater_id = 'webediamovies-W8400';
    UPDATE theater_images SET provider_theater_id = 'ticketingcine-EMS1378', image_revision = image_revision + 1
        WHERE provider = 'cinewest' AND provider_theater_id = 'webediamovies-W8400';
    -- Existing upper-bound CHECKs make an exhausted affected revision fail atomically.
    FOR item IN SELECT unnest(ARRAY['account_theater_preferences', 'account_theater_follows']) AS table_name
    LOOP
        EXECUTE format('UPDATE %I SET revision = revision + 1, theater_ids = ARRAY(SELECT DISTINCT CASE WHEN id = $1 THEN $2 ELSE id END COLLATE "C" FROM unnest(theater_ids) AS ids(id) ORDER BY 1) WHERE $1 = ANY(theater_ids)', item.table_name)
            USING old_id, new_id;
    END LOOP;
    IF changed THEN
        UPDATE theater_location_state SET version = version + 1 WHERE singleton;
        IF NOT FOUND THEN RAISE EXCEPTION 'missing theater location revision'; END IF;
    END IF;

    -- Flush every scoped edge before any ALTER. Intermediate parent/child
    -- tables also carry queued events belonging to other cinema constraints.
    EXECUTE 'SET CONSTRAINTS ' || cinema_constraints || ' IMMEDIATE';
    FOR item IN SELECT c.conrelid::regclass AS table_name, c.conname, n.nspname
        FROM pg_constraint c JOIN pg_namespace n ON n.oid = c.connamespace
        WHERE c.contype = 'f'
          AND c.conrelid = ANY (ARRAY['theater_dates'::regclass, 'theater_passes'::regclass, 'showtimes'::regclass,
              'screening_history_showtimes'::regclass, 'cinema_activity_state'::regclass,
              'cinema_activity_coverage'::regclass, 'cinema_activity_episodes'::regclass,
              'cinema_activity_episode_sources'::regclass])
          AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.conrelid AND a.attnum = ANY(c.conkey) AND a.attname = 'theater_id')
    LOOP
        EXECUTE format('ALTER TABLE %s ALTER CONSTRAINT %I NOT DEFERRABLE INITIALLY IMMEDIATE', item.table_name, item.conname);
    END LOOP;

    -- Replacing an immutable helper does not revalidate existing CHECKs.
    -- Recreate every scoped helper-dependent check with its exact expression.
    FOR item IN SELECT c.conrelid::regclass AS table_name, c.conname, pg_get_constraintdef(c.oid) AS definition
        FROM pg_constraint c
        WHERE c.contype = 'c'
          AND c.connamespace = (SELECT relnamespace FROM pg_class WHERE oid = 'theaters'::regclass)
          AND pg_get_expr(c.conbin, c.conrelid) LIKE '%cinewest_identity_valid%'
    LOOP
        EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I', item.table_name, item.conname);
        EXECUTE format('ALTER TABLE %s ADD CONSTRAINT %I %s', item.table_name, item.conname, item.definition);
        EXECUTE format('ALTER TABLE %s VALIDATE CONSTRAINT %I', item.table_name, item.conname);
    END LOOP;
END $$;

DO $$
DECLARE item regclass;
BEGIN
    FOREACH item IN ARRAY ARRAY['showtimes'::regclass, 'screening_history_showtimes'::regclass]
    LOOP
        EXECUTE format('ALTER TABLE %s ADD CONSTRAINT showtimes_cinewest_shape_check CHECK (
            provider <> ''cinewest'' OR (
                btrim(room) <> ''''
                AND (split_part(theater_id, ''-'', 2) = split_part(provider_showing_id, ''-'', 1)
                    OR (theater_id = ''cinewest-ticketingcine-EMS1378'' AND provider_showing_id LIKE ''webediamovies-%%''))
                AND split_part(movie_provider_id, ''-'', 1) = split_part(provider_showing_id, ''-'', 1)
                AND (provider_showing_id LIKE ''ticketingcine-%%'' OR first_part_duration_minutes = 0)
                AND (provider_version <> ''VERSION_MUET'' OR (provider_showing_id LIKE ''cineoffice-%%'' AND language = ''''))))', item);
    END LOOP;
END $$;
