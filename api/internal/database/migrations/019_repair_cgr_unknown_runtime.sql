ALTER TABLE showtimes DROP CONSTRAINT IF EXISTS showtimes_check1;
ALTER TABLE showtimes DROP CONSTRAINT IF EXISTS showtimes_time_check;
ALTER TABLE showtimes ADD CONSTRAINT showtimes_time_check CHECK (
    end_time > start_time OR (provider = 'cgr' AND end_time = start_time)
);
