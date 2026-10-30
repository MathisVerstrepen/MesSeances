-- TicketingCine also publishes the site/session route for global films.
-- Preserve exact site/showing binding, existing film paths, CHECKs and rows.
CREATE OR REPLACE FUNCTION majestic_booking_valid(value text, theater text, showing text, movie text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT length(value) <= 4096 AND majestic_identity_valid('theater',theater)
        AND majestic_identity_valid('showing',showing) AND substring(showing,5,4) = substring(theater,4,4)
        AND majestic_identity_valid('movie',movie)
        AND (movie ~ '^[A-Z0-9]{5}$' OR substring(movie,1,7) = theater)
        AND (value = majestic_website(theater)
            OR value IN (
                'https://www.ticketingcine.com?' || theater || '#showsession?id=' || showing,
                'https://www.ticketingcine.com?' || theater || '#showsession?id=' || showing || '&ps=ticketingcine')
            OR (movie ~ '^[A-Z0-9]{5}$' AND value IN (
                'https://www.ticketingcine.com/' || theater || '/FR' || movie || '#showsession?id=' || showing,
                'https://www.ticketingcine.com/' || theater || '/FR' || movie || '#showsession?id=' || showing || '&ps=ticketingcine')))
$$;
