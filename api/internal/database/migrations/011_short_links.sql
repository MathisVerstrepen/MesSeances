CREATE TABLE short_links (
    code text PRIMARY KEY,
    target text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT short_links_code_check CHECK (code ~ '^[A-Za-z0-9_-]{22}$'),
    CONSTRAINT short_links_target_length_check CHECK (target <> '' AND octet_length(target) <= 2048),
    CONSTRAINT short_links_target_relative_check CHECK (target LIKE '/%' AND target NOT LIKE '//%'),
    CONSTRAINT short_links_target_controls_check CHECK (strpos(target, E'\r') = 0 AND strpos(target, E'\n') = 0)
);
