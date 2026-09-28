ALTER TABLE account_watchlist_state
    ADD COLUMN sort_order text NOT NULL DEFAULT 'added_desc'
    CHECK (sort_order IN ('added_desc', 'added_asc', 'title_asc', 'title_desc', 'release_desc', 'release_asc'));
