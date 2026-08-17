CREATE TABLE local_movie_groups (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY CHECK (id > 0),
    primary_source_provider varchar(32) NOT NULL CHECK (primary_source_provider IN ('ugc', 'kinepolis')),
    primary_source_movie_id varchar(128) NOT NULL,
    CHECK ((primary_source_provider = 'ugc' AND primary_source_movie_id ~ '^[1-9][0-9]*$') OR
           (primary_source_provider = 'kinepolis' AND primary_source_movie_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]*$'))
);

CREATE TABLE local_movie_group_members (
    local_movie_id bigint NOT NULL REFERENCES local_movie_groups(id) ON DELETE CASCADE,
    source_provider varchar(32) NOT NULL CHECK (source_provider IN ('ugc', 'kinepolis')),
    source_movie_id varchar(128) NOT NULL,
    PRIMARY KEY (local_movie_id, source_provider, source_movie_id),
    UNIQUE (source_provider, source_movie_id),
    CHECK ((source_provider = 'ugc' AND source_movie_id ~ '^[1-9][0-9]*$') OR
           (source_provider = 'kinepolis' AND source_movie_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]*$'))
);

ALTER TABLE local_movie_groups ADD CONSTRAINT local_movie_groups_primary_member_fkey
    FOREIGN KEY (id, primary_source_provider, primary_source_movie_id)
    REFERENCES local_movie_group_members(local_movie_id, source_provider, source_movie_id)
    DEFERRABLE INITIALLY DEFERRED;
