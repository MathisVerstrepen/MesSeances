ALTER TABLE tmdb_upcoming_movies
    ADD COLUMN french_releases jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN reason_codes text[] NOT NULL DEFAULT '{}',
    ADD COLUMN assessed_at timestamptz,
    ADD COLUMN decision text NOT NULL DEFAULT 'unreviewed',
    ADD COLUMN review_revision bigint NOT NULL DEFAULT 1,
    ADD CONSTRAINT tmdb_upcoming_french_releases_check CHECK (
        CASE WHEN jsonb_typeof(french_releases) = 'array' THEN jsonb_array_length(french_releases) <= 64 ELSE false END),
    ADD CONSTRAINT tmdb_upcoming_reason_codes_check CHECK (
        cardinality(reason_codes) <= 4 AND array_position(reason_codes, NULL) IS NULL
        AND reason_codes <@ ARRAY['limited_only','non_theatrical_before_or_same_day','broadcaster_theatrical_note','single_screening_note']::text[]),
    ADD CONSTRAINT tmdb_upcoming_pending_check CHECK (
        assessed_at IS NOT NULL OR (french_releases = '[]'::jsonb AND reason_codes = '{}'::text[])),
    ADD CONSTRAINT tmdb_upcoming_decision_check CHECK (decision IN ('unreviewed','approved','excluded')),
    ADD CONSTRAINT tmdb_upcoming_review_revision_check CHECK (review_revision BETWEEN 1 AND 9007199254740991);
