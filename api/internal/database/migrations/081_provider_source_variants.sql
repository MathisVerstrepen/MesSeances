-- Admit the provider’s exact percent-encoded subtitle code for Saint-Affrique.
CREATE OR REPLACE FUNCTION veo_saint_affrique_relay_valid(value text) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE
    prefix constant text := 'https://relay.mvtx.us/ticketing/dbz?';
    pair text;
    parts text[];
    seen text[] := '{}';
    decoded text;
    day text;
    clock text;
BEGIN
    IF length(value) > 4096 OR left(value,length(prefix)) <> prefix THEN RETURN false; END IF;
    FOREACH pair IN ARRAY string_to_array(substr(value,length(prefix)+1),'&') LOOP
        parts := string_to_array(pair,'=');
        IF cardinality(parts) <> 2 OR parts[1] <> ALL(ARRAY['code_theater','gid_entity','tags','perfd','perft','code']) OR parts[1] = ANY(seen) THEN RETURN false; END IF;
        decoded := CASE WHEN parts[1] = 'code' AND parts[2] = 'VO%3B%20SUBTITLE'
            THEN 'VO; SUBTITLE' ELSE veo_relay_decode(parts[2]) END;
        IF decoded IS NULL THEN RETURN false; END IF;
        seen := array_append(seen,parts[1]);
        IF parts[1] = 'code_theater' AND decoded <> 'P0793' THEN RETURN false; END IF;
        IF parts[1] = 'code' AND decoded NOT IN ('VF','VO','VO; SUBTITLE') THEN RETURN false; END IF;
        IF parts[1] = 'perfd' THEN day := decoded; END IF;
        IF parts[1] = 'perft' THEN clock := decoded; END IF;
    END LOOP;
    IF cardinality(seen) <> 6 OR day !~ '^[0-9]{8}$' OR clock !~ '^([01][0-9]|2[0-3]):[0-5][0-9]$' OR left(day,4) = '0000' THEN RETURN false; END IF;
    PERFORM make_date(substr(day,1,4)::integer,substr(day,5,2)::integer,substr(day,7,2)::integer);
    RETURN true;
EXCEPTION WHEN datetime_field_overflow OR invalid_datetime_format THEN RETURN false;
END $$;

-- Cineode also publishes ACSTA posters with an uppercase PNG extension.
CREATE OR REPLACE FUNCTION cineode_poster_valid(value text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT value = '' OR (length(value) <= 4096
        AND (value ~ '^https://fr\.web\.img[0-9]*\.acsta\.net/(img|medias|pictures)/[A-Za-z0-9_./-]+\.(jpg|jpeg|png|webp|JPG|PNG)$'
             OR value ~ '^https://static\.cotecine\.fr/tb/Affiches/180x240/([A-Za-z0-9_+-]|%20)+\.JPG$')
        AND value !~ '/\.\.?(/|$)' AND substring(value,9) NOT LIKE '%//%')
$$;

-- Bind Ociné Boxoffice entry links to their cinema and exact reservation ID.
CREATE OR REPLACE FUNCTION ocine_identity_valid(kind text, value text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT CASE kind
        WHEN 'theater' THEN ocine_slug(value) <> ''
        WHEN 'movie' THEN length(value) <= 117 AND value ~ '^([1-9][0-9]*|[a-z][a-z0-9]*(-[a-z0-9]+)*)$'
        WHEN 'showing' THEN length(value) <= 114
            AND value ~ '^(309|310|461)-([1-9][0-9]*|[a-z][a-z0-9]*(-[a-z0-9]+)*)-[1-9][0-9]*-(past|r?[1-9][0-9]*)$'
            AND CASE WHEN substring(value from '-([0-9]+)-(past|r?[1-9][0-9]*)$') ~ '^[1-9][0-9]{0,18}$'
                THEN substring(value from '-([0-9]+)-(past|r?[1-9][0-9]*)$')::numeric <= 9223372036854775807 ELSE false END
        ELSE false END
$$;

CREATE OR REPLACE FUNCTION ocine_poster_valid(value text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT value = '' OR (length(value) <= 4096
        AND value ~ '^https://(fr\.web\.img[0-9]*|all\.web\.img)\.acsta\.net/(img|medias|pictures)/[A-Za-z0-9_./-]+\.(jpg|jpeg|png|webp|JPG|PNG)$'
        AND value !~ '/\.\.?(/|$)' AND substring(value,9) NOT LIKE '%//%')
$$;

CREATE OR REPLACE FUNCTION ocine_showing_valid(showing text, theater text, movie text, start_at timestamptz, end_at timestamptz, day date, lang text, version text, checkout text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT ocine_identity_valid('showing',showing) AND ocine_identity_valid('theater',theater)
        AND ocine_identity_valid('movie',movie) AND start_at = date_trunc('second',start_at)
        AND day = (start_at AT TIME ZONE 'Europe/Paris')::date
        AND showing = theater || '-' || movie || '-' || trunc(extract(epoch from start_at))::text || '-' || substring(showing from '-(past|r?[1-9][0-9]*)$')
        AND CASE WHEN right(showing,5) = '-past' THEN
            checkout = '' AND end_at = start_at AND ((lang = '' AND version = '') OR (lang = 'VF' AND version = 'VF') OR (lang = 'VOSTFR' AND version = 'VOSTF'))
        WHEN showing ~ '-r[1-9][0-9]*$' THEN
            end_at >= start_at AND length(checkout) <= 4096
            AND ((lang = 'VF' AND version = 'VF') OR (lang = 'VOSTFR' AND version = 'VOSTF') OR (lang = 'VFSTF' AND version = 'VFSTF'))
            AND checkout = 'https://achat.ocine.fr/' || ocine_slug(theater) || '/r/' || substring(showing from '-r([1-9][0-9]*)$')
        ELSE end_at >= start_at AND length(checkout) <= 4096
            AND checkout ~ ('^https://www\.ocine\.fr/' || ocine_slug(theater) || '/reserver/F[1-9][0-9]*/D' || trunc(extract(epoch from start_at))::text || '/' || CASE WHEN lang = 'VF' AND version = 'VF' THEN 'VF' WHEN lang = 'VOSTFR' AND version = 'VOSTF' THEN 'VO' ELSE 'INVALID' END || '/' || substring(showing from '-([1-9][0-9]*)$') || '/$')
            AND (movie !~ '^[0-9]+$' OR substring(checkout from '/reserver/F([1-9][0-9]*)/') = movie)
        END
$$;
