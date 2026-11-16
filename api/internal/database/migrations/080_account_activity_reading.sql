CREATE TABLE account_activity_preferences (
    account_id bigint PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    manual_reading boolean NOT NULL DEFAULT false
);

CREATE TABLE account_activity_reads (
    account_id bigint NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    event_id bigint NOT NULL REFERENCES cinema_activity_episodes(id) ON DELETE CASCADE,
    consulted boolean NOT NULL,
    PRIMARY KEY (account_id, event_id)
);

CREATE INDEX account_activity_reads_event_idx ON account_activity_reads(event_id);
