ALTER TABLE account_watchlist_state
    ADD COLUMN view_mode text NOT NULL DEFAULT 'list' CHECK (view_mode IN ('list', 'tags')),
    ADD COLUMN filter_tag_id bigint,
    ADD CONSTRAINT account_watchlist_state_filter_tag_fkey
        FOREIGN KEY (account_id, filter_tag_id)
        REFERENCES account_watchlist_tags(account_id, id)
        ON DELETE SET NULL (filter_tag_id);
