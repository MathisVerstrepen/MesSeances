ALTER TABLE movies DROP CONSTRAINT movies_runtime_minutes_positive_check;
ALTER TABLE movies ADD CONSTRAINT movies_runtime_minutes_nonnegative_check CHECK (runtime_minutes >= 0);

ALTER TABLE movie_matches DROP CONSTRAINT movie_matches_source_runtime_minutes_positive_check;
ALTER TABLE movie_matches ADD CONSTRAINT movie_matches_source_runtime_minutes_nonnegative_check CHECK (source_runtime_minutes >= 0);

ALTER TABLE movie_metadata_cache DROP CONSTRAINT movie_metadata_cache_runtime_minutes_positive_check;
ALTER TABLE movie_metadata_cache ADD CONSTRAINT movie_metadata_cache_runtime_minutes_nonnegative_check CHECK (runtime_minutes >= 0);

ALTER TABLE public_movies DROP CONSTRAINT public_movies_runtime_minutes_check;
ALTER TABLE public_movies ADD CONSTRAINT public_movies_runtime_minutes_nonnegative_check CHECK (runtime_minutes >= 0);

ALTER TABLE public_movie_sources DROP CONSTRAINT public_movie_sources_runtime_minutes_check;
ALTER TABLE public_movie_sources ADD CONSTRAINT public_movie_sources_runtime_minutes_nonnegative_check CHECK (runtime_minutes >= 0);
