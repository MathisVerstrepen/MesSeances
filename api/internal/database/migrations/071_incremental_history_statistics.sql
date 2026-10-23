-- Create an empty read model; the API background worker performs the one-time
-- backfill. No historical scan runs in the migration/startup transaction.
CREATE TABLE screening_statistics_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0),
    applied_revision bigint NOT NULL DEFAULT 0 CHECK (applied_revision >= 0 AND applied_revision <= revision),
    ready boolean NOT NULL DEFAULT false,
    processed_at timestamptz,
    coverage jsonb NOT NULL DEFAULT '{}'
);
INSERT INTO screening_statistics_state(singleton) VALUES(true);

CREATE TABLE screening_statistics_pending (
    provider text NOT NULL, movie_provider_id text NOT NULL, theater_id text NOT NULL, language text NOT NULL, format text NOT NULL,
    service_date date NOT NULL, hour smallint NOT NULL CHECK (hour BETWEEN 0 AND 23), delta bigint NOT NULL,
    PRIMARY KEY (provider,movie_provider_id,theater_id,language,format,service_date,hour)
);

CREATE TABLE screening_statistics_hourly (
    provider text NOT NULL, movie_provider_id text NOT NULL, theater_id text NOT NULL, language text NOT NULL, format text NOT NULL,
    service_date date NOT NULL, hour smallint NOT NULL CHECK (hour BETWEEN 0 AND 23), showtime_count bigint NOT NULL CHECK (showtime_count >= 0),
    PRIMARY KEY (provider,movie_provider_id,theater_id,language,format,service_date,hour)
);

CREATE TABLE screening_statistics_daily (
    provider text NOT NULL, movie_provider_id text NOT NULL, theater_id text NOT NULL, language text NOT NULL, format text NOT NULL,
    service_date date NOT NULL, showtime_count bigint NOT NULL CHECK (showtime_count >= 0),
    PRIMARY KEY (provider,movie_provider_id,theater_id,language,format,service_date)
);

CREATE TABLE screening_statistics_all_time (
    provider text NOT NULL, movie_provider_id text NOT NULL, theater_id text NOT NULL, language text NOT NULL, format text NOT NULL,
    weekday smallint NOT NULL CHECK (weekday BETWEEN 1 AND 7), hour smallint NOT NULL CHECK (hour BETWEEN 0 AND 23), showtime_count bigint NOT NULL CHECK (showtime_count >= 0),
    PRIMARY KEY (provider,movie_provider_id,theater_id,language,format,weekday,hour)
);

CREATE TABLE screening_statistics_memberships (
    provider text NOT NULL, movie_provider_id text NOT NULL, theater_id text NOT NULL, language text NOT NULL, format text NOT NULL,
    showtime_count bigint NOT NULL CHECK (showtime_count >= 0),
    PRIMARY KEY (provider,movie_provider_id,theater_id,language,format)
);

CREATE INDEX screening_statistics_hourly_date_idx ON screening_statistics_hourly(service_date);
CREATE INDEX screening_statistics_daily_date_idx ON screening_statistics_daily(service_date);
CREATE INDEX screening_statistics_memberships_theater_idx ON screening_statistics_memberships(theater_id);

-- Transition tables aggregate a publication's inserts/corrections in SQL. Only
-- statistical changes enter the queue; receipt/URL/room updates cancel out.
-- Lock order is state, then queue, for both publishers and the worker.
CREATE FUNCTION screening_statistics_capture() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE changed bigint;
BEGIN
    PERFORM 1 FROM screening_statistics_state WHERE singleton FOR UPDATE;
    IF TG_OP = 'INSERT' THEN
        INSERT INTO screening_statistics_pending(provider,movie_provider_id,theater_id,language,format,service_date,hour,delta)
        SELECT provider,movie_provider_id,theater_id,language,format,service_date,hour,sum(delta) FROM (SELECT provider,movie_provider_id,theater_id,language,format,service_date,extract(hour FROM start_time AT TIME ZONE 'Europe/Paris')::smallint AS hour,1::bigint delta FROM new_rows) changes
        GROUP BY provider,movie_provider_id,theater_id,language,format,service_date,hour HAVING sum(delta)<>0
        ON CONFLICT (provider,movie_provider_id,theater_id,language,format,service_date,hour) DO UPDATE SET delta=screening_statistics_pending.delta+excluded.delta;
    ELSIF TG_OP = 'UPDATE' THEN
        INSERT INTO screening_statistics_pending(provider,movie_provider_id,theater_id,language,format,service_date,hour,delta)
        SELECT provider,movie_provider_id,theater_id,language,format,service_date,hour,sum(delta) FROM (SELECT provider,movie_provider_id,theater_id,language,format,service_date,extract(hour FROM start_time AT TIME ZONE 'Europe/Paris')::smallint AS hour,-1::bigint delta FROM old_rows UNION ALL SELECT provider,movie_provider_id,theater_id,language,format,service_date,extract(hour FROM start_time AT TIME ZONE 'Europe/Paris')::smallint AS hour,1::bigint delta FROM new_rows) changes
        GROUP BY provider,movie_provider_id,theater_id,language,format,service_date,hour HAVING sum(delta)<>0
        ON CONFLICT (provider,movie_provider_id,theater_id,language,format,service_date,hour) DO UPDATE SET delta=screening_statistics_pending.delta+excluded.delta;
    ELSIF TG_OP = 'DELETE' THEN
        INSERT INTO screening_statistics_pending(provider,movie_provider_id,theater_id,language,format,service_date,hour,delta)
        SELECT provider,movie_provider_id,theater_id,language,format,service_date,hour,sum(delta) FROM (SELECT provider,movie_provider_id,theater_id,language,format,service_date,extract(hour FROM start_time AT TIME ZONE 'Europe/Paris')::smallint AS hour,-1::bigint delta FROM old_rows) changes
        GROUP BY provider,movie_provider_id,theater_id,language,format,service_date,hour HAVING sum(delta)<>0
        ON CONFLICT (provider,movie_provider_id,theater_id,language,format,service_date,hour) DO UPDATE SET delta=screening_statistics_pending.delta+excluded.delta;
    END IF;
    GET DIAGNOSTICS changed = ROW_COUNT;
    IF changed > 0 THEN
        UPDATE screening_statistics_state SET revision=revision+1 WHERE singleton;
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER screening_statistics_insert AFTER INSERT ON screening_history_showtimes
REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION screening_statistics_capture();
CREATE TRIGGER screening_statistics_update AFTER UPDATE ON screening_history_showtimes
REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION screening_statistics_capture();
CREATE TRIGGER screening_statistics_delete AFTER DELETE ON screening_history_showtimes
REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION screening_statistics_capture();

CREATE FUNCTION screening_statistics_coverage_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE screening_statistics_state SET revision=revision+1 WHERE singleton;
    RETURN NULL;
END;
$$;
CREATE TRIGGER screening_statistics_coverage AFTER INSERT OR UPDATE OR DELETE ON screening_history_providers
FOR EACH STATEMENT EXECUTE FUNCTION screening_statistics_coverage_changed();

CREATE FUNCTION screening_statistics_truncated() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE screening_statistics_state SET ready=false,revision=revision+1 WHERE singleton;
    RETURN NULL;
END;
$$;
CREATE TRIGGER screening_statistics_truncate AFTER TRUNCATE ON screening_history_showtimes
FOR EACH STATEMENT EXECUTE FUNCTION screening_statistics_truncated();
