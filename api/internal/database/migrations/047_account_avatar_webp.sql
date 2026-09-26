ALTER TABLE accounts DROP CONSTRAINT accounts_avatar_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_avatar_check CHECK (
    avatar_revision >= 0
    AND ((avatar_source IN ('none', 'removed') AND avatar_path IS NULL)
      OR (avatar_source IN ('google', 'upload') AND avatar_path IS NOT NULL AND avatar_revision > 0))
);

-- Existing PNG rows remain readable by the offline converter, but every new
-- INSERT/UPDATE must obey WebP-only storage. Live account writers must be stopped.
ALTER TABLE accounts ADD CONSTRAINT accounts_avatar_webp_check
    CHECK (avatar_path IS NULL OR avatar_path ~ '^[a-f0-9]{32}\.webp$') NOT VALID;

-- A fresh/empty installation needs no media conversion. Otherwise validation is
-- the converter's final durable completion marker, not merely this ledger entry.
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM accounts WHERE avatar_path IS NOT NULL
                   AND avatar_path !~ '^[a-f0-9]{32}\.webp$') THEN
        ALTER TABLE accounts VALIDATE CONSTRAINT accounts_avatar_webp_check;
    END IF;
END $$;
