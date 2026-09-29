CREATE TABLE account_watchlist_tags (
    account_id bigint NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    id bigint GENERATED ALWAYS AS IDENTITY CHECK (id > 0),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 40 AND btrim(name) <> ''),
    name_key text COLLATE "C" NOT NULL CHECK (char_length(name_key) BETWEEN 1 AND 120 AND btrim(name_key) <> ''),
    PRIMARY KEY (account_id, id),
    UNIQUE (account_id, name_key)
);

CREATE TABLE account_watchlist_item_tags (
    account_id bigint NOT NULL,
    public_movie_id bigint NOT NULL,
    tag_id bigint NOT NULL,
    PRIMARY KEY (account_id, public_movie_id, tag_id),
    FOREIGN KEY (account_id, public_movie_id) REFERENCES account_watchlist_items(account_id, public_movie_id) ON DELETE CASCADE,
    FOREIGN KEY (account_id, tag_id) REFERENCES account_watchlist_tags(account_id, id) ON DELETE CASCADE
);

CREATE INDEX account_watchlist_item_tags_tag_idx ON account_watchlist_item_tags(account_id, tag_id);
