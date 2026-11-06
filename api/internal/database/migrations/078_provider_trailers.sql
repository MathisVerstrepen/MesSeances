-- Provider evidence never overwrites TMDB or manual trailer fields.
CREATE TABLE movie_trailer_candidates (
 provider text NOT NULL CHECK (provider IN ('cgr','cineville','ugc')),
 source_movie_id varchar(128) NOT NULL CHECK (
  (provider='cgr' AND source_movie_id ~ '^[1-9][0-9]{0,127}$') OR
  (provider='ugc' AND source_movie_id ~ '^[1-9][0-9]*$') OR
  (provider='cineville' AND cineville_decimal_valid(source_movie_id,true))
 ),
 media_type text NOT NULL CHECK (media_type IN ('mp4','youtube')),
 url varchar(2048) NOT NULL CHECK (
  (provider IN ('cgr','ugc') AND media_type='mp4' AND url ~ '^https://fr\.vid\.web\.acsta\.net/(nmedia|medias)/[A-Za-z0-9_./-]+\.mp4$' AND substring(url FROM length('https://fr.vid.web.acsta.net')+1) !~ '(//|/\.\.?(/|$))') OR
  (provider='cineville' AND media_type='youtube' AND url ~ '^https://www\.youtube\.com/watch\?v=[A-Za-z0-9_-]{11}$')
 ),
 available boolean NOT NULL DEFAULT true,
 first_seen_at timestamptz NOT NULL,
 last_seen_at timestamptz NOT NULL CHECK (last_seen_at>=first_seen_at),
 PRIMARY KEY(provider,source_movie_id,url)
);
CREATE INDEX movie_trailer_candidates_available ON movie_trailer_candidates(provider,source_movie_id) WHERE available;

CREATE TABLE movie_trailer_lookups (
 provider text NOT NULL CHECK (provider='ugc'),
 source_movie_id varchar(128) NOT NULL CHECK (source_movie_id ~ '^[1-9][0-9]*$'),
 outcome text NOT NULL CHECK (outcome IN ('found','missing','failed')),
 checked_at timestamptz NOT NULL,
 retry_after timestamptz NOT NULL CHECK (retry_after>checked_at),
 PRIMARY KEY(provider,source_movie_id)
);

-- Only successfully fetched TMDB metadata with neither selected trailer is
-- eligible. Any explicit admin trailer override (including null) blocks fallback.
CREATE VIEW movie_trailer_eligible_sources AS
 SELECT s.public_movie_id,s.source_provider AS provider,s.source_movie_id
 FROM public_movie_sources s
 JOIN public_movies p ON p.id=s.public_movie_id AND p.redirect_to_id IS NULL
 JOIN movie_metadata_cache m ON m.provider='tmdb' AND m.locale='fr-FR' AND m.provider_movie_id=p.confirmed_tmdb_id
 LEFT JOIN public_movie_metadata_overrides o ON o.public_movie_id=p.id
 WHERE coalesce(m.trailer_vf_youtube_key,'')='' AND coalesce(m.trailer_vo_youtube_key,'')=''
 AND coalesce(p.trailer_vf_youtube_key,'')='' AND coalesce(p.trailer_vo_youtube_key,'')=''
 AND NOT coalesce(o.trailer_vf_youtube_key_overridden,false)
 AND NOT coalesce(o.trailer_vo_youtube_key_overridden,false);
