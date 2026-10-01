CREATE TABLE cinema_activity_state (
    theater_id varchar(128) PRIMARY KEY REFERENCES screening_history_theaters(id),
    history_started_at timestamptz NOT NULL,
    last_publication_at timestamptz NOT NULL CHECK (last_publication_at >= history_started_at),
    source_generated_at timestamptz NOT NULL
);

CREATE TABLE cinema_activity_coverage (
    provider varchar(32) NOT NULL CHECK (provider IN ('ugc','kinepolis','pathe','cgr','megarama','cineville','mk2','cinewest','grandecran','noecinemas')),
    generation bigint NOT NULL CHECK (generation > 0),
    theater_id varchar(128) NOT NULL,
    service_date date NOT NULL,
    status text NOT NULL CHECK (status IN ('complete','unknown')),
    basis text NOT NULL CHECK (basis IN ('date_response','accepted_omission','unproven')),
    source_generated_at timestamptz NOT NULL,
    detected_at timestamptz NOT NULL,
    PRIMARY KEY (provider,generation,theater_id,service_date),
    FOREIGN KEY (provider,theater_id) REFERENCES screening_history_theaters(provider,id),
    CHECK ((status='complete' AND basis='date_response' AND provider IN ('ugc','pathe','cgr','grandecran','noecinemas')) OR (status='unknown' AND basis IN ('accepted_omission','unproven')))
);
CREATE INDEX cinema_activity_coverage_date_idx ON cinema_activity_coverage (theater_id,service_date,source_generated_at DESC,detected_at DESC,generation DESC);

CREATE TABLE cinema_activity_episodes (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY CHECK (id > 0),
    theater_id varchar(128) NOT NULL REFERENCES screening_history_theaters(id),
    anchor_provider varchar(32) NOT NULL,
    anchor_source_movie_id varchar(128) NOT NULL,
    kind text NOT NULL CHECK (kind IN ('baseline','added_to_program','return_to_program')),
    detected_at timestamptz NOT NULL,
    first_screening_date date NOT NULL,
    previous_program_end_date date,
    observed_from date NOT NULL,
    observed_through date NOT NULL CHECK (observed_through >= observed_from),
    break_from date,
    break_through date,
    detecting_generation bigint NOT NULL CHECK (detecting_generation > 0),
    superseded_by_id bigint,
    UNIQUE (theater_id,id),
    FOREIGN KEY (theater_id,superseded_by_id) REFERENCES cinema_activity_episodes(theater_id,id),
    FOREIGN KEY (anchor_provider,anchor_source_movie_id) REFERENCES public_movie_sources(source_provider,source_movie_id),
    CHECK (superseded_by_id IS NULL OR superseded_by_id < id),
    CHECK ((kind='return_to_program' AND previous_program_end_date IS NOT NULL AND break_from IS NOT NULL AND break_through IS NOT NULL AND break_from=previous_program_end_date+1 AND break_through=first_screening_date-1 AND break_through-break_from+1 >= 28) OR (kind<>'return_to_program' AND previous_program_end_date IS NULL AND break_from IS NULL AND break_through IS NULL))
);
CREATE INDEX cinema_activity_events_idx ON cinema_activity_episodes (theater_id,detected_at DESC,id DESC) WHERE superseded_by_id IS NULL AND kind<>'baseline';
CREATE INDEX cinema_activity_event_upper_idx ON cinema_activity_episodes (theater_id,id DESC) WHERE superseded_by_id IS NULL AND kind<>'baseline';

CREATE TABLE cinema_activity_episode_sources (
    theater_id varchar(128) NOT NULL,
    episode_id bigint NOT NULL,
    source_provider varchar(32) NOT NULL,
    source_movie_id varchar(128) NOT NULL,
    observed_from date NOT NULL,
    observed_through date NOT NULL CHECK (observed_through >= observed_from),
    first_seen_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL CHECK (last_seen_at >= first_seen_at),
    PRIMARY KEY (theater_id,episode_id,source_provider,source_movie_id),
    FOREIGN KEY (theater_id,episode_id) REFERENCES cinema_activity_episodes(theater_id,id),
    FOREIGN KEY (source_provider,source_movie_id) REFERENCES public_movie_sources(source_provider,source_movie_id)
);
CREATE INDEX cinema_activity_source_idx ON cinema_activity_episode_sources (theater_id,source_provider,source_movie_id,episode_id DESC);
