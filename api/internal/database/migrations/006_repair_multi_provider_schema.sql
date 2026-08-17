ALTER TABLE movies ADD COLUMN IF NOT EXISTS source_overview varchar(10000);
ALTER TABLE movies ADD COLUMN IF NOT EXISTS source_release_date date;
ALTER TABLE movies ADD COLUMN IF NOT EXISTS source_genres text[];

ALTER TABLE movies ALTER COLUMN source_overview TYPE varchar(10000) USING source_overview::varchar(10000);
ALTER TABLE movies ALTER COLUMN source_overview DROP DEFAULT;
ALTER TABLE movies ALTER COLUMN source_overview DROP NOT NULL;
ALTER TABLE movies ALTER COLUMN source_release_date TYPE date USING source_release_date::date;
ALTER TABLE movies ALTER COLUMN source_release_date DROP DEFAULT;
ALTER TABLE movies ALTER COLUMN source_release_date DROP NOT NULL;
ALTER TABLE movies ALTER COLUMN source_genres TYPE text[] USING source_genres::text[];
UPDATE movies SET source_genres = '{}'::text[] WHERE source_genres IS NULL;
ALTER TABLE movies ALTER COLUMN source_genres SET DEFAULT '{}'::text[];
ALTER TABLE movies ALTER COLUMN source_genres SET NOT NULL;

ALTER TABLE movies DROP CONSTRAINT IF EXISTS movies_source_overview_check;
ALTER TABLE movies ADD CONSTRAINT movies_source_overview_check CHECK (source_overview IS NULL OR length(source_overview) <= 10000);
ALTER TABLE movies DROP CONSTRAINT IF EXISTS movies_source_genres_check;
ALTER TABLE movies ADD CONSTRAINT movies_source_genres_check CHECK (cardinality(source_genres) <= 32);

ALTER TABLE theaters DROP CONSTRAINT IF EXISTS theaters_address_check;
ALTER TABLE theaters ADD CONSTRAINT theaters_address_check CHECK (provider = 'kinepolis' OR btrim(address) <> '');
ALTER TABLE theaters DROP CONSTRAINT IF EXISTS theaters_postal_code_check;
ALTER TABLE theaters ADD CONSTRAINT theaters_postal_code_check CHECK (provider = 'kinepolis' OR btrim(postal_code) <> '');
