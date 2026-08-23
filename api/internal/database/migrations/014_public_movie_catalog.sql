CREATE FUNCTION public_movie_genres_valid(values_to_check text[])
RETURNS boolean
LANGUAGE sql
IMMUTABLE
STRICT
AS $$
    SELECT cardinality(values_to_check) <= 32
       AND COALESCE(bool_and(value IS NOT NULL AND btrim(value) <> '' AND length(value) <= 256), true)
    FROM unnest(values_to_check) AS value
$$;

CREATE TABLE public_movies (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY CHECK (id > 0),
    redirect_to_id bigint REFERENCES public_movies(id),
    identity_anchor_provider varchar(32) NOT NULL CHECK (identity_anchor_provider IN ('ugc', 'kinepolis')),
    identity_anchor_source_movie_id varchar(128) NOT NULL,
    title varchar(1024) NOT NULL CHECK (btrim(title) <> ''),
    runtime_minutes integer NOT NULL CHECK (runtime_minutes > 0),
    poster_url varchar(4096) CHECK (poster_url IS NULL OR poster_url ~ '^https://'),
    backdrop_url varchar(4096) CHECK (backdrop_url IS NULL OR backdrop_url ~ '^https://image[.]tmdb[.]org/t/p/w780/[^/?#\\]+$'),
    overview varchar(10000),
    release_date date,
    genres text[] NOT NULL DEFAULT '{}' CHECK (public_movie_genres_valid(genres)),
    confirmed_tmdb_id bigint CHECK (confirmed_tmdb_id > 0),
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (redirect_to_id IS NULL OR redirect_to_id <> id),
    CHECK ((identity_anchor_provider = 'ugc' AND identity_anchor_source_movie_id ~ '^[1-9][0-9]*$') OR
           (identity_anchor_provider = 'kinepolis' AND identity_anchor_source_movie_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]*$'))
);

CREATE UNIQUE INDEX public_movies_active_tmdb_id_key
    ON public_movies (confirmed_tmdb_id)
    WHERE redirect_to_id IS NULL AND confirmed_tmdb_id IS NOT NULL;
CREATE UNIQUE INDEX public_movies_anchor_key
    ON public_movies (identity_anchor_provider, identity_anchor_source_movie_id)
    WHERE redirect_to_id IS NULL;

CREATE TABLE public_movie_sources (
    source_provider varchar(32) NOT NULL CHECK (source_provider IN ('ugc', 'kinepolis')),
    source_movie_id varchar(128) NOT NULL,
    public_movie_id bigint NOT NULL REFERENCES public_movies(id),
    source_slug varchar(128) NOT NULL CHECK (btrim(source_slug) <> ''),
    title varchar(1024) NOT NULL CHECK (btrim(title) <> ''),
    runtime_minutes integer NOT NULL CHECK (runtime_minutes > 0),
    poster_url varchar(4096) CHECK (poster_url IS NULL OR poster_url ~ '^https://'),
    overview varchar(10000),
    release_date date,
    genres text[] NOT NULL DEFAULT '{}' CHECK (public_movie_genres_valid(genres)),
    first_seen_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (source_provider, source_movie_id),
    CHECK ((source_provider = 'ugc' AND source_movie_id ~ '^[1-9][0-9]*$') OR
           (source_provider = 'kinepolis' AND source_movie_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]*$'))
);
CREATE INDEX public_movie_sources_public_movie_id_idx ON public_movie_sources (public_movie_id);

CREATE TABLE movie_slug_aliases (
    slug varchar(128) PRIMARY KEY CHECK (btrim(slug) <> '' AND slug !~ '^film-[1-9][0-9]*$'),
    public_movie_id bigint NOT NULL REFERENCES public_movies(id),
    alias_kind varchar(16) NOT NULL CHECK (alias_kind IN ('source', 'local', 'tmdb')),
    source_provider varchar(32),
    source_movie_id varchar(128),
    first_seen_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    retargeted_at timestamptz,
    CHECK ((alias_kind = 'source' AND source_provider IN ('ugc', 'kinepolis') AND source_movie_id IS NOT NULL) OR
           (alias_kind <> 'source' AND source_provider IS NULL AND source_movie_id IS NULL)),
    CHECK (source_provider IS NULL OR
           (source_provider = 'ugc' AND source_movie_id ~ '^[1-9][0-9]*$') OR
           (source_provider = 'kinepolis' AND source_movie_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]*$'))
);
CREATE INDEX movie_slug_aliases_public_movie_id_idx ON movie_slug_aliases (public_movie_id);

DO $$
DECLARE
    component record;
    public_id bigint;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM schedule_snapshot WHERE singleton = true) THEN
        RETURN;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM local_movie_group_members member
        JOIN movie_matches match ON match.source_provider = member.source_provider
            AND match.source_movie_id = member.source_movie_id
            AND match.metadata_provider = 'tmdb' AND match.status = 'matched'
    ) THEN
        RAISE EXCEPTION 'local and TMDB movie identities overlap';
    END IF;

    CREATE TEMP TABLE public_movie_backfill_sources ON COMMIT DROP AS
    SELECT movie.provider AS source_provider, movie.provider_id AS source_movie_id, movie.slug AS source_slug,
           movie.title, movie.runtime_minutes, movie.poster_url, movie.source_overview AS overview,
           movie.source_release_date AS release_date, movie.source_genres AS genres,
           member.local_movie_id, match.metadata_movie_id AS tmdb_id,
           CASE
               WHEN member.local_movie_id IS NOT NULL THEN 'local:' || member.local_movie_id::text
               WHEN match.metadata_movie_id IS NOT NULL THEN 'tmdb:' || match.metadata_movie_id::text
               ELSE 'source:' || movie.provider || ':' || movie.provider_id
           END AS component_key
    FROM movies movie
    JOIN schedule_snapshot snapshot ON snapshot.singleton = true AND snapshot.version = movie.generation_id
    LEFT JOIN local_movie_group_members member ON member.source_provider = movie.provider AND member.source_movie_id = movie.provider_id
    LEFT JOIN movie_matches match ON match.source_provider = movie.provider AND match.source_movie_id = movie.provider_id
        AND match.metadata_provider = 'tmdb' AND match.status = 'matched';

    CREATE TEMP TABLE public_movie_backfill_map (
        source_provider varchar(32) NOT NULL,
        source_movie_id varchar(128) NOT NULL,
        public_movie_id bigint NOT NULL,
        PRIMARY KEY (source_provider, source_movie_id)
    ) ON COMMIT DROP;

    FOR component IN
        SELECT source.component_key,
               COALESCE(grouping.primary_source_provider,
                        (array_agg(source.source_provider ORDER BY CASE source.source_provider WHEN 'ugc' THEN 0 ELSE 1 END, source.source_movie_id))[1]) AS anchor_provider,
               COALESCE(grouping.primary_source_movie_id,
                        (array_agg(source.source_movie_id ORDER BY CASE source.source_provider WHEN 'ugc' THEN 0 ELSE 1 END, source.source_movie_id))[1]) AS anchor_source_movie_id,
               max(source.tmdb_id) AS tmdb_id
        FROM public_movie_backfill_sources source
        LEFT JOIN local_movie_groups grouping ON grouping.id = source.local_movie_id
        GROUP BY source.component_key, grouping.primary_source_provider, grouping.primary_source_movie_id
        ORDER BY source.component_key
    LOOP
        INSERT INTO public_movies (
            identity_anchor_provider, identity_anchor_source_movie_id, title, runtime_minutes,
            poster_url, backdrop_url, overview, release_date, genres, confirmed_tmdb_id
        )
        SELECT component.anchor_provider, component.anchor_source_movie_id,
               COALESCE(
                   (SELECT NULLIF(btrim(cache.localized_title), '') FROM movie_metadata_cache cache WHERE cache.provider='tmdb' AND cache.provider_movie_id=component.tmdb_id AND cache.locale='fr-FR'),
                   (SELECT source.title FROM public_movie_backfill_sources source WHERE source.component_key=component.component_key AND source.source_provider=component.anchor_provider AND source.source_movie_id=component.anchor_source_movie_id),
                   (SELECT source.title FROM public_movie_backfill_sources source WHERE source.component_key=component.component_key ORDER BY CASE source.source_provider WHEN 'ugc' THEN 0 ELSE 1 END, source.source_movie_id LIMIT 1)
               ),
               COALESCE(
                   (SELECT cache.runtime_minutes FROM movie_metadata_cache cache WHERE cache.provider='tmdb' AND cache.provider_movie_id=component.tmdb_id AND cache.locale='fr-FR'),
                   (SELECT source.runtime_minutes FROM public_movie_backfill_sources source WHERE source.component_key=component.component_key AND source.source_provider=component.anchor_provider AND source.source_movie_id=component.anchor_source_movie_id),
                   (SELECT source.runtime_minutes FROM public_movie_backfill_sources source WHERE source.component_key=component.component_key ORDER BY CASE source.source_provider WHEN 'ugc' THEN 0 ELSE 1 END, source.source_movie_id LIMIT 1)
               ),
               COALESCE(
                   (SELECT cache.poster_url FROM movie_metadata_cache cache WHERE cache.provider='tmdb' AND cache.provider_movie_id=component.tmdb_id AND cache.locale='fr-FR' AND cache.poster_url IS NOT NULL),
                   (SELECT source.poster_url FROM public_movie_backfill_sources source WHERE source.component_key=component.component_key AND source.source_provider=component.anchor_provider AND source.source_movie_id=component.anchor_source_movie_id AND source.poster_url IS NOT NULL),
                   (SELECT source.poster_url FROM public_movie_backfill_sources source WHERE source.component_key=component.component_key AND source.poster_url IS NOT NULL ORDER BY CASE source.source_provider WHEN 'ugc' THEN 0 ELSE 1 END, source.source_movie_id LIMIT 1)
               ),
               (SELECT cache.backdrop_url FROM movie_metadata_cache cache WHERE cache.provider='tmdb' AND cache.provider_movie_id=component.tmdb_id AND cache.locale='fr-FR'),
               COALESCE(
                   (SELECT cache.overview FROM movie_metadata_cache cache WHERE cache.provider='tmdb' AND cache.provider_movie_id=component.tmdb_id AND cache.locale='fr-FR' AND NULLIF(btrim(cache.overview), '') IS NOT NULL),
                   (SELECT source.overview FROM public_movie_backfill_sources source WHERE source.component_key=component.component_key AND source.source_provider=component.anchor_provider AND source.source_movie_id=component.anchor_source_movie_id AND NULLIF(btrim(source.overview), '') IS NOT NULL),
                   (SELECT source.overview FROM public_movie_backfill_sources source WHERE source.component_key=component.component_key AND NULLIF(btrim(source.overview), '') IS NOT NULL ORDER BY CASE source.source_provider WHEN 'ugc' THEN 0 ELSE 1 END, source.source_movie_id LIMIT 1)
               ),
               COALESCE(
                   (SELECT cache.release_date FROM movie_metadata_cache cache WHERE cache.provider='tmdb' AND cache.provider_movie_id=component.tmdb_id AND cache.locale='fr-FR' AND cache.release_date IS NOT NULL),
                   (SELECT source.release_date FROM public_movie_backfill_sources source WHERE source.component_key=component.component_key AND source.source_provider=component.anchor_provider AND source.source_movie_id=component.anchor_source_movie_id AND source.release_date IS NOT NULL),
                   (SELECT source.release_date FROM public_movie_backfill_sources source WHERE source.component_key=component.component_key AND source.release_date IS NOT NULL ORDER BY CASE source.source_provider WHEN 'ugc' THEN 0 ELSE 1 END, source.source_movie_id LIMIT 1)
               ),
               COALESCE(
                   (SELECT cache.genres FROM movie_metadata_cache cache WHERE cache.provider='tmdb' AND cache.provider_movie_id=component.tmdb_id AND cache.locale='fr-FR' AND cardinality(cache.genres) > 0),
                   (SELECT source.genres FROM public_movie_backfill_sources source WHERE source.component_key=component.component_key AND source.source_provider=component.anchor_provider AND source.source_movie_id=component.anchor_source_movie_id AND cardinality(source.genres) > 0),
                   (SELECT source.genres FROM public_movie_backfill_sources source WHERE source.component_key=component.component_key AND cardinality(source.genres) > 0 ORDER BY CASE source.source_provider WHEN 'ugc' THEN 0 ELSE 1 END, source.source_movie_id LIMIT 1),
                   '{}'::text[]
               ),
               component.tmdb_id
        RETURNING id INTO public_id;

        INSERT INTO public_movie_backfill_map
        SELECT source_provider, source_movie_id, public_id
        FROM public_movie_backfill_sources
        WHERE component_key = component.component_key;
    END LOOP;

    INSERT INTO public_movie_sources (
        source_provider, source_movie_id, public_movie_id, source_slug, title, runtime_minutes,
        poster_url, overview, release_date, genres
    )
    SELECT source.source_provider, source.source_movie_id, mapping.public_movie_id, source.source_slug,
           source.title, source.runtime_minutes, source.poster_url, source.overview, source.release_date, source.genres
    FROM public_movie_backfill_sources source
    JOIN public_movie_backfill_map mapping USING (source_provider, source_movie_id);

    INSERT INTO movie_slug_aliases (slug, public_movie_id, alias_kind, source_provider, source_movie_id)
    SELECT source.source_slug, mapping.public_movie_id, 'source', source.source_provider, source.source_movie_id
    FROM public_movie_backfill_sources source
    JOIN public_movie_backfill_map mapping USING (source_provider, source_movie_id);

    INSERT INTO movie_slug_aliases (slug, public_movie_id, alias_kind)
    SELECT 'local-film-' || grouping.id::text, min(mapping.public_movie_id), 'local'
    FROM local_movie_groups grouping
    JOIN local_movie_group_members member ON member.local_movie_id=grouping.id
    JOIN public_movie_backfill_map mapping ON mapping.source_provider=member.source_provider AND mapping.source_movie_id=member.source_movie_id
    GROUP BY grouping.id
    ON CONFLICT (slug) DO UPDATE SET public_movie_id=EXCLUDED.public_movie_id;

    INSERT INTO movie_slug_aliases (slug, public_movie_id, alias_kind)
    SELECT 'tmdb-film-' || source.tmdb_id::text, min(mapping.public_movie_id), 'tmdb'
    FROM public_movie_backfill_sources source
    JOIN public_movie_backfill_map mapping USING (source_provider, source_movie_id)
    WHERE source.tmdb_id IS NOT NULL
    GROUP BY source.tmdb_id
    ON CONFLICT (slug) DO UPDATE SET public_movie_id=EXCLUDED.public_movie_id;
END $$;
