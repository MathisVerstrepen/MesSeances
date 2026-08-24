LOCK TABLE showtimes IN ACCESS EXCLUSIVE MODE;

DO $$
DECLARE
    booking_pattern constant text := '^https://s[.]pathe[.]fr/fr/(V[1-9][0-9]*S[1-9][0-9]*)/booking$';
BEGIN
    IF EXISTS (
        SELECT 1
        FROM showtimes AS showing
        CROSS JOIN LATERAL (
            SELECT substring(showing.booking_url FROM booking_pattern) AS token
        ) AS booking
        WHERE showing.provider = 'pathe'
          AND (
              booking.token IS NULL OR
              length(booking.token) > 128 - length('pathe-showing-') OR
              NOT (
                  showing.provider_showing_id = booking.token AND
                  showing.id = 'pathe-showing-' || booking.token
              ) AND NOT (
                  showing.provider_showing_id ~ '^S[1-9][0-9]*$' AND
                  showing.provider_showing_id = substring(booking.token FROM 'S[1-9][0-9]*$') AND
                  showing.id = 'pathe-showing-' || showing.provider_showing_id
              )
          )
    ) THEN
        RAISE EXCEPTION 'cannot migrate Pathé showing identities with invalid booking identity';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM showtimes AS showing
        CROSS JOIN LATERAL (
            SELECT substring(showing.booking_url FROM booking_pattern) AS token
        ) AS booking
        WHERE showing.provider = 'pathe'
        GROUP BY showing.generation_id, booking.token
        HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'cannot migrate Pathé showing identities with collisions';
    END IF;
END
$$;

ALTER TABLE showtimes DROP CONSTRAINT showtimes_provider_identity_check;

UPDATE showtimes
SET provider_showing_id = substring(booking_url FROM '^https://s[.]pathe[.]fr/fr/(V[1-9][0-9]*S[1-9][0-9]*)/booking$'),
    id = 'pathe-showing-' || substring(booking_url FROM '^https://s[.]pathe[.]fr/fr/(V[1-9][0-9]*S[1-9][0-9]*)/booking$')
WHERE provider = 'pathe'
  AND provider_showing_id ~ '^S[1-9][0-9]*$';

ALTER TABLE showtimes ADD CONSTRAINT showtimes_provider_identity_check CHECK (
    (provider = 'ugc' AND provider_showing_id ~ '^[1-9][0-9]*$') OR
    (provider = 'kinepolis' AND provider_showing_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$') OR
    (provider = 'pathe' AND provider_showing_id ~ '^V[1-9][0-9]*S[1-9][0-9]*$')
);
