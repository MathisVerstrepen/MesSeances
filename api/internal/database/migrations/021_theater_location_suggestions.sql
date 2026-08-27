ALTER TABLE theater_locations
    ADD COLUMN candidate_latitude double precision,
    ADD COLUMN candidate_longitude double precision,
    ADD COLUMN candidate_postal_code text,
    ADD COLUMN candidate_city text,
    ADD COLUMN candidate_type text,
    ADD CONSTRAINT theater_locations_candidate_coordinate_pair_check
        CHECK ((candidate_latitude IS NULL) = (candidate_longitude IS NULL)),
    ADD CONSTRAINT theater_locations_candidate_latitude_check
        CHECK (candidate_latitude IS NULL OR candidate_latitude BETWEEN -90 AND 90),
    ADD CONSTRAINT theater_locations_candidate_longitude_check
        CHECK (candidate_longitude IS NULL OR candidate_longitude BETWEEN -180 AND 180),
    ADD CONSTRAINT theater_locations_candidate_postal_code_check
        CHECK (candidate_postal_code IS NULL OR btrim(candidate_postal_code) <> ''),
    ADD CONSTRAINT theater_locations_candidate_city_check
        CHECK (candidate_city IS NULL OR btrim(candidate_city) <> ''),
    ADD CONSTRAINT theater_locations_candidate_type_check
        CHECK (candidate_type IS NULL OR btrim(candidate_type) <> ''),
    ADD CONSTRAINT theater_locations_candidate_status_check
        CHECK (status = 'ambiguous' OR (
               candidate_latitude IS NULL AND candidate_longitude IS NULL AND
               candidate_postal_code IS NULL AND candidate_city IS NULL AND candidate_type IS NULL));
