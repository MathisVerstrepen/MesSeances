-- Account data is independent of schedule generations and administrator auth.
CREATE TABLE accounts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email text NOT NULL UNIQUE CHECK (octet_length(email) BETWEEN 3 AND 254 AND email = lower(email COLLATE "C") AND email = btrim(email) AND email COLLATE "C" !~ '[^!-~]' AND email ~ '^[^@]+@[^@]+\.[^@]+$'),
    email_verified_at timestamptz,
    verification_source text CHECK (verification_source IN ('email', 'google')),
    created_at timestamptz NOT NULL,
    pending_kind text CHECK (pending_kind IN ('email', 'google')),
    auth_revision bigint NOT NULL DEFAULT 1 CHECK (auth_revision > 0),
    CHECK ((email_verified_at IS NULL) = (verification_source IS NULL)),
    CHECK (email_verified_at IS NULL OR email_verified_at >= created_at),
    CHECK (pending_kind IS NOT NULL OR email_verified_at IS NOT NULL)
);
CREATE INDEX accounts_pending_cleanup_idx ON accounts (created_at) WHERE pending_kind IS NOT NULL;

CREATE TABLE account_passwords (
    account_id bigint PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    encoded_hash text NOT NULL CHECK (octet_length(encoded_hash) BETWEEN 90 AND 128 AND encoded_hash LIKE '$argon2id$v=19$%'),
    tentative boolean NOT NULL
);

CREATE TABLE account_google_identities (
    account_id bigint PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    issuer text NOT NULL CHECK (issuer = 'https://accounts.google.com'),
    subject text COLLATE "C" NOT NULL CHECK (octet_length(subject) BETWEEN 1 AND 255),
    observed_email text CHECK (octet_length(observed_email) BETWEEN 3 AND 254),
    observed_email_verified boolean NOT NULL,
    UNIQUE (issuer, subject)
);

-- This table deliberately has only two columns. ON DELETE leaves a username-only
-- permanent reservation, without identity, digest or deletion timestamp.
CREATE TABLE account_username_claims (
    username text COLLATE "C" PRIMARY KEY CHECK (username ~ '^[a-z][a-z0-9_]{2,29}$'),
    account_id bigint UNIQUE REFERENCES accounts(id) ON DELETE SET NULL
);

CREATE TABLE account_sessions (
    token_digest bytea PRIMARY KEY CHECK (octet_length(token_digest) = 32),
    account_id bigint NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    auth_revision bigint NOT NULL CHECK (auth_revision > 0),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    scope text NOT NULL CHECK (scope IN ('pending_email', 'pending_username', 'complete')),
    UNIQUE (token_digest, account_id),
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '720 hours'),
    CHECK (last_seen_at >= created_at AND last_seen_at < expires_at)
);
CREATE INDEX account_sessions_account_idx ON account_sessions (account_id);
CREATE INDEX account_sessions_expiry_idx ON account_sessions (expires_at);
CREATE INDEX account_sessions_idle_idx ON account_sessions (last_seen_at);

CREATE TABLE account_tokens (
    token_digest bytea PRIMARY KEY CHECK (octet_length(token_digest) = 32),
    account_id bigint NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    auth_revision bigint NOT NULL CHECK (auth_revision > 0),
    purpose text NOT NULL CHECK (purpose IN ('verification', 'password_reset', 'email_change', 'email_step_up', 'reauth_grant', 'google_reauth')),
    target_email text CHECK (octet_length(target_email) BETWEEN 3 AND 254 AND target_email = lower(target_email COLLATE "C") AND target_email = btrim(target_email)),
    action text CHECK (action IN ('password_add', 'password_change', 'email_change', 'google_link', 'google_unlink', 'delete_account')),
    action_digest bytea CHECK (octet_length(action_digest) = 32),
    session_digest bytea,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    UNIQUE (token_digest, account_id),
    FOREIGN KEY (session_digest, account_id) REFERENCES account_sessions(token_digest, account_id) ON DELETE CASCADE,
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '24 hours'),
    CHECK (consumed_at IS NULL OR consumed_at >= created_at),
    CHECK ((action IS NULL) = (action_digest IS NULL)),
    CHECK (purpose NOT IN ('email_step_up', 'reauth_grant', 'google_reauth') OR (session_digest IS NOT NULL AND action IS NOT NULL)),
    CHECK (purpose <> 'email_change' OR (target_email IS NOT NULL AND action = 'email_change' AND action IS NOT NULL))
);
CREATE INDEX account_tokens_account_purpose_idx ON account_tokens (account_id, purpose);
CREATE INDEX account_tokens_session_idx ON account_tokens (session_digest) WHERE session_digest IS NOT NULL;
CREATE INDEX account_tokens_expiry_idx ON account_tokens (expires_at);

