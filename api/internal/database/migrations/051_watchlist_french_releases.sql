-- Reusable verified metadata, not publication evidence or private membership.
CREATE TABLE tmdb_french_release_cache (
    tmdb_id bigint PRIMARY KEY CHECK (tmdb_id > 0),
    french_release_date date,
    french_releases jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (
        jsonb_typeof(french_releases) = 'array' AND jsonb_array_length(french_releases) <= 64
    ),
    verified_at timestamptz,
    retry_after timestamptz NOT NULL,
    attempt_revision bigint NOT NULL CHECK (attempt_revision BETWEEN 1 AND 9007199254740991),
    CHECK (verified_at IS NOT NULL OR (french_release_date IS NULL AND french_releases = '[]'::jsonb))
);

ALTER TABLE account_rate_limits DROP CONSTRAINT account_rate_limits_purpose_check;
ALTER TABLE account_rate_limits ADD CONSTRAINT account_rate_limits_purpose_check CHECK (
    purpose IN ('login', 'verification_send', 'reset_send', 'step_up', 'google_start', 'token_confirm', 'username', 'email_change', 'avatar_write', 'avatar_import', 'watchlist_write', 'watchlist_search', 'watchlist_import', 'watchlist_release_fetch')
);
