CREATE TABLE account_watchlist_state (
    account_id bigint PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    revision bigint NOT NULL CHECK (revision BETWEEN 1 AND 9007199254740991)
);

CREATE TABLE account_watchlist_items (
    account_id bigint NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    public_movie_id bigint NOT NULL REFERENCES public_movies(id),
    added_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, public_movie_id)
);
CREATE INDEX account_watchlist_items_movie_idx ON account_watchlist_items(public_movie_id);

-- Public publication evidence has no importing-account attribution or lifetime.
CREATE TABLE tmdb_catalog_imports (
    tmdb_id bigint PRIMARY KEY CHECK (tmdb_id > 0),
    public_movie_id bigint NOT NULL REFERENCES public_movies(id),
    published_at timestamptz NOT NULL
);
CREATE INDEX tmdb_catalog_imports_public_movie_id_idx ON tmdb_catalog_imports(public_movie_id);

ALTER TABLE account_rate_limits DROP CONSTRAINT account_rate_limits_purpose_check;
ALTER TABLE account_rate_limits ADD CONSTRAINT account_rate_limits_purpose_check CHECK (
    purpose IN ('login', 'verification_send', 'reset_send', 'step_up', 'google_start', 'token_confirm', 'username', 'email_change', 'avatar_write', 'avatar_import', 'watchlist_write', 'watchlist_search', 'watchlist_import')
);
