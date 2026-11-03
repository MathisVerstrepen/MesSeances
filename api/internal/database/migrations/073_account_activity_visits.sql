CREATE TABLE account_activity_visits (
    account_id bigint NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    scope varchar(136) NOT NULL CHECK (scope = 'followed' OR scope ~ '^theater:[A-Za-z0-9][A-Za-z0-9_-]{0,127}$'),
    last_event_id bigint NOT NULL CHECK (last_event_id >= 0),
    PRIMARY KEY (account_id, scope)
);
