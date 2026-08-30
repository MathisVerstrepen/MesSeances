CREATE FUNCTION public_movie_metadata_override_genres_valid(values_to_check text[])
RETURNS boolean
LANGUAGE sql
IMMUTABLE
STRICT
AS $$
    SELECT cardinality(values_to_check) <= 32
       AND COALESCE(bool_and(value IS NOT NULL AND btrim(value) <> '' AND length(value) <= 256), true)
    FROM unnest(values_to_check) AS value
$$;

CREATE TABLE public_movie_metadata_overrides (
    public_movie_id bigint PRIMARY KEY REFERENCES public_movies(id) ON DELETE CASCADE,

    title varchar(1024),
    title_overridden boolean NOT NULL DEFAULT false,
    runtime_minutes integer,
    runtime_minutes_overridden boolean NOT NULL DEFAULT false,
    release_date date,
    release_date_overridden boolean NOT NULL DEFAULT false,
    genres text[],
    genres_overridden boolean NOT NULL DEFAULT false,
    overview varchar(10000),
    overview_overridden boolean NOT NULL DEFAULT false,
    poster_url varchar(4096),
    poster_url_overridden boolean NOT NULL DEFAULT false,
    backdrop_url varchar(4096),
    backdrop_url_overridden boolean NOT NULL DEFAULT false,
    trailer_vf_youtube_key varchar(11),
    trailer_vf_youtube_key_overridden boolean NOT NULL DEFAULT false,
    trailer_vo_youtube_key varchar(11),
    trailer_vo_youtube_key_overridden boolean NOT NULL DEFAULT false,

    CONSTRAINT public_movie_metadata_overrides_has_active_field CHECK (
        title_overridden OR runtime_minutes_overridden OR release_date_overridden OR genres_overridden OR
        overview_overridden OR poster_url_overridden OR backdrop_url_overridden OR
        trailer_vf_youtube_key_overridden OR trailer_vo_youtube_key_overridden
    ),
    CONSTRAINT public_movie_metadata_overrides_inactive_values_null CHECK (
        (title_overridden OR title IS NULL) AND
        (runtime_minutes_overridden OR runtime_minutes IS NULL) AND
        (release_date_overridden OR release_date IS NULL) AND
        (genres_overridden OR genres IS NULL) AND
        (overview_overridden OR overview IS NULL) AND
        (poster_url_overridden OR poster_url IS NULL) AND
        (backdrop_url_overridden OR backdrop_url IS NULL) AND
        (trailer_vf_youtube_key_overridden OR trailer_vf_youtube_key IS NULL) AND
        (trailer_vo_youtube_key_overridden OR trailer_vo_youtube_key IS NULL)
    ),
    CONSTRAINT public_movie_metadata_overrides_title_valid CHECK (
        NOT title_overridden OR title IS NOT NULL AND btrim(title) <> ''
    ),
    CONSTRAINT public_movie_metadata_overrides_runtime_valid CHECK (
        NOT runtime_minutes_overridden OR runtime_minutes IS NOT NULL AND runtime_minutes >= 0
    ),
    CONSTRAINT public_movie_metadata_overrides_genres_valid CHECK (
        NOT genres_overridden OR genres IS NOT NULL AND public_movie_metadata_override_genres_valid(genres)
    ),
    CONSTRAINT public_movie_metadata_overrides_poster_url_valid CHECK (
        NOT poster_url_overridden OR poster_url IS NULL OR poster_url ~ '^https://[^[:space:]/?#]+([/?#][^[:space:]]*)?$'
    ),
    CONSTRAINT public_movie_metadata_overrides_backdrop_url_valid CHECK (
        NOT backdrop_url_overridden OR backdrop_url IS NULL OR backdrop_url ~ '^https://[^[:space:]/?#]+([/?#][^[:space:]]*)?$'
    ),
    CONSTRAINT public_movie_metadata_overrides_trailer_vf_valid CHECK (
        NOT trailer_vf_youtube_key_overridden OR trailer_vf_youtube_key IS NULL OR trailer_vf_youtube_key ~ '^[A-Za-z0-9_-]{11}$'
    ),
    CONSTRAINT public_movie_metadata_overrides_trailer_vo_valid CHECK (
        NOT trailer_vo_youtube_key_overridden OR trailer_vo_youtube_key IS NULL OR trailer_vo_youtube_key ~ '^[A-Za-z0-9_-]{11}$'
    ),
    CONSTRAINT public_movie_metadata_overrides_distinct_trailers CHECK (
        trailer_vf_youtube_key IS NULL OR trailer_vo_youtube_key IS NULL OR trailer_vf_youtube_key <> trailer_vo_youtube_key
    )
);
