ALTER TABLE schedule_snapshot DROP CONSTRAINT schedule_snapshot_provider_check;
ALTER TABLE schedule_snapshot ADD CONSTRAINT schedule_snapshot_provider_check CHECK (provider IN ('ugc', 'kinepolis', 'pathe', 'cgr', 'combined'));

ALTER TABLE provider_snapshots DROP CONSTRAINT provider_snapshots_provider_check;
ALTER TABLE provider_snapshots ADD CONSTRAINT provider_snapshots_provider_check CHECK (provider IN ('ugc', 'kinepolis', 'pathe', 'cgr'));

ALTER TABLE theaters DROP CONSTRAINT theaters_provider_check;
ALTER TABLE theaters ADD CONSTRAINT theaters_provider_check CHECK (provider IN ('ugc', 'kinepolis', 'pathe', 'cgr'));
ALTER TABLE theaters DROP CONSTRAINT theaters_provider_identity_check;
ALTER TABLE theaters ADD CONSTRAINT theaters_provider_identity_check CHECK (
    (provider = 'ugc' AND provider_id ~ '^[1-9][0-9]*$') OR
    (provider IN ('kinepolis', 'pathe') AND provider_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$') OR
    (provider = 'cgr' AND provider_id ~ '^[A-Z][0-9]{4}$')
);

ALTER TABLE movies DROP CONSTRAINT movies_provider_check;
ALTER TABLE movies ADD CONSTRAINT movies_provider_check CHECK (provider IN ('ugc', 'kinepolis', 'pathe', 'cgr'));
ALTER TABLE movies DROP CONSTRAINT movies_provider_identity_check;
ALTER TABLE movies ADD CONSTRAINT movies_provider_identity_check CHECK (
    (provider = 'ugc' AND provider_id ~ '^[1-9][0-9]*$') OR
    (provider IN ('kinepolis', 'pathe') AND provider_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$') OR
    (provider = 'cgr' AND provider_id ~ '^[1-9][0-9]{0,127}$')
);
ALTER TABLE movies DROP CONSTRAINT movies_runtime_minutes_positive_check;
ALTER TABLE movies ADD CONSTRAINT movies_runtime_minutes_positive_check CHECK (runtime_minutes > 0 OR provider = 'cgr' AND runtime_minutes = 0);

ALTER TABLE showtimes DROP CONSTRAINT showtimes_provider_check;
ALTER TABLE showtimes ADD CONSTRAINT showtimes_provider_check CHECK (provider IN ('ugc', 'kinepolis', 'pathe', 'cgr'));

ALTER TABLE showtimes DROP CONSTRAINT showtimes_check1;
ALTER TABLE showtimes ADD CONSTRAINT showtimes_time_check CHECK (
    end_time > start_time OR (provider = 'cgr' AND end_time = start_time)
);
ALTER TABLE showtimes DROP CONSTRAINT showtimes_provider_identity_check;
ALTER TABLE showtimes ADD CONSTRAINT showtimes_provider_identity_check CHECK (
    (provider = 'ugc' AND provider_showing_id ~ '^[1-9][0-9]*$') OR
    (provider = 'kinepolis' AND provider_showing_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$') OR
    (provider = 'pathe' AND provider_showing_id ~ '^V[1-9][0-9]*S[1-9][0-9]*$') OR
    (provider = 'cgr' AND provider_showing_id ~ '^[A-Z][0-9]{4}-[a-f0-9]{64}$')
);
ALTER TABLE showtimes DROP CONSTRAINT showtimes_language_check;
ALTER TABLE showtimes ADD CONSTRAINT showtimes_language_check CHECK (language <> 'ALL' AND language ~ '^[A-Z][A-Z0-9_]{0,15}$');

ALTER TABLE movie_matches DROP CONSTRAINT movie_matches_source_provider_check;
ALTER TABLE movie_matches ADD CONSTRAINT movie_matches_source_provider_check CHECK (source_provider IN ('ugc', 'kinepolis', 'pathe', 'cgr'));
ALTER TABLE movie_matches DROP CONSTRAINT movie_matches_source_movie_id_check;
ALTER TABLE movie_matches ADD CONSTRAINT movie_matches_source_movie_id_check CHECK (
    (source_provider = 'ugc' AND source_movie_id ~ '^[1-9][0-9]*$') OR
    (source_provider IN ('kinepolis', 'pathe') AND source_movie_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$') OR
    (source_provider = 'cgr' AND source_movie_id ~ '^[1-9][0-9]{0,127}$')
);

ALTER TABLE local_movie_groups DROP CONSTRAINT local_movie_groups_primary_source_provider_check;
ALTER TABLE local_movie_groups ADD CONSTRAINT local_movie_groups_primary_source_provider_check CHECK (primary_source_provider IN ('ugc', 'kinepolis', 'pathe', 'cgr'));
ALTER TABLE local_movie_groups DROP CONSTRAINT local_movie_groups_check;
ALTER TABLE local_movie_groups ADD CONSTRAINT local_movie_groups_check CHECK (
    (primary_source_provider = 'ugc' AND primary_source_movie_id ~ '^[1-9][0-9]*$') OR
    (primary_source_provider IN ('kinepolis', 'pathe') AND primary_source_movie_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$') OR
    (primary_source_provider = 'cgr' AND primary_source_movie_id ~ '^[1-9][0-9]{0,127}$')
);

ALTER TABLE local_movie_group_members DROP CONSTRAINT local_movie_group_members_source_provider_check;
ALTER TABLE local_movie_group_members ADD CONSTRAINT local_movie_group_members_source_provider_check CHECK (source_provider IN ('ugc', 'kinepolis', 'pathe', 'cgr'));
ALTER TABLE local_movie_group_members DROP CONSTRAINT local_movie_group_members_check;
ALTER TABLE local_movie_group_members ADD CONSTRAINT local_movie_group_members_check CHECK (
    (source_provider = 'ugc' AND source_movie_id ~ '^[1-9][0-9]*$') OR
    (source_provider IN ('kinepolis', 'pathe') AND source_movie_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$') OR
    (source_provider = 'cgr' AND source_movie_id ~ '^[1-9][0-9]{0,127}$')
);

ALTER TABLE sync_runs DROP CONSTRAINT sync_runs_target_check;
ALTER TABLE sync_runs ADD CONSTRAINT sync_runs_target_check CHECK (target IN ('all', 'ugc', 'kinepolis', 'pathe', 'cgr'));
ALTER TABLE sync_runs DROP CONSTRAINT sync_runs_trigger_occurrence_check;
ALTER TABLE sync_runs ADD CONSTRAINT sync_runs_trigger_occurrence_check CHECK (
    (trigger_source = 'manual' AND schedule_revision IS NULL AND scheduled_for IS NULL AND schedule_attempt IS NULL) OR
    (trigger_source = 'scheduled' AND target IN ('ugc', 'kinepolis', 'pathe', 'cgr') AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2)
);

ALTER TABLE sync_schedules DROP CONSTRAINT sync_schedules_provider_check;
ALTER TABLE sync_schedules ADD CONSTRAINT sync_schedules_provider_check CHECK (provider IN ('ugc', 'kinepolis', 'pathe', 'cgr'));

ALTER TABLE public_movies DROP CONSTRAINT public_movies_identity_anchor_provider_check;
ALTER TABLE public_movies ADD CONSTRAINT public_movies_identity_anchor_provider_check CHECK (identity_anchor_provider IN ('ugc', 'kinepolis', 'pathe', 'cgr'));
ALTER TABLE public_movies DROP CONSTRAINT public_movies_check1;
ALTER TABLE public_movies ADD CONSTRAINT public_movies_check1 CHECK (
    (identity_anchor_provider = 'ugc' AND identity_anchor_source_movie_id ~ '^[1-9][0-9]*$') OR
    (identity_anchor_provider IN ('kinepolis', 'pathe') AND identity_anchor_source_movie_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$') OR
    (identity_anchor_provider = 'cgr' AND identity_anchor_source_movie_id ~ '^[1-9][0-9]{0,127}$')
);
ALTER TABLE public_movies DROP CONSTRAINT public_movies_runtime_minutes_check;
ALTER TABLE public_movies ADD CONSTRAINT public_movies_runtime_minutes_check CHECK (runtime_minutes > 0 OR identity_anchor_provider = 'cgr' AND runtime_minutes = 0);

ALTER TABLE public_movie_sources DROP CONSTRAINT public_movie_sources_source_provider_check;
ALTER TABLE public_movie_sources ADD CONSTRAINT public_movie_sources_source_provider_check CHECK (source_provider IN ('ugc', 'kinepolis', 'pathe', 'cgr'));
ALTER TABLE public_movie_sources DROP CONSTRAINT public_movie_sources_check;
ALTER TABLE public_movie_sources ADD CONSTRAINT public_movie_sources_check CHECK (
    (source_provider = 'ugc' AND source_movie_id ~ '^[1-9][0-9]*$') OR
    (source_provider IN ('kinepolis', 'pathe') AND source_movie_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$') OR
    (source_provider = 'cgr' AND source_movie_id ~ '^[1-9][0-9]{0,127}$')
);
ALTER TABLE public_movie_sources DROP CONSTRAINT public_movie_sources_runtime_minutes_check;
ALTER TABLE public_movie_sources ADD CONSTRAINT public_movie_sources_runtime_minutes_check CHECK (runtime_minutes > 0 OR source_provider = 'cgr' AND runtime_minutes = 0);

ALTER TABLE movie_slug_aliases DROP CONSTRAINT movie_slug_aliases_check;
ALTER TABLE movie_slug_aliases ADD CONSTRAINT movie_slug_aliases_check CHECK (
    (alias_kind = 'source' AND source_provider IN ('ugc', 'kinepolis', 'pathe', 'cgr') AND source_movie_id IS NOT NULL) OR
    (alias_kind <> 'source' AND source_provider IS NULL AND source_movie_id IS NULL)
);
ALTER TABLE movie_slug_aliases DROP CONSTRAINT movie_slug_aliases_check1;
ALTER TABLE movie_slug_aliases ADD CONSTRAINT movie_slug_aliases_check1 CHECK (
    source_provider IS NULL OR
    (source_provider = 'ugc' AND source_movie_id ~ '^[1-9][0-9]*$') OR
    (source_provider IN ('kinepolis', 'pathe') AND source_movie_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$') OR
    (source_provider = 'cgr' AND source_movie_id ~ '^[1-9][0-9]{0,127}$')
);
