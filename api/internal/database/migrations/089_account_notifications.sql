-- Private notification rules and in-app inbox derived from the public cinema
-- activity journal. No delivery channel other than the inbox is created here.
CREATE TABLE account_notification_rules (
    account_id bigint NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    id bigint GENERATED ALWAYS AS IDENTITY UNIQUE CHECK (id > 0),
    kind text NOT NULL CHECK (kind IN ('cinema_activity', 'watchlist_screening')),
    theater_scope text NOT NULL CHECK (theater_scope IN ('followed', 'preferred', 'custom')),
    theater_ids text[] NOT NULL DEFAULT '{}'::text[] CHECK (account_theater_ids_valid(theater_ids) AND cardinality(theater_ids) <= 64),
    include_returns boolean NOT NULL DEFAULT false,
    enabled boolean NOT NULL DEFAULT true,
    armed_after_event_id bigint NOT NULL CHECK (armed_after_event_id >= 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL CHECK (updated_at >= created_at),
    PRIMARY KEY (account_id, id),
    CHECK ((theater_scope = 'custom') = (cardinality(theater_ids) > 0)),
    CHECK (kind = 'cinema_activity' OR NOT include_returns)
);
CREATE INDEX account_notification_rules_enabled_idx ON account_notification_rules (account_id) WHERE enabled;

CREATE TABLE account_notifications (
    account_id bigint NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    id bigint GENERATED ALWAYS AS IDENTITY UNIQUE CHECK (id > 0),
    rule_id bigint,
    kind text NOT NULL CHECK (kind IN ('cinema_activity', 'watchlist_screening')),
    public_movie_id bigint REFERENCES public_movies(id),
    movie_ids bigint[] NOT NULL CHECK (cardinality(movie_ids) BETWEEN 1 AND 1000 AND array_ndims(movie_ids) = 1 AND array_position(movie_ids, NULL) IS NULL),
    theater_ids text[] NOT NULL CHECK (account_theater_ids_valid(theater_ids) AND cardinality(theater_ids) > 0),
    event_count integer NOT NULL CHECK (event_count >= cardinality(movie_ids)),
    last_event_id bigint NOT NULL CHECK (last_event_id > 0),
    created_at timestamptz NOT NULL,
    read_at timestamptz CHECK (read_at IS NULL OR read_at >= created_at),
    PRIMARY KEY (account_id, id),
    FOREIGN KEY (account_id, rule_id) REFERENCES account_notification_rules(account_id, id) ON DELETE SET NULL (rule_id),
    CHECK ((kind = 'watchlist_screening') = (public_movie_id IS NOT NULL)),
    CHECK (kind = 'cinema_activity' OR movie_ids = ARRAY[public_movie_id])
);
CREATE INDEX account_notifications_feed_idx ON account_notifications (account_id, id DESC);
CREATE INDEX account_notifications_unread_idx ON account_notifications (account_id, rule_id) WHERE read_at IS NULL;
CREATE INDEX account_notifications_retention_idx ON account_notifications (created_at);
CREATE UNIQUE INDEX account_notifications_watchlist_once_idx ON account_notifications (account_id, rule_id, public_movie_id) WHERE kind = 'watchlist_screening';

-- One global, monotonic journal position. Seeding at the current maximum makes
-- deployment silent: no historical activity is turned into notifications.
CREATE TABLE account_notification_cursor (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    last_event_id bigint NOT NULL CHECK (last_event_id >= 0),
    updated_at timestamptz NOT NULL
);
INSERT INTO account_notification_cursor (singleton, last_event_id, updated_at)
SELECT true, coalesce(max(id), 0), now() FROM cinema_activity_episodes;
