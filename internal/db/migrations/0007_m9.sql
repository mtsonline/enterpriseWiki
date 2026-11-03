-- One-time notifications already sent (e.g. "PostgreSQL 19 recommended",
-- "PostgreSQL 16 support ends in 30 days").
CREATE TABLE system_notices (
    key      text PRIMARY KEY,
    sent_at  timestamptz NOT NULL DEFAULT now()
);
