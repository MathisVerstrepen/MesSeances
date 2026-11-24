-- Provider registry: cinema providers become rows of `providers` instead of
-- string allowlists repeated in CHECK constraints, and provider-specific
-- identity, booking, poster and shape rules move to the application
-- (schedule.ValidateDataset is the single source of truth).
--
-- Every replacement constraint below is implied by the constraint it
-- replaces: it is the old predicate with provider-specific terms replaced by
-- TRUE (or by the shared identity character set, which every provider format
-- already satisfies). Rows accepted before this migration therefore remain
-- valid, and validation of the new constraints cannot fail on existing data.
-- Adding a provider later only requires inserting a `providers` row.

CREATE TABLE providers (
    key text PRIMARY KEY CHECK (key ~ '^[a-z][a-z0-9]{1,31}$')
);

INSERT INTO providers (key) VALUES
    ('ugc'), ('kinepolis'), ('pathe'), ('cgr'), ('megarama'), ('cineville'), ('mk2'), ('cinewest'),
    ('grandecran'), ('noecinemas'), ('veo'), ('majestic'), ('cineode'), ('cinemovida'), ('ocine');

-- 1. Drop provider-specific CHECK constraints on live schedule tables and
--    their screening history copies (same constraint names).
DO $$
DECLARE
    item record;