CREATE TABLE account_oauth_flows (
    state_digest bytea PRIMARY KEY CHECK (octet_length(state_digest) = 32),
    browser_digest bytea NOT NULL CHECK (octet_length(browser_digest) = 32),
    nonce text NOT NULL CHECK (nonce ~ '^[A-Za-z0-9_-]{43}$'),
    verifier_key_id text NOT NULL CHECK (verifier_key_id ~ '^[A-Za-z0-9_-]{1,64}$'),
    verifier_nonce bytea NOT NULL CHECK (octet_length(verifier_nonce) = 12),
    verifier_ciphertext bytea NOT NULL CHECK (octet_length(verifier_ciphertext) BETWEEN 59 AND 144),
    mode text NOT NULL CHECK (mode IN ('login', 'link', 'reauth')),
    account_id bigint REFERENCES accounts(id) ON DELETE CASCADE,
    session_digest bytea,
    auth_revision bigint CHECK (auth_revision > 0),
    grant_digest bytea,
    action text CHECK (action IN ('password_add', 'password_change', 'email_change', 'google_link', 'google_unlink', 'delete_account')),
    action_digest bytea CHECK (octet_length(action_digest) = 32),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    FOREIGN KEY (session_digest, account_id) REFERENCES account_sessions(token_digest, account_id) ON DELETE CASCADE,
    FOREIGN KEY (grant_digest, account_id) REFERENCES account_tokens(token_digest, account_id) ON DELETE CASCADE,
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '10 minutes'),
    CHECK ((action IS NULL) = (action_digest IS NULL)),
    CHECK ((mode = 'login' AND account_id IS NULL AND session_digest IS NULL AND auth_revision IS NULL AND grant_digest IS NULL AND action IS NULL)
        OR (mode IN ('link', 'reauth') AND account_id IS NOT NULL AND session_digest IS NOT NULL AND auth_revision IS NOT NULL AND action IS NOT NULL)),
    CHECK (mode <> 'link' OR (grant_digest IS NOT NULL AND action = 'google_link'))
);
CREATE INDEX account_oauth_flows_account_idx ON account_oauth_flows (account_id) WHERE account_id IS NOT NULL;
CREATE INDEX account_oauth_flows_session_idx ON account_oauth_flows (session_digest) WHERE session_digest IS NOT NULL;
CREATE INDEX account_oauth_flows_grant_idx ON account_oauth_flows (grant_digest) WHERE grant_digest IS NOT NULL;
CREATE INDEX account_oauth_flows_expiry_idx ON account_oauth_flows (expires_at);

CREATE TABLE account_mail_outbox (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_digest bytea NOT NULL UNIQUE CHECK (octet_length(event_digest) = 32),
    account_id bigint REFERENCES accounts(id) ON DELETE CASCADE,
    token_digest bytea,
    auth_revision bigint CHECK (auth_revision > 0),
    purpose text NOT NULL CHECK (purpose IN ('verification', 'password_reset', 'email_change', 'email_step_up', 'security_notification')),
    payload_key_id text CHECK (payload_key_id ~ '^[A-Za-z0-9_-]{1,64}$'),
    payload_nonce bytea CHECK (octet_length(payload_nonce) = 12),
    payload_ciphertext bytea CHECK (octet_length(payload_ciphertext) BETWEEN 17 AND 16400),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'sent', 'failed')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 6),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    next_attempt_at timestamptz NOT NULL,
    lease_until timestamptz,
    lease_digest bytea CHECK (octet_length(lease_digest) = 32),
    finished_at timestamptz,
    FOREIGN KEY (token_digest, account_id) REFERENCES account_tokens(token_digest, account_id) ON DELETE CASCADE,
    CHECK (token_digest IS NULL OR account_id IS NOT NULL),
    CHECK ((account_id IS NULL) = (auth_revision IS NULL)),
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '24 hours'),
    CHECK (next_attempt_at >= created_at),
    CHECK ((lease_until IS NULL) = (lease_digest IS NULL)),
    CHECK ((state = 'pending' AND payload_key_id IS NOT NULL AND payload_nonce IS NOT NULL AND payload_ciphertext IS NOT NULL AND finished_at IS NULL)
        OR (state IN ('sent', 'failed') AND payload_key_id IS NULL AND payload_nonce IS NULL AND payload_ciphertext IS NULL AND lease_until IS NULL AND finished_at IS NOT NULL))
);
CREATE INDEX account_mail_outbox_ready_idx ON account_mail_outbox (next_attempt_at, id) WHERE state = 'pending';
CREATE INDEX account_mail_outbox_account_idx ON account_mail_outbox (account_id) WHERE account_id IS NOT NULL;
CREATE INDEX account_mail_outbox_token_idx ON account_mail_outbox (token_digest) WHERE token_digest IS NOT NULL;
CREATE INDEX account_mail_outbox_expiry_idx ON account_mail_outbox (expires_at);
CREATE INDEX account_mail_outbox_retention_idx ON account_mail_outbox (finished_at) WHERE finished_at IS NOT NULL;

CREATE TABLE account_mail_suppressions (
    address_digest bytea PRIMARY KEY CHECK (octet_length(address_digest) = 32),
    reason text NOT NULL CHECK (reason IN ('permanent_bounce', 'complaint')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    CHECK (updated_at >= created_at AND expires_at > updated_at AND expires_at <= updated_at + interval '4320 hours')
);
CREATE INDEX account_mail_suppressions_expiry_idx ON account_mail_suppressions (expires_at);

CREATE TABLE account_rate_limits (
    purpose text NOT NULL CHECK (purpose IN ('login', 'verification_send', 'reset_send', 'step_up', 'google_start', 'token_confirm', 'username', 'email_change')),
    key_digest bytea NOT NULL CHECK (octet_length(key_digest) = 32),
    window_start timestamptz NOT NULL,
    window_seconds integer NOT NULL CHECK (window_seconds IN (60, 900, 3600, 86400)),
    count integer NOT NULL CHECK (count > 0),
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (purpose, key_digest, window_start, window_seconds),
    CHECK (expires_at > window_start AND expires_at <= window_start + interval '48 hours')
);
CREATE INDEX account_rate_limits_expiry_idx ON account_rate_limits (expires_at);
