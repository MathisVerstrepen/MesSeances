CREATE INDEX short_links_retention_idx ON short_links (created_at);

DELETE FROM short_links
WHERE created_at < CURRENT_TIMESTAMP - INTERVAL '90 days';
