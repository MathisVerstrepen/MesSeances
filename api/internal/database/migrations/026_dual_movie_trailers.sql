ALTER TABLE movie_metadata_cache
    DROP COLUMN trailer_youtube_key,
    ADD COLUMN trailer_vf_youtube_key varchar(11),
    ADD COLUMN trailer_vo_youtube_key varchar(11),
    ADD CHECK (trailer_vf_youtube_key IS NULL OR trailer_vf_youtube_key ~ '^[A-Za-z0-9_-]{11}$'),
    ADD CHECK (trailer_vo_youtube_key IS NULL OR trailer_vo_youtube_key ~ '^[A-Za-z0-9_-]{11}$'),
    ADD CHECK (trailer_vf_youtube_key IS NULL OR trailer_vo_youtube_key IS NULL OR trailer_vf_youtube_key <> trailer_vo_youtube_key);

ALTER TABLE public_movies
    DROP COLUMN trailer_youtube_key,
    ADD COLUMN trailer_vf_youtube_key varchar(11),
    ADD COLUMN trailer_vo_youtube_key varchar(11),
    ADD CHECK (trailer_vf_youtube_key IS NULL OR (confirmed_tmdb_id IS NOT NULL AND trailer_vf_youtube_key ~ '^[A-Za-z0-9_-]{11}$')),
    ADD CHECK (trailer_vo_youtube_key IS NULL OR (confirmed_tmdb_id IS NOT NULL AND trailer_vo_youtube_key ~ '^[A-Za-z0-9_-]{11}$')),
    ADD CHECK (trailer_vf_youtube_key IS NULL OR trailer_vo_youtube_key IS NULL OR trailer_vf_youtube_key <> trailer_vo_youtube_key);
