ALTER TABLE movie_metadata_cache
    ADD COLUMN imdb_id varchar(32),
    ADD CHECK (imdb_id IS NULL OR imdb_id ~ '^tt[0-9]{7,30}$');

ALTER TABLE public_movies
    ADD COLUMN imdb_id varchar(32),
    ADD CHECK (imdb_id IS NULL OR (confirmed_tmdb_id IS NOT NULL AND imdb_id ~ '^tt[0-9]{7,30}$'));
