-- Accept the second exact Véo source alias without rewriting bookings or CHECKs.
CREATE OR REPLACE FUNCTION veo_booking_valid(value text, theater text, showing text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT length(value) <= 4096 AND veo_identity_valid('theater', theater)
        AND veo_identity_valid('showing', showing)
        AND split_part(showing, '-', 1) = theater
        AND CASE WHEN theater = 'EMS1361' THEN
            value IN ('https://castelginest.miracinemas.fr','https://castelginest.miracinemas.fr/')
            OR (value ~ '^https://castelginest\.miracinemas\.fr/?#showsession\?id=emsx1361[0-9]{8}$'
                AND showing = 'EMS1361-' || encode(sha256(convert_to('EMS1361','UTF8') || decode('00','hex') || convert_to(substring(value from '#showsession\?id=(emsx1361[0-9]{8})$'),'UTF8')), 'hex'))
        ELSE value ~ '^https://achat\.veocinemas\.fr/[a-z0-9]+(-[a-z0-9]+)*/(reserver/)?r/[1-9][0-9]*$' END
$$;
