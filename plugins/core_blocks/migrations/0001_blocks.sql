CREATE TABLE plugin_blocks (
    id         bigserial PRIMARY KEY,
    name       text NOT NULL,
    location   text NOT NULL,
    position   int NOT NULL DEFAULT 0,
    audience   text NOT NULL DEFAULT 'all' CHECK (audience IN ('all', 'users', 'guests')),
    enabled    boolean NOT NULL DEFAULT true,
    titles     jsonb NOT NULL DEFAULT '{}',  -- {"de": "Kontakt", ...}
    bodies     jsonb NOT NULL DEFAULT '{}',  -- {"de": {"format": "markdown", "source": "..."}, ...}
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX plugin_blocks_location ON plugin_blocks(location, position);
