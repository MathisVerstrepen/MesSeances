ALTER TABLE accounts
    ADD COLUMN avatar_path text,
    ADD COLUMN avatar_source text NOT NULL DEFAULT 'none',
    ADD COLUMN avatar_revision bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT accounts_avatar_check CHECK (
        avatar_revision >= 0 AND (
            (avatar_source IN ('none', 'removed') AND avatar_path IS NULL) OR
            (avatar_source IN ('google', 'upload') AND avatar_path IS NOT NULL AND avatar_revision > 0)
        ) AND (avatar_path IS NULL OR avatar_path ~ '^[a-f0-9]{32}\.png$')
    );
CREATE UNIQUE INDEX accounts_avatar_path_idx ON accounts (avatar_path) WHERE avatar_path IS NOT NULL;

ALTER TABLE account_rate_limits DROP CONSTRAINT account_rate_limits_purpose_check;
ALTER TABLE account_rate_limits ADD CONSTRAINT account_rate_limits_purpose_check CHECK (
    purpose IN ('login', 'verification_send', 'reset_send', 'step_up', 'google_start', 'token_confirm', 'username', 'email_change', 'avatar_write', 'avatar_import')
);
