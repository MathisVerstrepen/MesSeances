CREATE TABLE movie_matches (
    source_provider varchar(32) NOT NULL,
    source_movie_id varchar(128) NOT NULL,
    metadata_provider varchar(32) NOT NULL,
    status varchar(32) NOT NULL CHECK (status IN ('matched', 'review_required', 'unmatched')),
    metadata_movie_id bigint,
    score double precision CHECK (score IS NULL OR score BETWEEN 0 AND 1),
    normalized_source_title varchar(1024) NOT NULL CHECK (btrim(normalized_source_title) <> ''),
    source_runtime_minutes smallint NOT NULL CHECK (source_runtime_minutes BETWEEN 1 AND 600),
    candidates jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(candidates) = 'array' AND jsonb_array_length(candidates) <= 5),
    evaluated_at timestamptz NOT NULL,
    retry_after timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (source_provider, source_movie_id, metadata_provider),
    CHECK (source_provider = 'ugc'),
    CHECK (metadata_provider = 'tmdb'),
    CHECK (source_movie_id ~ '^[1-9][0-9]*$'),
    CHECK ((status = 'matched' AND metadata_movie_id > 0 AND score IS NOT NULL) OR
           (status <> 'matched' AND metadata_movie_id IS NULL AND score IS NULL))
);

CREATE INDEX movie_matches_retry_idx ON movie_matches (status, retry_after);

CREATE TABLE movie_metadata_cache (
    provider varchar(32) NOT NULL,
    provider_movie_id bigint NOT NULL CHECK (provider_movie_id > 0),
    locale varchar(16) NOT NULL,
    provider_title varchar(1024) NOT NULL CHECK (btrim(provider_title) <> ''),
    localized_title varchar(1024) NOT NULL CHECK (btrim(localized_title) <> ''),
    overview varchar(10000),
    release_date date,
    poster_url varchar(4096),
    runtime_minutes smallint NOT NULL CHECK (runtime_minutes BETWEEN 1 AND 600),
    genres text[] NOT NULL DEFAULT '{}',
    fetched_at timestamptz NOT NULL,
    refresh_after timestamptz NOT NULL,
    PRIMARY KEY (provider, provider_movie_id, locale),
    CHECK (provider = 'tmdb'),
    CHECK (locale = 'fr-FR'),
    CHECK (overview IS NULL OR length(overview) <= 10000),
    CHECK (poster_url IS NULL OR poster_url ~ '^https://'),
    CHECK (cardinality(genres) <= 32)
);

CREATE TABLE movie_enrichment_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    version bigint NOT NULL CHECK (version >= 0)
);

INSERT INTO movie_enrichment_state (singleton, version) VALUES (true, 0);
