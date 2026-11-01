-- Milestone 4: e-mail, notifications, change log for external search indexes

-- Notification preferences per user, e.g. {"tasks": true, "comments": false}.
-- Missing keys mean "on".
ALTER TABLE users ADD COLUMN notify jsonb NOT NULL DEFAULT '{}';

-- Outgoing e-mail. Messages are written in the same transaction as the change
-- that causes them and sent by a background worker with retries.
CREATE TABLE mail_outbox (
    id              bigserial PRIMARY KEY,
    status          text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'sent', 'failed', 'held')),
    from_addr       text NOT NULL,
    from_name       text NOT NULL DEFAULT '',
    reply_to        text NOT NULL DEFAULT '',
    to_addrs        text[] NOT NULL,
    cc_addrs        text[] NOT NULL DEFAULT '{}',
    subject         text NOT NULL,
    body_text       text NOT NULL,
    source          text NOT NULL DEFAULT '',   -- e.g. workflow.process, core.comments, test
    ref             text NOT NULL DEFAULT '',   -- e.g. case:42
    attempts        int NOT NULL DEFAULT 0,
    last_error      text NOT NULL DEFAULT '',
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    created_at      timestamptz NOT NULL DEFAULT now(),
    sent_at         timestamptz
);
CREATE INDEX mail_outbox_due ON mail_outbox(next_attempt_at) WHERE status = 'queued';
CREATE INDEX mail_outbox_created ON mail_outbox(created_at DESC);

-- Change log of the search index. External search engines (plugins) follow
-- it to stay in sync with search_docs; PostgreSQL stays the source of truth.
CREATE TABLE search_changes (
    seq        bigserial PRIMARY KEY,
    element_id bigint NOT NULL,
    at         timestamptz NOT NULL DEFAULT now()
);

CREATE FUNCTION search_changes_docs() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        INSERT INTO search_changes(element_id) VALUES (OLD.element_id);
    ELSE
        INSERT INTO search_changes(element_id) VALUES (NEW.element_id);
    END IF;
    RETURN NULL;
END $$;
CREATE TRIGGER search_docs_changed AFTER INSERT OR UPDATE OR DELETE ON search_docs
    FOR EACH ROW EXECUTE FUNCTION search_changes_docs();

CREATE FUNCTION search_changes_elements() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        INSERT INTO search_changes(element_id) VALUES (OLD.id);
    ELSIF NEW.deleted_at IS DISTINCT FROM OLD.deleted_at OR NEW.tags IS DISTINCT FROM OLD.tags
          OR NEW.type IS DISTINCT FROM OLD.type OR NEW.category_id IS DISTINCT FROM OLD.category_id THEN
        INSERT INTO search_changes(element_id) VALUES (NEW.id);
    END IF;
    RETURN NULL;
END $$;
CREATE TRIGGER elements_search_changed AFTER UPDATE OR DELETE ON elements
    FOR EACH ROW EXECUTE FUNCTION search_changes_elements();
