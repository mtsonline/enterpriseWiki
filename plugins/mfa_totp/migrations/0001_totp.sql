-- Authenticator app (TOTP) of a user: the shared secret and the last time
-- step used (every code works once).
CREATE TABLE plugin_mfa_totp (
    user_id    bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret     text NOT NULL,
    last_step  bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);
