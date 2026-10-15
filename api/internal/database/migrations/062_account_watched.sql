CREATE TABLE account_watched_state (
    account_id bigint PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    revision bigint NOT NULL CHECK (revision BETWEEN 1 AND 9007199254740991),
    default_visibility text NOT NULL DEFAULT 'hide' CHECK (default_visibility IN ('hide', 'show')),
    search_visibility text NOT NULL DEFAULT 'inherit' CHECK (search_visibility IN ('inherit', 'hide', 'show')),
    planning_visibility text NOT NULL DEFAULT 'inherit' CHECK (planning_visibility IN ('inherit', 'hide', 'show')),
    films_visibility text NOT NULL DEFAULT 'inherit' CHECK (films_visibility IN ('inherit', 'hide', 'show')),
    upcoming_visibility text NOT NULL DEFAULT 'inherit' CHECK (upcoming_visibility IN ('inherit', 'hide', 'show')),
    home_visibility text NOT NULL DEFAULT 'inherit' CHECK (home_visibility IN ('inherit', 'hide', 'show')),
    cinema_city_visibility text NOT NULL DEFAULT 'inherit' CHECK (cinema_city_visibility IN ('inherit', 'hide', 'show')),
    auto_remove_watchlist boolean NOT NULL DEFAULT false
);

CREATE TABLE account_watched_items (
    account_id bigint NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    public_movie_id bigint NOT NULL REFERENCES public_movies(id),
    watched_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, public_movie_id)
);
CREATE INDEX account_watched_items_movie_idx ON account_watched_items(public_movie_id);
