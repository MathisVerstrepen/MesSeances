ALTER TABLE showtimes DROP CONSTRAINT showtimes_format_check;
ALTER TABLE showtimes ADD CONSTRAINT showtimes_format_check CHECK (format IN ('2D', '3D', 'IMAX', 'DOLBY', 'SCREENX', 'LASER_ULTRA', '4DX'));
