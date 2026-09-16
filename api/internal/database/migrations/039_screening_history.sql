-- Start fresh. LIKE copies scalar types, NOT NULL and the complete provider
-- CHECK predicates as of 038, but deliberately not indexes or foreign keys.
CREATE TABLE screening_history_providers (
    provider varchar(32) PRIMARY KEY CHECK (provider IN ('ugc','kinepolis','pathe','cgr','megarama','cineville','mk2','cinewest','grandecran','noecinemas')),
    collection_started_at timestamptz NOT NULL,
    last_publication_at timestamptz NOT NULL CHECK (last_publication_at >= collection_started_at),
    last_generation bigint NOT NULL CHECK (last_generation > 0),
    source_generated_at timestamptz NOT NULL
);

CREATE TABLE screening_history_theaters (LIKE theaters INCLUDING CONSTRAINTS);
ALTER TABLE screening_history_theaters
    DROP COLUMN generation_id,
    ADD PRIMARY KEY (id),
    ADD UNIQUE (provider, id),
    ADD COLUMN city_slug text NOT NULL,
    ADD COLUMN city_name varchar(256) NOT NULL,
    ADD COLUMN passes text[] NOT NULL CHECK (passes = '{}'::text[] OR passes = ARRAY['UGC_ILLIMITE']),
    ADD COLUMN last_observed_at timestamptz NOT NULL;

CREATE TABLE screening_history_showtimes (LIKE showtimes INCLUDING CONSTRAINTS);
ALTER TABLE screening_history_showtimes
    DROP COLUMN generation_id,
    ADD PRIMARY KEY (provider, provider_showing_id, theater_id, service_date),
    ADD FOREIGN KEY (provider, theater_id) REFERENCES screening_history_theaters(provider, id),
    ADD FOREIGN KEY (provider, movie_provider_id) REFERENCES public_movie_sources(source_provider, source_movie_id),
    ADD COLUMN first_seen_at timestamptz NOT NULL,
    ADD COLUMN last_seen_at timestamptz NOT NULL CHECK (last_seen_at >= first_seen_at),
    ADD COLUMN source_generated_at timestamptz NOT NULL,
    ADD COLUMN last_generation bigint NOT NULL CHECK (last_generation > 0);

CREATE INDEX screening_history_date_theater_idx ON screening_history_showtimes(service_date, theater_id);
CREATE INDEX screening_history_theater_date_idx ON screening_history_showtimes(theater_id, service_date);
CREATE INDEX screening_history_movie_date_idx ON screening_history_showtimes(provider, movie_provider_id, service_date);
CREATE INDEX screening_history_city_idx ON screening_history_theaters(city_slug, id);
