ALTER TABLE showtimes
    ADD CONSTRAINT showtimes_language_vof_check CHECK (language <> 'VOF');

ALTER TABLE screening_history_showtimes
    ADD CONSTRAINT screening_history_showtimes_language_vof_check CHECK (language <> 'VOF');
