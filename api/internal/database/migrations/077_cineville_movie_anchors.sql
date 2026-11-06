-- Cineville publishes film sections inside cinema programme pages. Keep all
-- other providers' fragment restriction and bind Cineville anchors to the visa.
ALTER TABLE movies DROP CONSTRAINT movies_source_page_url_check;
ALTER TABLE movies ADD CONSTRAINT movies_source_page_url_check CHECK (
    source_page_url IS NULL OR (
        source_page_url ~ '^https://[^/?#@[:space:]]+/[^?#[:space:]]+$'
        AND source_page_url !~ '[%\\]'
    ) OR (
        provider = 'cineville'
        AND source_page_url ~ '^https://www[.]cineville[.]fr/programmes/[a-z0-9_-]+#-?[1-9][0-9]*$'
        AND split_part(source_page_url, '#', 2) = provider_id
    )
);
