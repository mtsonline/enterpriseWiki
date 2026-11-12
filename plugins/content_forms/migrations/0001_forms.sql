-- Submissions of forms (element type content.forms). data holds the values
-- by field id, files the uploaded files ([{field, name, hash, size, mime,
-- comment}]); targets the state of the further targets (e-mail, webhook,
-- property database, workflow) for the delivery worker.
CREATE TABLE plugin_form_submissions (
    id          bigserial PRIMARY KEY,
    element_id  bigint NOT NULL REFERENCES elements(id) ON DELETE CASCADE,
    lang        text NOT NULL DEFAULT '',
    data        jsonb NOT NULL DEFAULT '{}',
    files       jsonb NOT NULL DEFAULT '[]',
    user_id     bigint REFERENCES users(id) ON DELETE SET NULL,
    author      text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    seen        boolean NOT NULL DEFAULT false,
    -- delivery of the further targets
    pending     jsonb NOT NULL DEFAULT '[]',
    attempts    int NOT NULL DEFAULT 0,
    next_at     timestamptz,
    last_error  text NOT NULL DEFAULT ''
);
CREATE INDEX plugin_form_submissions_element ON plugin_form_submissions(element_id, created_at DESC);
CREATE INDEX plugin_form_submissions_user ON plugin_form_submissions(element_id, user_id) WHERE user_id IS NOT NULL;
CREATE INDEX plugin_form_submissions_due ON plugin_form_submissions(next_at) WHERE next_at IS NOT NULL;

-- Secret of the form tokens (one row; shared by all nodes of a cluster).
CREATE TABLE plugin_form_secret (
    id      int PRIMARY KEY CHECK (id = 1),
    secret  bytea NOT NULL
);
