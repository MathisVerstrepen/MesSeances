CREATE FUNCTION account_theater_ids_valid(ids text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT cardinality(ids) <= 4096
        AND (cardinality(ids) = 0 OR (array_ndims(ids) = 1 AND array_lower(ids, 1) = 1))
        AND NOT EXISTS (
            SELECT 1 FROM unnest(ids) AS item(id)
            WHERE id IS NULL OR id COLLATE "C" !~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$'
        )
        AND cardinality(ids) = (SELECT count(DISTINCT id COLLATE "C") FROM unnest(ids) AS item(id));
$$;

CREATE TABLE account_theater_preferences (
    account_id bigint PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    revision bigint NOT NULL CHECK (revision BETWEEN 1 AND 9007199254740991),
    theater_ids text[] NOT NULL CHECK (account_theater_ids_valid(theater_ids))
);
