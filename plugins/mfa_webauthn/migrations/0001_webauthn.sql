-- WebAuthn: the random user handle per user (the authenticator stores it in
-- passkeys; it reveals nothing about the account) and the registered
-- credentials (security keys, passkeys). The credential record is kept as
-- the library stores it (public key, sign count, flags, transports).
CREATE TABLE plugin_webauthn_users (
    user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    handle  bytea NOT NULL UNIQUE CHECK (length(handle) = 32)
);

CREATE TABLE plugin_webauthn_credentials (
    id           bigserial PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    cred_id      bytea NOT NULL UNIQUE,
    name         text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    passkey      boolean NOT NULL DEFAULT false,   -- discoverable: also for sign-in without password
    credential   jsonb NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);
CREATE INDEX plugin_webauthn_credentials_user ON plugin_webauthn_credentials(user_id);

-- registrations in progress (one per user, 5 minutes)
CREATE TABLE plugin_webauthn_registrations (
    user_id    bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    passkey    boolean NOT NULL,
    session    jsonb NOT NULL,
    expires_at timestamptz NOT NULL
);
