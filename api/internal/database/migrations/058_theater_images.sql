CREATE TABLE theater_images (
    provider varchar(32) NOT NULL CHECK (provider IN ('ugc', 'kinepolis', 'pathe', 'cgr', 'megarama', 'cineville', 'mk2', 'cinewest', 'grandecran', 'noecinemas')),
    provider_theater_id varchar(128) NOT NULL CHECK (length(btrim(provider_theater_id)) > 0),
    image_revision bigint NOT NULL DEFAULT 0 CHECK (image_revision BETWEEN 0 AND 9007199254740991),
    file_key text,
    width integer,
    height integer,
    size_bytes integer,
    PRIMARY KEY (provider, provider_theater_id),
    CONSTRAINT theater_images_presence_check CHECK (
        (file_key IS NULL AND width IS NULL AND height IS NULL AND size_bytes IS NULL)
        OR
        (file_key IS NOT NULL AND width IS NOT NULL AND height IS NOT NULL AND size_bytes IS NOT NULL
         AND image_revision > 0 AND file_key ~ '^[a-f0-9]{32}\.webp$'
         AND width BETWEEN 1 AND 1600 AND height BETWEEN 1 AND 1600
         AND size_bytes BETWEEN 1 AND 1048576)
    )
);

CREATE UNIQUE INDEX theater_images_file_key_idx ON theater_images (file_key) WHERE file_key IS NOT NULL;