BEGIN
    FOR item IN SELECT * FROM (VALUES
        ('theaters', 'theaters_provider_check'),
        ('theaters', 'theaters_provider_identity_check'),
        ('theaters', 'theaters_address_check'),
        ('theaters', 'theaters_postal_code_check'),
        ('screening_history_theaters', 'theaters_provider_check'),
        ('screening_history_theaters', 'theaters_provider_identity_check'),
        ('screening_history_theaters', 'theaters_address_check'),
        ('screening_history_theaters', 'theaters_postal_code_check'),
        ('movies', 'movies_provider_check'),
        ('movies', 'movies_provider_identity_check'),
        ('movies', 'movies_cineode_poster_check'),
        ('movies', 'movies_majestic_poster_check'),
        ('movies', 'movies_ocine_poster_check'),
        ('movies', 'movies_source_page_url_check'),
        ('provider_snapshots', 'provider_snapshots_provider_check'),
        ('screening_history_providers', 'screening_history_providers_provider_check'),
        ('schedule_snapshot', 'schedule_snapshot_provider_check'),
        ('movie_matches', 'movie_matches_source_provider_check'),
        ('movie_matches', 'movie_matches_source_movie_id_check'),
        ('local_movie_groups', 'local_movie_groups_primary_source_provider_check'),
        ('local_movie_groups', 'local_movie_groups_check'),
        ('local_movie_group_members', 'local_movie_group_members_source_provider_check'),
        ('local_movie_group_members', 'local_movie_group_members_check'),
        ('public_movies', 'public_movies_identity_anchor_provider_check'),
        ('public_movies', 'public_movies_check1'),
        ('public_movie_sources', 'public_movie_sources_source_provider_check'),
        ('public_movie_sources', 'public_movie_sources_check'),
        ('public_movie_sources', 'public_movie_sources_cinemovida_slug_check'),
        ('public_movie_sources', 'public_movie_sources_cineode_shape_check'),
        ('public_movie_sources', 'public_movie_sources_grandecran_slug_check'),
        ('public_movie_sources', 'public_movie_sources_majestic_shape_check'),
        ('public_movie_sources', 'public_movie_sources_noecinemas_slug_check'),
        ('public_movie_sources', 'public_movie_sources_ocine_shape_check'),
        ('public_movie_sources', 'public_movie_sources_veo_slug_check'),
        ('movie_slug_aliases', 'movie_slug_aliases_check'),
        ('movie_slug_aliases', 'movie_slug_aliases_check1'),
        ('movie_slug_aliases', 'movie_slug_aliases_cinemovida_shape_check'),
        ('movie_slug_aliases', 'movie_slug_aliases_cineode_shape_check'),
        ('movie_slug_aliases', 'movie_slug_aliases_cineville_shape_check'),
        ('movie_slug_aliases', 'movie_slug_aliases_cinewest_shape_check'),
        ('movie_slug_aliases', 'movie_slug_aliases_grandecran_shape_check'),
        ('movie_slug_aliases', 'movie_slug_aliases_majestic_shape_check'),
        ('movie_slug_aliases', 'movie_slug_aliases_mk2_shape_check'),
        ('movie_slug_aliases', 'movie_slug_aliases_noecinemas_shape_check'),
        ('movie_slug_aliases', 'movie_slug_aliases_ocine_shape_check'),
        ('movie_slug_aliases', 'movie_slug_aliases_veo_shape_check'),
        ('sync_runs', 'sync_runs_target_check'),
        ('sync_runs', 'sync_runs_trigger_occurrence_check'),
        ('sync_schedules', 'sync_schedules_target_check'),
        ('theater_locations', 'theater_locations_provider_check'),
        ('theater_locations', 'theater_locations_cinemovida_identity_check'),
        ('theater_locations', 'theater_locations_cineode_identity_check'),
        ('theater_locations', 'theater_locations_cineville_identity_check'),
        ('theater_locations', 'theater_locations_cinewest_identity_check'),
        ('theater_locations', 'theater_locations_grandecran_identity_check'),
        ('theater_locations', 'theater_locations_majestic_identity_check'),
        ('theater_locations', 'theater_locations_mk2_identity_check'),
        ('theater_locations', 'theater_locations_noecinemas_identity_check'),
        ('theater_locations', 'theater_locations_ocine_identity_check'),
        ('theater_locations', 'theater_locations_veo_identity_check'),
        ('theater_images', 'theater_images_provider_check'),
        ('theater_images', 'theater_images_cinemovida_identity_check'),
        ('theater_images', 'theater_images_cineode_identity_check'),
        ('theater_images', 'theater_images_majestic_identity_check'),
        ('theater_images', 'theater_images_ocine_identity_check'),
        ('theater_images', 'theater_images_veo_identity_check'),
        ('cinema_activity_coverage', 'cinema_activity_coverage_provider_check'),
        ('cinema_activity_coverage', 'cinema_activity_coverage_check')
    ) AS drops(table_name, constraint_name)
    LOOP
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I', item.table_name, item.constraint_name);
    END LOOP;

    FOR item IN SELECT unnest(ARRAY['showtimes', 'screening_history_showtimes']) AS table_name LOOP
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_provider_check', item.table_name);
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_provider_identity_check', item.table_name);
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_booking_url_check', item.table_name);
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_check', item.table_name);
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_language_check', item.table_name);
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_provider_version_check', item.table_name);
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_time_check', item.table_name);
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_cinemovida_shape_check', item.table_name);
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_cineode_shape_check', item.table_name);
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_cinewest_shape_check', item.table_name);
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_grandecran_shape_check', item.table_name);
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_majestic_shape_check', item.table_name);
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_mk2_shape_check', item.table_name);
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_noecinemas_shape_check', item.table_name);
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_ocine_shape_check', item.table_name);
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT showtimes_veo_shape_check', item.table_name);

        -- Generic replacements shared by live showtimes and history.
        EXECUTE format($sql$ALTER TABLE %I ADD CONSTRAINT showtimes_provider_showing_id_charset_check
            CHECK (provider_showing_id ~ '^[A-Za-z0-9_-]+$')$sql$, item.table_name);
        EXECUTE format($sql$ALTER TABLE %I ADD CONSTRAINT showtimes_booking_url_check
            CHECK (btrim(booking_url) <> '' OR booking_url = '')$sql$, item.table_name);
        EXECUTE format($sql$ALTER TABLE %I ADD CONSTRAINT showtimes_first_part_duration_check
            CHECK (first_part_duration_minutes >= 0)$sql$, item.table_name);
        EXECUTE format($sql$ALTER TABLE %I ADD CONSTRAINT showtimes_language_check
            CHECK (language = '' OR (language <> 'ALL' AND language ~ '^[A-Z][A-Z0-9_]{0,15}$'))$sql$, item.table_name);
        EXECUTE format($sql$ALTER TABLE %I ADD CONSTRAINT showtimes_provider_version_check
            CHECK (btrim(provider_version) <> '' OR (provider_version = '' AND language = ''))$sql$, item.table_name);
        EXECUTE format($sql$ALTER TABLE %I ADD CONSTRAINT showtimes_time_check
            CHECK (end_time >= start_time)$sql$, item.table_name);
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I FOREIGN KEY (provider) REFERENCES providers (key)',
            item.table_name, item.table_name || '_provider_fkey');
    END LOOP;

    FOR item IN SELECT unnest(ARRAY['theaters', 'screening_history_theaters']) AS table_name LOOP
        EXECUTE format($sql$ALTER TABLE %I ADD CONSTRAINT theaters_provider_id_charset_check
            CHECK (provider_id ~ '^[A-Za-z0-9_-]+$')$sql$, item.table_name);
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I FOREIGN KEY (provider) REFERENCES providers (key)',
            item.table_name, item.table_name || '_provider_fkey');
    END LOOP;
END $$;

