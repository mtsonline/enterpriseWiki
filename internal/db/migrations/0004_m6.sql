-- Milestone 6: reliable mail delivery per recipient, watches, feeds

-- One row per recipient: delivered, still pending (temporary errors are
-- retried without limit) or failed permanently (5xx or bounce).
CREATE TABLE mail_recipients (
    id              bigserial PRIMARY KEY,
    message_id      bigint NOT NULL REFERENCES mail_outbox(id) ON DELETE CASCADE,
    addr            text NOT NULL,
    kind            text NOT NULL DEFAULT 'to' CHECK (kind IN ('to', 'cc')),
    status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts        int NOT NULL DEFAULT 0,
    last_code       int NOT NULL DEFAULT 0,
    last_error      text NOT NULL DEFAULT '',
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    sent_at         timestamptz,
    failed_at       timestamptz,
    bounced         boolean NOT NULL DEFAULT false
);
CREATE INDEX mail_recipients_message ON mail_recipients(message_id);
CREATE INDEX mail_recipients_pending ON mail_recipients(next_attempt_at) WHERE status = 'pending';
CREATE INDEX mail_recipients_addr ON mail_recipients(lower(addr));

ALTER TABLE mail_outbox DROP CONSTRAINT mail_outbox_status_check;
ALTER TABLE mail_outbox ADD CONSTRAINT mail_outbox_status_check CHECK (status IN ('queued', 'sent', 'failed', 'partial', 'held'));
ALTER TABLE mail_outbox ADD COLUMN owner_id bigint REFERENCES users(id) ON DELETE SET NULL; -- who caused it (gets failure reports)
ALTER TABLE mail_outbox ADD COLUMN report boolean NOT NULL DEFAULT true;                   -- send failure reports
ALTER TABLE mail_outbox ADD COLUMN body_html text NOT NULL DEFAULT '';                     -- optional HTML part
ALTER TABLE mail_outbox ADD COLUMN lease_until timestamptz;                                -- claimed by a worker until
ALTER TABLE mail_outbox ADD COLUMN priority int NOT NULL DEFAULT 0;

INSERT INTO mail_recipients(message_id, addr, kind, status, attempts, last_error, sent_at, failed_at)
SELECT o.id, a.addr, a.kind,
       CASE o.status WHEN 'sent' THEN 'sent' WHEN 'failed' THEN 'failed' ELSE 'pending' END,
       o.attempts, o.last_error, o.sent_at, CASE WHEN o.status = 'failed' THEN now() END
FROM mail_outbox o
CROSS JOIN LATERAL (SELECT unnest(o.to_addrs) AS addr, 'to' AS kind UNION ALL SELECT unnest(o.cc_addrs), 'cc') a;

-- Watches: users follow categories (optionally with subcategories) or elements.
CREATE TABLE watches (
    id          bigserial PRIMARY KEY,
    user_id     bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type text NOT NULL CHECK (target_type IN ('category', 'element')),
    target_id   bigint NOT NULL,
    mode        text NOT NULL DEFAULT 'link' CHECK (mode IN ('link', 'content')),
    recursive   boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, target_type, target_id)
);
CREATE INDEX watches_target ON watches(target_type, target_id);

-- Changes waiting to be sent to watchers (coalesced per user and object;
-- sent as one e-mail per user after a quiet period).
CREATE TABLE watch_events (
    user_id     bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entity_type text NOT NULL CHECK (entity_type IN ('category', 'element')),
    entity_id   bigint NOT NULL,
    event       text NOT NULL,
    actor_id    bigint,
    mode        text NOT NULL,
    first_at    timestamptz NOT NULL DEFAULT now(),
    last_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, entity_type, entity_id)
);
CREATE INDEX watch_events_due ON watch_events(last_at);

-- Personal feed links (feed readers cannot sign in).
ALTER TABLE users ADD COLUMN feed_token text UNIQUE;

-- Atom/RSS feed of a category (and optionally its subcategories).
ALTER TABLE categories ADD COLUMN feed text NOT NULL DEFAULT 'off' CHECK (feed IN ('off', 'on', 'recursive'));

-- Profile ("business card") maintained by the user or admins; not
-- overwritten by identity providers (unlike attributes).
ALTER TABLE users ADD COLUMN profile jsonb NOT NULL DEFAULT '{}';

-- Generation of the guest cache: incremented on every change so all nodes
-- (and changes made by CLI processes) invalidate their in-memory caches.
CREATE TABLE cache_state (
    id          int PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    generation  bigint NOT NULL DEFAULT 0
);
INSERT INTO cache_state DEFAULT VALUES;
