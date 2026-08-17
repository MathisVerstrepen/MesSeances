ALTER TABLE schedule_snapshot DROP CONSTRAINT schedule_snapshot_provider_check;
ALTER TABLE schedule_snapshot ADD CONSTRAINT schedule_snapshot_provider_check CHECK (provider IN ('ugc', 'kinepolis', 'combined'));
CREATE TABLE provider_snapshots (
 provider varchar(32) PRIMARY KEY CHECK (provider IN ('ugc', 'kinepolis')),
 schema_version integer NOT NULL CHECK (schema_version = 1), scope varchar(32) NOT NULL CHECK (scope = 'all_cinemas'),
 generated_at timestamptz NOT NULL, timezone varchar(64) NOT NULL CHECK (timezone = 'Europe/Paris'),
 window_from date NOT NULL, window_through date NOT NULL,
 CHECK (window_through >= window_from AND window_through <= window_from + 13)
);
INSERT INTO provider_snapshots SELECT provider, schema_version, scope, generated_at, timezone, window_from, window_through FROM schedule_snapshot;

ALTER TABLE theaters ADD COLUMN provider varchar(32) NOT NULL DEFAULT 'ugc' CHECK (provider IN ('ugc', 'kinepolis'));
ALTER TABLE theaters DROP CONSTRAINT theaters_address_check;
ALTER TABLE theaters DROP CONSTRAINT theaters_postal_code_check;
ALTER TABLE theaters ADD CONSTRAINT theaters_address_check CHECK (provider='kinepolis' OR btrim(address) <> '');
ALTER TABLE theaters ADD CONSTRAINT theaters_postal_code_check CHECK (provider='kinepolis' OR btrim(postal_code) <> '');
ALTER TABLE theaters DROP CONSTRAINT theaters_provider_id_key;
ALTER TABLE theaters DROP CONSTRAINT theaters_provider_id_check;
ALTER TABLE theaters DROP CONSTRAINT theaters_check;
ALTER TABLE theaters DROP CONSTRAINT theaters_check1;
ALTER TABLE theaters ADD CONSTRAINT theaters_provider_identity_check CHECK ((provider='ugc' AND provider_id ~ '^[1-9][0-9]*$') OR (provider='kinepolis' AND provider_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]*$'));
ALTER TABLE theaters ADD CONSTRAINT theaters_id_check CHECK (id = provider || '-' || provider_id);
ALTER TABLE theaters ADD CONSTRAINT theaters_slug_check CHECK (slug = id);
ALTER TABLE theaters ADD UNIQUE (provider, provider_id);

ALTER TABLE movies ADD COLUMN provider varchar(32) NOT NULL DEFAULT 'ugc' CHECK (provider IN ('ugc', 'kinepolis'));
ALTER TABLE movies ADD COLUMN source_overview varchar(10000);
ALTER TABLE movies ADD COLUMN source_release_date date;
ALTER TABLE movies ADD COLUMN source_genres text[] NOT NULL DEFAULT '{}';
ALTER TABLE showtimes DROP CONSTRAINT showtimes_movie_provider_id_fkey;
ALTER TABLE movies DROP CONSTRAINT movies_pkey;
ALTER TABLE movies DROP CONSTRAINT movies_provider_id_check;
ALTER TABLE movies DROP CONSTRAINT movies_check;
ALTER TABLE movies ADD CONSTRAINT movies_provider_identity_check CHECK ((provider='ugc' AND provider_id ~ '^[1-9][0-9]*$') OR (provider='kinepolis' AND provider_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]*$'));
ALTER TABLE movies ADD CONSTRAINT movies_slug_check CHECK (slug = provider || '-film-' || provider_id);
ALTER TABLE movies ADD PRIMARY KEY (provider, provider_id);
ALTER TABLE movies ADD CHECK (source_overview IS NULL OR length(source_overview) <= 10000);
ALTER TABLE movies ADD CHECK (cardinality(source_genres) <= 32);

ALTER TABLE showtimes ADD COLUMN provider varchar(32) NOT NULL DEFAULT 'ugc' CHECK (provider IN ('ugc', 'kinepolis'));
ALTER TABLE showtimes DROP CONSTRAINT showtimes_provider_showing_id_key;
ALTER TABLE showtimes DROP CONSTRAINT showtimes_provider_showing_id_check;
ALTER TABLE showtimes DROP CONSTRAINT showtimes_check;
ALTER TABLE showtimes ADD CONSTRAINT showtimes_provider_identity_check CHECK ((provider='ugc' AND provider_showing_id ~ '^[1-9][0-9]*$') OR (provider='kinepolis' AND provider_showing_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]*$'));
ALTER TABLE showtimes ADD CONSTRAINT showtimes_id_check CHECK (id = provider || '-showing-' || provider_showing_id);
ALTER TABLE showtimes ADD UNIQUE (provider, provider_showing_id);
ALTER TABLE showtimes ADD FOREIGN KEY (provider, movie_provider_id) REFERENCES movies(provider, provider_id);

ALTER TABLE movie_matches DROP CONSTRAINT movie_matches_source_provider_check;
ALTER TABLE movie_matches DROP CONSTRAINT movie_matches_source_movie_id_check;
ALTER TABLE movie_matches ADD CONSTRAINT movie_matches_source_provider_check CHECK (source_provider IN ('ugc', 'kinepolis'));
ALTER TABLE movie_matches ADD CONSTRAINT movie_matches_source_movie_id_check CHECK ((source_provider='ugc' AND source_movie_id ~ '^[1-9][0-9]*$') OR (source_provider='kinepolis' AND source_movie_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]*$'));
