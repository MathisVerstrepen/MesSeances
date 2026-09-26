-- A registration attempt is separate from tentative-password login authority.
-- Replacing an attempt changes both its credential and browser proof atomically.
ALTER TABLE account_passwords ADD COLUMN registration_digest bytea
    CHECK (octet_length(registration_digest) = 32);
ALTER TABLE account_passwords ADD CONSTRAINT account_passwords_registration_tentative
    CHECK (registration_digest IS NULL OR tentative);
