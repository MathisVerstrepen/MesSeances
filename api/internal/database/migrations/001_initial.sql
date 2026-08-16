CREATE TABLE schedule_snapshot (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    version bigint NOT NULL CHECK (version > 0),
    schema_version integer NOT NULL CHECK (schema_version = 1),
    provider varchar(32) NOT NULL CHECK (provider = 'ugc'),
    scope varchar(32) NOT NULL CHECK (scope = 'all_cinemas'),
    generated_at timestamptz NOT NULL,
    timezone varchar(64) NOT NULL CHECK (timezone = 'Europe/Paris'),
    window_from date NOT NULL,
    window_through date NOT NULL,
    CHECK (window_through >= window_from AND window_through <= window_from + 13)
);

CREATE TABLE theaters (
    id varchar(128) PRIMARY KEY,
    provider_id varchar(128) NOT NULL UNIQUE,
    slug varchar(128) NOT NULL UNIQUE,
    name varchar(1024) NOT NULL CHECK (btrim(name) <> ''),
    address varchar(2048) NOT NULL CHECK (btrim(address) <> ''),
    city varchar(256) NOT NULL CHECK (btrim(city) <> ''),
    postal_code varchar(256) NOT NULL CHECK (btrim(postal_code) <> ''),
    CHECK (provider_id ~ '^[1-9][0-9]*$'),
    CHECK (id = 'ugc-' || provider_id),
    CHECK (slug = id)
);

CREATE TABLE theater_dates (
    theater_id varchar(128) NOT NULL REFERENCES theaters(id) ON DELETE CASCADE,
    service_date date NOT NULL,
    PRIMARY KEY (theater_id, service_date)
);

CREATE TABLE passes (
    code varchar(256) PRIMARY KEY CHECK (code = 'UGC_ILLIMITE')
);

CREATE TABLE theater_passes (
    theater_id varchar(128) NOT NULL REFERENCES theaters(id) ON DELETE CASCADE,
    pass_code varchar(256) NOT NULL REFERENCES passes(code) ON DELETE CASCADE,
    PRIMARY KEY (theater_id, pass_code)
);

CREATE TABLE movies (
    provider_id varchar(128) PRIMARY KEY,
    slug varchar(128) NOT NULL UNIQUE,
    title varchar(1024) NOT NULL CHECK (btrim(title) <> ''),
    runtime_minutes smallint NOT NULL CHECK (runtime_minutes BETWEEN 1 AND 600),
    poster_url varchar(4096),
    CHECK (provider_id ~ '^[1-9][0-9]*$'),
    CHECK (slug = 'ugc-film-' || provider_id)
);

CREATE TABLE showtimes (
    id varchar(128) PRIMARY KEY,
    provider_showing_id varchar(128) NOT NULL UNIQUE,
    service_date date NOT NULL,
    theater_id varchar(128) NOT NULL,
    movie_provider_id varchar(128) NOT NULL REFERENCES movies(provider_id),
    start_time timestamptz NOT NULL,
    end_time timestamptz NOT NULL,
    language varchar(16) NOT NULL CHECK (language IN ('VOSTFR', 'VF', 'VO', 'VF_SME')),
    provider_version varchar(256) NOT NULL CHECK (btrim(provider_version) <> ''),
    format varchar(16) NOT NULL CHECK (format IN ('2D', '3D', 'IMAX', 'DOLBY', '4DX')),
    room varchar(256) NOT NULL,
    booking_url varchar(4096) NOT NULL CHECK (btrim(booking_url) <> ''),
    CHECK (provider_showing_id ~ '^[1-9][0-9]*$'),
    CHECK (id = 'ugc-showing-' || provider_showing_id),
    CHECK (end_time > start_time),
    FOREIGN KEY (theater_id, service_date) REFERENCES theater_dates(theater_id, service_date)
);

CREATE INDEX theaters_city_lower_idx ON theaters (lower(city));
CREATE INDEX theater_dates_service_date_idx ON theater_dates (service_date, theater_id);
CREATE INDEX theater_passes_pass_code_idx ON theater_passes (pass_code, theater_id);
CREATE INDEX showtimes_service_theater_start_idx ON showtimes (service_date, theater_id, start_time, id);
CREATE INDEX showtimes_service_window_idx ON showtimes (service_date, start_time, end_time);
CREATE INDEX showtimes_movie_service_start_idx ON showtimes (movie_provider_id, service_date, start_time);
