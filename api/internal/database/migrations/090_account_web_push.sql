-- Optional browser push delivery for inbox notifications. Subscriptions are
-- capability URLs and keys: they are stored only as an HMAC lookup digest and an
-- AEAD envelope. Outbox rows reference inbox entries, never rendered payloads.
ALTER TABLE account_notification_rules ADD COLUMN push boolean NOT NULL DEFAULT false;

CREATE TABLE account_push_subscriptions (
    account_id bigint NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    id bigint GENERATED ALWAYS AS IDENTITY UNIQUE CHECK (id > 0),
    auth_revision bigint NOT NULL CHECK (auth_revision > 0),
    endpoint_digest bytea NOT NULL UNIQUE CHECK (octet_length(endpoint_digest) = 32),
    payload_key_id text NOT NULL CHECK (payload_key_id ~ '^[A-Za-z0-9_-]{1,64}$'),
    payload_nonce bytea NOT NULL CHECK (octet_length(payload_nonce) = 12),
    payload_ciphertext bytea NOT NULL CHECK (octet_length(payload_ciphertext) BETWEEN 17 AND 4096),
    label text NOT NULL CHECK (char_length(label) BETWEEN 1 AND 64),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL CHECK (updated_at >= created_at),
    last_success_at timestamptz,
    failure_count integer NOT NULL DEFAULT 0 CHECK (failure_count BETWEEN 0 AND 100),
    PRIMARY KEY (account_id, id)
);

-- No subscription FK: deleting a device never waits on concurrent enqueueing.
-- Delivery drops rows whose subscription or inbox entry has disappeared.
CREATE TABLE account_push_outbox (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id bigint NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    subscription_id bigint NOT NULL CHECK (subscription_id > 0),
    notification_id bigint NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at AND expires_at <= created_at + interval '24 hours'),
    next_attempt_at timestamptz NOT NULL CHECK (next_attempt_at >= created_at),
    lease_until timestamptz,
    UNIQUE (subscription_id, notification_id),
    FOREIGN KEY (account_id, notification_id) REFERENCES account_notifications(account_id, id) ON DELETE CASCADE
);
CREATE INDEX account_push_outbox_ready_idx ON account_push_outbox (next_attempt_at, id);
CREATE INDEX account_push_outbox_notification_idx ON account_push_outbox (account_id, notification_id);
CREATE INDEX account_push_outbox_expiry_idx ON account_push_outbox (expires_at);
