-- Milestone 2: external identities, single sign-on, two-factor auth, files

-- Roles synchronized from identity providers carry their source (like groups).
ALTER TABLE user_roles ADD COLUMN source text NOT NULL DEFAULT 'local';

-- TOTP two-factor authentication for local accounts.
ALTER TABLE users ADD COLUMN totp_secret text;
ALTER TABLE users ADD COLUMN totp_enabled boolean NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN totp_last_step bigint NOT NULL DEFAULT 0;

-- Pending single sign-on logins (OIDC, SAML): state, nonce, PKCE verifier.
CREATE TABLE sso_states (
    state      text PRIMARY KEY,
    provider   text NOT NULL,
    nonce      text NOT NULL,
    verifier   text NOT NULL,
    next       text NOT NULL DEFAULT '/',
    data       jsonb NOT NULL DEFAULT '{}',
    expires_at timestamptz NOT NULL
);

-- Uploaded files (content addressed: the SHA-256 is the key and the storage name).
CREATE TABLE files (
    hash       text PRIMARY KEY,
    size       bigint NOT NULL,
    mime       text NOT NULL,
    name       text NOT NULL,
    created_by bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
