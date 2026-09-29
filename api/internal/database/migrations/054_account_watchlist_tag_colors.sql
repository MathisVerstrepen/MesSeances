ALTER TABLE account_watchlist_tags
    ADD COLUMN color text NOT NULL DEFAULT 'neutral',
    ADD CONSTRAINT account_watchlist_tags_color_check
        CHECK (color IN ('neutral', 'red', 'amber', 'green', 'teal', 'blue', 'violet', 'rose'));
