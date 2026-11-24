-- Sync sources: cinemas served by a generic platform connector are rows, not
-- code. The generic TicketingCiné provider is registered here; it has no
-- sources, so it stays inactive until an operator adds and enables rows.

INSERT INTO providers (key) VALUES ('ticketingcine');

CREATE TABLE sync_sources (
    provider varchar(32) NOT NULL REFERENCES providers (key),
    source_key varchar(128) NOT NULL CHECK (source_key ~ '^[A-Za-z0-9_-]+$'),
    enabled boolean NOT NULL DEFAULT false,
    name varchar(1024) NOT NULL DEFAULT '' CHECK (name = btrim(name)),
    website varchar(2048) CHECK (website IS NULL OR (website ~ '^https://[^/?#@[:space:]]+(/[^[:space:]]*)?$' AND website !~ '\\')),
    cnc_id varchar(32) CHECK (cnc_id IS NULL OR cnc_id ~ '^[A-Za-z0-9_-]{1,32}$'),
    params jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(params) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now() CHECK (updated_at >= created_at),
    PRIMARY KEY (provider, source_key)
);

CREATE INDEX sync_sources_enabled_idx ON sync_sources (provider, source_key) WHERE enabled;
