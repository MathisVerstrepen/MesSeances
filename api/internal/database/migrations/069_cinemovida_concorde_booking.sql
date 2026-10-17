-- Admit only Concorde's additional original route; preserve CHECKs and rows.
CREATE OR REPLACE FUNCTION cinemovida_booking_valid(value text, theater text, showing text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT length(value) <= 4096 AND cinemovida_identity_valid('theater',theater)
        AND cinemovida_identity_valid('showing',showing) AND split_part(showing,'-',1) = theater
        AND (value ~ ('^https://' || CASE theater
            WHEN 'P0176' THEN 'achat\.cinema-castillet\.com'
            WHEN 'P0218' THEN 'achat\.cinema-megacastillet\.com'
            WHEN 'P7951' THEN 'achat\.cinemaleselect-granville\.com'
            WHEN 'P0876' THEN 'achat\.concordecinema\.com'
            ELSE 'invalid' END || '/reserver/r/[1-9][0-9]*$')
            OR (theater = 'P0876' AND value ~ '^https://achat\.concordecinema\.com/r/[1-9][0-9]*$'))
$$;
