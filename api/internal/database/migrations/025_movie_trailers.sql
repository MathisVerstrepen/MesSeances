ALTER TABLE movie_metadata_cache
    ADD COLUMN trailer_youtube_key varchar(11)
    CHECK (trailer_youtube_key IS NULL OR trailer_youtube_key ~ '^[A-Za-z0-9_-]{11}$');

ALTER TABLE public_movies
    ADD COLUMN trailer_youtube_key varchar(11)
    CHECK (trailer_youtube_key IS NULL OR (confirmed_tmdb_id IS NOT NULL AND trailer_youtube_key ~ '^[A-Za-z0-9_-]{11}$'));
