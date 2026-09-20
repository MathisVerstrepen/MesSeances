ALTER TABLE movie_metadata_cache
    ADD COLUMN original_language varchar(2),
    ADD CONSTRAINT movie_metadata_cache_original_language_check
        CHECK (original_language ~ '^[a-z]{2}$');

ALTER TABLE public_movies
    ADD COLUMN original_language varchar(2),
    ADD CONSTRAINT public_movies_original_language_check
        CHECK (original_language ~ '^[a-z]{2}$'),
    ADD CONSTRAINT public_movies_original_language_tmdb_check
        CHECK (original_language IS NULL OR confirmed_tmdb_id IS NOT NULL);

ALTER TABLE showtimes
    ADD CONSTRAINT showtimes_language_original_check CHECK (language <> 'ORIGINAL');

ALTER TABLE screening_history_showtimes
    ADD CONSTRAINT screening_history_showtimes_language_original_check CHECK (language <> 'ORIGINAL');
