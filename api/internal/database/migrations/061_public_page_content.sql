CREATE TABLE public_page_content_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    observed_at timestamptz
);

INSERT INTO public_page_content_state(singleton, observed_at) VALUES (true, NULL);

CREATE TABLE public_page_content (
    path text PRIMARY KEY CHECK (
        path ~ '^/(film|cinema)/[A-Za-z0-9][A-Za-z0-9_-]*$'
        OR path ~ '^/ville/[A-Za-z0-9][A-Za-z0-9_-]*/cinemas$'
    ),
    fingerprint bytea NOT NULL CHECK (octet_length(fingerprint) = 32),
    present boolean NOT NULL,
    changed_at timestamptz
);
