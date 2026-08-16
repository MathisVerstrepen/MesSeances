ALTER TABLE movie_matches DROP CONSTRAINT movie_matches_status_check;
ALTER TABLE movie_matches ADD CONSTRAINT movie_matches_status_check CHECK (status IN ('matched', 'review_required', 'unmatched', 'rejected'));
