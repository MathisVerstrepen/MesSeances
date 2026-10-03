ALTER TABLE movie_metadata_cache
    ADD COLUMN metacritic_id varchar(255),
    ADD CONSTRAINT movie_metadata_cache_metacritic_id_check CHECK (
        metacritic_id IS NULL OR (
            octet_length(metacritic_id) BETWEEN 7 AND 255
            AND metacritic_id COLLATE "C" ~ '^movie/[a-z0-9!+_()-]+$'
        )
    );

ALTER TABLE public_movies
    ADD COLUMN metacritic_id varchar(255),
    ADD CONSTRAINT public_movies_metacritic_id_check CHECK (
        metacritic_id IS NULL OR (
            confirmed_tmdb_id IS NOT NULL
            AND octet_length(metacritic_id) BETWEEN 7 AND 255
            AND metacritic_id COLLATE "C" ~ '^movie/[a-z0-9!+_()-]+$'
        )
    );
