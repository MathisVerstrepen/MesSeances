ALTER TABLE showtimes DROP CONSTRAINT showtimes_format_check;
ALTER TABLE showtimes ADD CONSTRAINT showtimes_format_check CHECK (format IN ('2D','3D','IMAX','DOLBY','SCREENX','LASER_ULTRA','4DX','ICE','INFINITY_VISION'));

-- LIKE INCLUDING CONSTRAINTS in migration 039 retains the original CHECK name.
ALTER TABLE screening_history_showtimes DROP CONSTRAINT showtimes_format_check;
ALTER TABLE screening_history_showtimes ADD CONSTRAINT showtimes_format_check CHECK (format IN ('2D','3D','IMAX','DOLBY','SCREENX','LASER_ULTRA','4DX','ICE','INFINITY_VISION'));
