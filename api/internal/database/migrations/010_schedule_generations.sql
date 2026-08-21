DO $$
BEGIN
    IF (EXISTS (SELECT 1 FROM provider_snapshots) OR EXISTS (SELECT 1 FROM theaters) OR
        EXISTS (SELECT 1 FROM theater_dates) OR EXISTS (SELECT 1 FROM theater_passes) OR
        EXISTS (SELECT 1 FROM movies) OR EXISTS (SELECT 1 FROM showtimes))
       AND NOT EXISTS (SELECT 1 FROM schedule_snapshot WHERE singleton = true) THEN
        RAISE EXCEPTION 'schedule rows require an active snapshot';
    END IF;
END $$;

ALTER TABLE provider_snapshots ADD COLUMN generation_id bigint;
ALTER TABLE theaters ADD COLUMN generation_id bigint;
ALTER TABLE theater_dates ADD COLUMN generation_id bigint;
ALTER TABLE theater_passes ADD COLUMN generation_id bigint;
ALTER TABLE movies ADD COLUMN generation_id bigint;
ALTER TABLE showtimes ADD COLUMN generation_id bigint;

UPDATE provider_snapshots SET generation_id = (SELECT version FROM schedule_snapshot WHERE singleton = true);
UPDATE theaters SET generation_id = (SELECT version FROM schedule_snapshot WHERE singleton = true);
UPDATE theater_dates SET generation_id = (SELECT version FROM schedule_snapshot WHERE singleton = true);
UPDATE theater_passes SET generation_id = (SELECT version FROM schedule_snapshot WHERE singleton = true);
UPDATE movies SET generation_id = (SELECT version FROM schedule_snapshot WHERE singleton = true);
UPDATE showtimes SET generation_id = (SELECT version FROM schedule_snapshot WHERE singleton = true);

ALTER TABLE provider_snapshots ALTER COLUMN generation_id SET NOT NULL;
ALTER TABLE theaters ALTER COLUMN generation_id SET NOT NULL;
ALTER TABLE theater_dates ALTER COLUMN generation_id SET NOT NULL;
ALTER TABLE theater_passes ALTER COLUMN generation_id SET NOT NULL;
ALTER TABLE movies ALTER COLUMN generation_id SET NOT NULL;
ALTER TABLE showtimes ALTER COLUMN generation_id SET NOT NULL;
ALTER TABLE provider_snapshots ADD CHECK (generation_id > 0);
ALTER TABLE theaters ADD CHECK (generation_id > 0);
ALTER TABLE theater_dates ADD CHECK (generation_id > 0);
ALTER TABLE theater_passes ADD CHECK (generation_id > 0);
ALTER TABLE movies ADD CHECK (generation_id > 0);
ALTER TABLE showtimes ADD CHECK (generation_id > 0);

ALTER TABLE showtimes DROP CONSTRAINT showtimes_theater_id_service_date_fkey;
ALTER TABLE showtimes DROP CONSTRAINT showtimes_provider_movie_provider_id_fkey;
ALTER TABLE theater_dates DROP CONSTRAINT theater_dates_theater_id_fkey;
ALTER TABLE theater_passes DROP CONSTRAINT theater_passes_theater_id_fkey;

ALTER TABLE provider_snapshots DROP CONSTRAINT provider_snapshots_pkey;
ALTER TABLE provider_snapshots ADD PRIMARY KEY (generation_id, provider);

ALTER TABLE theaters DROP CONSTRAINT theaters_pkey;
ALTER TABLE theaters DROP CONSTRAINT theaters_slug_key;
ALTER TABLE theaters DROP CONSTRAINT theaters_provider_provider_id_key;
ALTER TABLE theaters ADD PRIMARY KEY (generation_id, id);
ALTER TABLE theaters ADD UNIQUE (generation_id, slug);
ALTER TABLE theaters ADD UNIQUE (generation_id, provider, provider_id);

ALTER TABLE theater_dates DROP CONSTRAINT theater_dates_pkey;
ALTER TABLE theater_dates ADD PRIMARY KEY (generation_id, theater_id, service_date);
ALTER TABLE theater_dates ADD FOREIGN KEY (generation_id, theater_id) REFERENCES theaters(generation_id, id) ON DELETE CASCADE;

ALTER TABLE theater_passes DROP CONSTRAINT theater_passes_pkey;
ALTER TABLE theater_passes ADD PRIMARY KEY (generation_id, theater_id, pass_code);
ALTER TABLE theater_passes ADD FOREIGN KEY (generation_id, theater_id) REFERENCES theaters(generation_id, id) ON DELETE CASCADE;

ALTER TABLE movies DROP CONSTRAINT movies_pkey;
ALTER TABLE movies DROP CONSTRAINT movies_slug_key;
ALTER TABLE movies ADD PRIMARY KEY (generation_id, provider, provider_id);
ALTER TABLE movies ADD UNIQUE (generation_id, slug);

ALTER TABLE showtimes DROP CONSTRAINT showtimes_pkey;
ALTER TABLE showtimes DROP CONSTRAINT showtimes_provider_provider_showing_id_key;
ALTER TABLE showtimes ADD PRIMARY KEY (generation_id, id);
ALTER TABLE showtimes ADD UNIQUE (generation_id, provider, provider_showing_id);
ALTER TABLE showtimes ADD FOREIGN KEY (generation_id, provider, movie_provider_id) REFERENCES movies(generation_id, provider, provider_id);
ALTER TABLE showtimes ADD FOREIGN KEY (generation_id, theater_id, service_date) REFERENCES theater_dates(generation_id, theater_id, service_date);

DROP INDEX theaters_city_lower_idx;
DROP INDEX theater_dates_service_date_idx;
DROP INDEX theater_passes_pass_code_idx;
DROP INDEX showtimes_service_theater_start_idx;
DROP INDEX showtimes_service_window_idx;
DROP INDEX showtimes_movie_service_start_idx;
CREATE INDEX theaters_city_lower_idx ON theaters (generation_id, lower(city));
CREATE INDEX theater_dates_service_date_idx ON theater_dates (generation_id, service_date, theater_id);
CREATE INDEX theater_passes_pass_code_idx ON theater_passes (generation_id, pass_code, theater_id);
CREATE INDEX showtimes_service_theater_start_idx ON showtimes (generation_id, service_date, theater_id, start_time, id);
CREATE INDEX showtimes_service_window_idx ON showtimes (generation_id, service_date, start_time, end_time);
CREATE INDEX showtimes_movie_service_start_idx ON showtimes (generation_id, movie_provider_id, provider, service_date, start_time);
