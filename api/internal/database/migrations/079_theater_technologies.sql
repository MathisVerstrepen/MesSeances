-- Empty read model; the history worker backfills from committed daily summaries.
CREATE TABLE theater_technology_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    version bigint NOT NULL DEFAULT 0 CHECK (version >= 0),
    ready boolean NOT NULL DEFAULT false,
    evaluated_on date,
    CHECK (ready = (evaluated_on IS NOT NULL))
);
INSERT INTO theater_technology_state (singleton) VALUES (true);

CREATE TABLE theater_technology_summary (
    theater_id varchar(128) NOT NULL REFERENCES screening_history_theaters(id),
    format varchar(16) NOT NULL CHECK (format IN ('3D','IMAX','DOLBY','SCREENX','LASER_ULTRA','4DX','ICE','INFINITY_VISION')),
    latest_advertised_date date NOT NULL,
    PRIMARY KEY (theater_id, format)
);

CREATE INDEX screening_statistics_daily_theater_date_idx
    ON screening_statistics_daily (theater_id, service_date);
