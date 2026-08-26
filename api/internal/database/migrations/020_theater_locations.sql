CREATE TABLE theater_location_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    version bigint NOT NULL CHECK (version >= 0)
);

INSERT INTO theater_location_state (singleton, version) VALUES (true, 0);

CREATE TABLE theater_locations (
    provider varchar(32) NOT NULL CHECK (provider IN ('ugc', 'kinepolis', 'pathe', 'cgr')),
    provider_theater_id varchar(128) NOT NULL CHECK (btrim(provider_theater_id) <> ''),
    latitude double precision,
    longitude double precision,
    source varchar(32) NOT NULL,
    matched_label text,
    match_score double precision CHECK (match_score IS NULL OR match_score BETWEEN 0 AND 1),
    address_hash char(64),
    status varchar(32) NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (provider, provider_theater_id),
    CHECK ((latitude IS NULL) = (longitude IS NULL)),
    CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180),
    CHECK ((source = 'ign' AND status IN ('matched', 'ambiguous', 'not_found')) OR
           (source = 'manual' AND status = 'manual')),
    CHECK ((status IN ('matched', 'manual')) = (latitude IS NOT NULL)),
    CHECK ((source = 'ign' AND address_hash ~ '^[a-f0-9]{64}$') OR
           (source = 'manual' AND address_hash IS NULL)),
    CHECK ((status IN ('matched', 'ambiguous') AND matched_label IS NOT NULL AND btrim(matched_label) <> '' AND match_score IS NOT NULL) OR
           (status IN ('not_found', 'manual') AND matched_label IS NULL AND match_score IS NULL))
);
