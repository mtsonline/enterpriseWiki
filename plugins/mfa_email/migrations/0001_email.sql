-- Users who use codes by e-mail as a factor, and the codes sent (only the
-- hash is stored; login: at a sign-in, setup: to switch the factor on).
CREATE TABLE plugin_mfa_email (
    user_id    bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE plugin_mfa_email_codes (
    user_id    bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose    text NOT NULL CHECK (purpose IN ('login', 'setup')),
    code_hash  bytea NOT NULL,
    attempts   int NOT NULL DEFAULT 0,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, purpose)
);
