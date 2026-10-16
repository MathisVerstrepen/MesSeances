-- Structural policy only. Acquisition binds source start/tags and the private
-- checkout chain binds its final epoch. Preserve original URLs, rows and CHECKs.
CREATE FUNCTION veo_relay_decode(value text) RETURNS text
LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE
    result text := '';
    i integer := 1;
    c text;
BEGIN
    IF length(value) NOT BETWEEN 1 AND 3072 OR value !~ '^[A-Za-z0-9._~,:%-]+$' THEN RETURN NULL; END IF;
    WHILE i <= length(value) LOOP
        c := substr(value,i,1);
        IF c = '%' THEN
            IF substr(value,i+1,2) !~ '^[0-9A-Fa-f]{2}$' THEN RETURN NULL; END IF;
            -- Restrict before chr: NUL and multibyte sequences are never valid.
            IF get_byte(decode(substr(value,i+1,2),'hex'),0) NOT BETWEEN 33 AND 126 THEN RETURN NULL; END IF;
            c := chr(get_byte(decode(substr(value,i+1,2),'hex'),0));
            i := i+2;
        END IF;
        IF c !~ '^[A-Za-z0-9._~,:-]$' THEN RETURN NULL; END IF;
        result := result || c;
        i := i+1;
    END LOOP;
    IF length(result) NOT BETWEEN 1 AND 1024 THEN RETURN NULL; END IF;
    RETURN result;
END $$;

CREATE FUNCTION veo_gueret_relay_valid(value text) RETURNS boolean
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
        IF cardinality(parts) <> 2 OR parts[1] <> ALL(ARRAY['code_theater','gid_entity','tags','perfd','perft']) OR parts[1] = ANY(seen) THEN RETURN false; END IF;
        decoded := veo_relay_decode(parts[2]);
        IF decoded IS NULL THEN RETURN false; END IF;
        seen := array_append(seen,parts[1]);
        IF parts[1] = 'code_theater' AND decoded <> 'P1394' THEN RETURN false; END IF;
        IF parts[1] = 'perfd' THEN day := decoded; END IF;
        IF parts[1] = 'perft' THEN clock := decoded; END IF;
    END LOOP;
    IF cardinality(seen) <> 5 OR day !~ '^[0-9]{8}$' OR clock !~ '^([01][0-9]|2[0-3]):[0-5][0-9]$' OR left(day,4) = '0000' THEN RETURN false; END IF;
    PERFORM make_date(substr(day,1,4)::integer,substr(day,5,2)::integer,substr(day,7,2)::integer);
    RETURN true;
EXCEPTION WHEN datetime_field_overflow OR invalid_datetime_format THEN RETURN false;
END $$;

CREATE OR REPLACE FUNCTION veo_booking_valid(value text, theater text, showing text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT length(value) <= 4096 AND veo_identity_valid('theater', theater)
        AND veo_identity_valid('showing', showing)
        AND split_part(showing, '-', 1) = theater
        AND CASE WHEN theater = 'EMS1361' THEN
            value IN ('https://castelginest.miracinemas.fr','https://castelginest.miracinemas.fr/')
            OR (value ~ '^https://castelginest\.miracinemas\.fr/?#showsession\?id=emsx1361[0-9]{8}$'
                AND showing = 'EMS1361-' || encode(sha256(convert_to('EMS1361','UTF8') || decode('00','hex') || convert_to(substring(value from '#showsession\?id=(emsx1361[0-9]{8})$'),'UTF8')), 'hex'))
        ELSE value ~ '^https://achat\.veocinemas\.fr/[a-z0-9]+(-[a-z0-9]+)*/(reserver/)?r/[1-9][0-9]*$'
            OR (theater = 'G06DB' AND veo_gueret_relay_valid(value)) END
$$;
