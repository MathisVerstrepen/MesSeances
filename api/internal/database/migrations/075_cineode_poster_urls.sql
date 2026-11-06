-- Cinéode publishes uppercase Acsta JPGs and percent-encoded Cotecine spaces.
-- Preserve source URL bytes and every existing host/path restriction.
CREATE OR REPLACE FUNCTION cineode_poster_valid(value text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT value = '' OR (length(value) <= 4096
        AND (value ~ '^https://fr\.web\.img[0-9]*\.acsta\.net/(img|medias|pictures)/[A-Za-z0-9_./-]+\.(jpg|jpeg|png|webp|JPG)$'
             OR value ~ '^https://static\.cotecine\.fr/tb/Affiches/180x240/([A-Za-z0-9_+-]|%20)+\.JPG$')
        AND value !~ '/\.\.?(/|$)' AND substring(value,9) NOT LIKE '%//%')
$$;
