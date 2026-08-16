ALTER TABLE movie_metadata_cache
    ADD COLUMN backdrop_url varchar(4096),
    ADD CONSTRAINT movie_metadata_cache_backdrop_url_check CHECK (
        backdrop_url IS NULL OR (
            backdrop_url LIKE 'https://image.tmdb.org/t/p/w780/%' AND
            length(backdrop_url) > length('https://image.tmdb.org/t/p/w780/') AND
            backdrop_url NOT LIKE 'https://image.tmdb.org/t/p/w780//%' AND
            position('%' IN backdrop_url) = 0 AND
            position('?' IN backdrop_url) = 0 AND
            position('#' IN backdrop_url) = 0 AND
            position(E'\\' IN backdrop_url) = 0 AND
            position('..' IN backdrop_url) = 0
        )
    );

UPDATE movie_metadata_cache
SET refresh_after = LEAST(refresh_after, CURRENT_TIMESTAMP);
