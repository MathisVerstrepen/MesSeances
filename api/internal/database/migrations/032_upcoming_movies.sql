ALTER TABLE public_movies
    ALTER COLUMN identity_anchor_provider DROP NOT NULL,
    ALTER COLUMN identity_anchor_source_movie_id DROP NOT NULL,
    ADD COLUMN identity_anchor_tmdb_id bigint,
    ADD CONSTRAINT public_movies_anchor_shape_check CHECK (
        (identity_anchor_provider IS NOT NULL AND identity_anchor_source_movie_id IS NOT NULL AND identity_anchor_tmdb_id IS NULL)
        OR (identity_anchor_provider IS NULL AND identity_anchor_source_movie_id IS NULL AND identity_anchor_tmdb_id IS NOT NULL AND identity_anchor_tmdb_id > 0)
    );
CREATE UNIQUE INDEX public_movies_tmdb_anchor_key ON public_movies(identity_anchor_tmdb_id) WHERE redirect_to_id IS NULL;

CREATE TABLE tmdb_upcoming_movies (
    tmdb_id bigint PRIMARY KEY CHECK (tmdb_id > 0),
    public_movie_id bigint NOT NULL REFERENCES public_movies(id),
    french_release_date date,
    active boolean NOT NULL,
    verified_at timestamptz NOT NULL,
    CHECK (NOT active OR french_release_date IS NOT NULL)
);
CREATE INDEX tmdb_upcoming_movies_public_movie_id_idx ON tmdb_upcoming_movies(public_movie_id);
CREATE INDEX tmdb_upcoming_movies_release_date_idx ON tmdb_upcoming_movies(french_release_date, public_movie_id) WHERE active;
CREATE TABLE tmdb_upcoming_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    completed_at timestamptz NOT NULL,
    window_from date NOT NULL,
    window_through date NOT NULL CHECK (window_through >= window_from)
);
ALTER TABLE sync_schedules DROP CONSTRAINT sync_schedules_target_check;
ALTER TABLE sync_schedules ADD CONSTRAINT sync_schedules_target_check CHECK (target IN ('ugc','kinepolis','pathe','cgr','megarama','tmdb_metadata_refresh','tmdb_upcoming_movies'));
