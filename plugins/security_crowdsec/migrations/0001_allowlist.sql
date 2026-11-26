-- Addresses and networks CrowdSec must never block (main site only). The
-- wiki writes them to the exchange file; CrowdSec applies them.
CREATE TABLE plugin_crowdsec_allowlist (
    value      text PRIMARY KEY CHECK (length(value) BETWEEN 2 AND 64),  -- canonical IP address or network
    note       text NOT NULL DEFAULT '' CHECK (length(note) <= 200),
    created_at timestamptz NOT NULL DEFAULT now()
);
