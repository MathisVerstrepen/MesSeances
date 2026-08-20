ALTER TABLE movies DROP CONSTRAINT movies_runtime_minutes_check;
ALTER TABLE movies ALTER COLUMN runtime_minutes TYPE integer USING runtime_minutes::integer;
ALTER TABLE movies ADD CONSTRAINT movies_runtime_minutes_positive_check CHECK (runtime_minutes > 0);

ALTER TABLE movie_matches DROP CONSTRAINT movie_matches_source_runtime_minutes_check;
ALTER TABLE movie_matches ALTER COLUMN source_runtime_minutes TYPE integer USING source_runtime_minutes::integer;
ALTER TABLE movie_matches ADD CONSTRAINT movie_matches_source_runtime_minutes_positive_check CHECK (source_runtime_minutes > 0);

ALTER TABLE movie_metadata_cache DROP CONSTRAINT movie_metadata_cache_runtime_minutes_check;
ALTER TABLE movie_metadata_cache ALTER COLUMN runtime_minutes TYPE integer USING runtime_minutes::integer;
ALTER TABLE movie_metadata_cache ADD CONSTRAINT movie_metadata_cache_runtime_minutes_positive_check CHECK (runtime_minutes > 0);