-- 2. Provider foreign keys replace the remaining allowlists.
ALTER TABLE movies ADD CONSTRAINT movies_provider_fkey FOREIGN KEY (provider) REFERENCES providers (key);
ALTER TABLE provider_snapshots ADD CONSTRAINT provider_snapshots_provider_fkey FOREIGN KEY (provider) REFERENCES providers (key);
ALTER TABLE screening_history_providers ADD CONSTRAINT screening_history_providers_provider_fkey FOREIGN KEY (provider) REFERENCES providers (key);
ALTER TABLE movie_matches ADD CONSTRAINT movie_matches_source_provider_fkey FOREIGN KEY (source_provider) REFERENCES providers (key);
ALTER TABLE local_movie_groups ADD CONSTRAINT local_movie_groups_primary_source_provider_fkey FOREIGN KEY (primary_source_provider) REFERENCES providers (key);
ALTER TABLE local_movie_group_members ADD CONSTRAINT local_movie_group_members_source_provider_fkey FOREIGN KEY (source_provider) REFERENCES providers (key);
ALTER TABLE public_movies ADD CONSTRAINT public_movies_identity_anchor_provider_fkey FOREIGN KEY (identity_anchor_provider) REFERENCES providers (key);
ALTER TABLE public_movie_sources ADD CONSTRAINT public_movie_sources_source_provider_fkey FOREIGN KEY (source_provider) REFERENCES providers (key);
ALTER TABLE movie_slug_aliases ADD CONSTRAINT movie_slug_aliases_source_provider_fkey FOREIGN KEY (source_provider) REFERENCES providers (key);
ALTER TABLE theater_locations ADD CONSTRAINT theater_locations_provider_fkey FOREIGN KEY (provider) REFERENCES providers (key);
ALTER TABLE theater_images ADD CONSTRAINT theater_images_provider_fkey FOREIGN KEY (provider) REFERENCES providers (key);
ALTER TABLE cinema_activity_coverage ADD CONSTRAINT cinema_activity_coverage_provider_fkey FOREIGN KEY (provider) REFERENCES providers (key);

-- 3. Generic replacements for the remaining dropped checks.
ALTER TABLE movies ADD CONSTRAINT movies_provider_id_charset_check
    CHECK (provider_id ~ '^[A-Za-z0-9_-]+$');
ALTER TABLE movies ADD CONSTRAINT movies_source_page_url_check
    CHECK (source_page_url IS NULL OR (source_page_url ~ '^https://[^/?#@[:space:]]+/[^?#[:space:]]+(#[^?#[:space:]]+)?$' AND source_page_url !~ '[%\\]'));
ALTER TABLE schedule_snapshot ADD CONSTRAINT schedule_snapshot_provider_check
    CHECK (provider ~ '^[a-z][a-z0-9]{1,31}$');
ALTER TABLE movie_matches ADD CONSTRAINT movie_matches_source_movie_id_check
    CHECK (source_movie_id ~ '^[A-Za-z0-9_-]+$');
ALTER TABLE local_movie_groups ADD CONSTRAINT local_movie_groups_primary_source_movie_id_check
    CHECK (primary_source_movie_id ~ '^[A-Za-z0-9_-]+$');
ALTER TABLE local_movie_group_members ADD CONSTRAINT local_movie_group_members_source_movie_id_check
    CHECK (source_movie_id ~ '^[A-Za-z0-9_-]+$');
ALTER TABLE public_movies ADD CONSTRAINT public_movies_identity_anchor_source_movie_id_check
    CHECK (identity_anchor_provider IS NULL OR identity_anchor_source_movie_id ~ '^[A-Za-z0-9_-]+$');
ALTER TABLE public_movie_sources ADD CONSTRAINT public_movie_sources_source_movie_id_check
    CHECK (source_movie_id ~ '^[A-Za-z0-9_-]+$');
ALTER TABLE movie_slug_aliases ADD CONSTRAINT movie_slug_aliases_check
    CHECK ((alias_kind = 'source' AND source_movie_id IS NOT NULL)
        OR (alias_kind <> 'source' AND source_provider IS NULL AND source_movie_id IS NULL));
ALTER TABLE movie_slug_aliases ADD CONSTRAINT movie_slug_aliases_source_movie_id_check
    CHECK (source_provider IS NULL OR source_movie_id ~ '^[A-Za-z0-9_-]+$');
ALTER TABLE sync_runs ADD CONSTRAINT sync_runs_target_check
    CHECK (target ~ '^[a-z][a-z0-9_]{1,63}$');
ALTER TABLE sync_runs ADD CONSTRAINT sync_runs_trigger_occurrence_check
    CHECK ((trigger_source = 'manual' AND schedule_id IS NULL AND schedule_revision IS NULL AND scheduled_for IS NULL AND schedule_attempt IS NULL)
        OR (trigger_source = 'scheduled' AND schedule_id > 0 AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2));
ALTER TABLE sync_schedules ADD CONSTRAINT sync_schedules_target_check
    CHECK (target ~ '^[a-z][a-z0-9_]{1,63}$');
ALTER TABLE cinema_activity_coverage ADD CONSTRAINT cinema_activity_coverage_check
    CHECK ((status = 'complete' AND basis = 'date_response')
        OR (status = 'unknown' AND basis IN ('accepted_omission', 'unproven')));

-- 4. Provider helper functions are no longer referenced by any constraint.
--    cineville_decimal_valid stays: trailer candidate checks still use it.
DROP FUNCTION cinemovida_booking_valid, cinemovida_identity_valid,
    cineode_identity_valid, cineode_poster_valid, cineode_slug,
    cinewest_identity_valid, grandecran_identity_valid,
    majestic_booking_valid, majestic_identity_valid, majestic_poster_valid, majestic_website,
    mk2_identity_valid, noecinemas_identity_valid,
    ocine_identity_valid, ocine_poster_valid, ocine_showing_valid, ocine_slug,
    veo_booking_valid, veo_gueret_relay_valid, veo_identity_valid, veo_relay_decode, veo_saint_affrique_relay_valid;
