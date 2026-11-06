-- Captured source routes belong to a provider movie in a schedule generation.
-- Existing generations stay linkless until their next successful provider sync.
ALTER TABLE movies ADD COLUMN source_page_url varchar(4096);
ALTER TABLE movies ADD CONSTRAINT movies_source_page_url_check CHECK (
    source_page_url IS NULL OR (
        source_page_url ~ '^https://[^/?#@[:space:]]+/[^?#[:space:]]+$'
        AND source_page_url !~ '[%\\]'
    )
);
