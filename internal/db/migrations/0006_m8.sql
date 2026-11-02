-- Watches: instant e-mails or a daily / weekly digest.
ALTER TABLE watches ADD COLUMN frequency text NOT NULL DEFAULT 'instant' CHECK (frequency IN ('instant', 'daily', 'weekly'));
-- 0 = instant, 1 = daily, 2 = weekly (the most urgent watch of a user wins)
ALTER TABLE watch_events ADD COLUMN freq smallint NOT NULL DEFAULT 0;

-- Webhooks: HTTP notifications about events (Teams, Slack, generic JSON).
CREATE TABLE webhooks (
    id           bigserial PRIMARY KEY,
    name         text NOT NULL,
    url          text NOT NULL,
    format       text NOT NULL DEFAULT 'json' CHECK (format IN ('json', 'slack', 'teams')),
    secret       text NOT NULL DEFAULT '',          -- HMAC-SHA256 signing key (json format)
    events       text[] NOT NULL DEFAULT '{}',      -- e.g. element.update, event.registration; empty = all
    category_id  bigint REFERENCES categories(id) ON DELETE CASCADE, -- only changes below this category
    enabled      boolean NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    last_status  int NOT NULL DEFAULT 0,
    last_error   text NOT NULL DEFAULT '',
    last_at      timestamptz
);

-- Deliveries waiting or retried (oldest first).
CREATE TABLE webhook_deliveries (
    id           bigserial PRIMARY KEY,
    webhook_id   bigint NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event        text NOT NULL,
    payload      jsonb NOT NULL,
    status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'failed')),
    attempts     int NOT NULL DEFAULT 0,
    next_at      timestamptz NOT NULL DEFAULT now(),
    last_status  int NOT NULL DEFAULT 0,
    last_error   text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    delivered_at timestamptz
);
CREATE INDEX webhook_deliveries_due ON webhook_deliveries(next_at) WHERE status = 'pending';
CREATE INDEX webhook_deliveries_hook ON webhook_deliveries(webhook_id, created_at DESC);
