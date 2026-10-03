CREATE TABLE account_theater_follows (
    account_id bigint PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    revision bigint NOT NULL CHECK (revision BETWEEN 1 AND 9007199254740991),
    theater_ids text[] NOT NULL CHECK (account_theater_ids_valid(theater_ids))
);
