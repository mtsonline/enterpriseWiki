-- Enterprise Wiki accounts: users, groups, role definitions, SSH keys.
--
-- Created in the shared schema (public) for all sites with shared accounts,
-- and additionally in the schema of each site with separate accounts
-- (sites.accounts = 'separate'), before that site's own tables, so their
-- foreign keys point to the site's own accounts.
-- ---------------------------------------------------------------- identities
-- SCIM 2.0 clients (Entra ID, Okta, ...) that provision users and groups;
-- only the hash of the bearer token is stored
CREATE TABLE scim_clients (
    id           bigserial PRIMARY KEY,
    name         text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    provider     text NOT NULL,                     -- sign-in the provisioned people use (oidc, oidc-entra, ...)
    subject_from text NOT NULL DEFAULT 'userName' CHECK (subject_from IN ('userName', 'externalId')),
    on_delete    text NOT NULL DEFAULT 'disable' CHECK (on_delete IN ('disable', 'delete')),
    token_hash   bytea NOT NULL UNIQUE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);

CREATE TABLE users (
    id             bigserial PRIMARY KEY,
    username       text NOT NULL,                 -- login name inside its provider
    provider       text NOT NULL DEFAULT 'local', -- auth provider id (local, ldap, oidc, ...)
    subject        text NOT NULL,                 -- stable id inside the provider
    display_name   text NOT NULL DEFAULT '',
    email          text NOT NULL DEFAULT '',
    password_hash  text,                          -- only for provider 'local'
    language       text NOT NULL DEFAULT '',
    disabled       boolean NOT NULL DEFAULT false,
    failed_logins  int NOT NULL DEFAULT 0,
    locked_until   timestamptz,
    attributes     jsonb NOT NULL DEFAULT '{}',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    last_login_at  timestamptz,
    -- TOTP two-factor authentication (local accounts)
    totp_secret    text,
    totp_enabled   boolean NOT NULL DEFAULT false,
    totp_last_step bigint NOT NULL DEFAULT 0,
    -- second factor by e-mail (local accounts): a code is sent at sign-in
    mfa_email      boolean NOT NULL DEFAULT false,
    -- sign-in and use only from these IP addresses / networks (CIDR); empty = anywhere
    allowed_networks text[] NOT NULL DEFAULT '{}',
    -- personal settings of the user interface (keys see auth.Prefs)
    prefs          jsonb NOT NULL DEFAULT '{}',
    -- e-mail preferences per kind, e.g. {"tasks": true, "comments": false}; missing keys mean "on"
    notify         jsonb NOT NULL DEFAULT '{}',
    feed_token     text UNIQUE,                   -- personal feed links (feed readers cannot sign in)
    -- profile ("business card") maintained by the user or admins; not
    -- overwritten by identity providers (unlike attributes)
    profile        jsonb NOT NULL DEFAULT '{}',
    -- provisioned by a SCIM client (and its id for the person)
    scim_client    bigint REFERENCES scim_clients(id) ON DELETE SET NULL,
    external_id    text NOT NULL DEFAULT '',
    UNIQUE (provider, subject),
    UNIQUE (provider, username),
    CHECK (username ~ '^[A-Za-z0-9._@+-]{1,128}$')
);

CREATE TABLE groups (
    id          bigserial PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    source      text NOT NULL DEFAULT 'local',
    created_at  timestamptz NOT NULL DEFAULT now(),
    scim_client bigint REFERENCES scim_clients(id) ON DELETE SET NULL,  -- managed by a SCIM client
    external_id text NOT NULL DEFAULT ''
);

CREATE TABLE roles (
    id          bigserial PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    is_admin    boolean NOT NULL DEFAULT false,   -- superadmin: bypasses all ACL checks
    created_at  timestamptz NOT NULL DEFAULT now(),
    permissions text[] NOT NULL DEFAULT '{}'      -- global permissions (package perm)
);

CREATE TABLE user_groups (
    user_id  bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id bigint NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    source   text NOT NULL DEFAULT 'local',
    PRIMARY KEY (user_id, group_id)
);
CREATE INDEX user_groups_group ON user_groups(group_id);

CREATE TABLE ssh_keys (
    id           bigserial PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         text NOT NULL,
    public_key   text NOT NULL,
    fingerprint  text NOT NULL UNIQUE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);

-- ---------------------------------------------------------------- e-mail addresses and linked sign-ins
-- A user has one or more e-mail addresses; a confirmed address belongs to
-- exactly one user. users.email mirrors the primary address.
CREATE TABLE user_emails (
    id          bigserial PRIMARY KEY,
    user_id     bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email       text NOT NULL CHECK (email = lower(email) AND length(email) BETWEEN 3 AND 254 AND position('@' in email) > 1),
    verified    boolean NOT NULL DEFAULT false,
    is_primary  boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    verified_at timestamptz,
    scim        boolean NOT NULL DEFAULT false,   -- delivered by a SCIM client (removed when it no longer sends it)
    UNIQUE (user_id, email)
);
CREATE UNIQUE INDEX user_emails_verified ON user_emails(email) WHERE verified;  -- one owner per confirmed address
CREATE UNIQUE INDEX user_emails_primary ON user_emails(user_id) WHERE is_primary;
CREATE INDEX user_emails_email ON user_emails(email);

-- confirmation links (only the hash of the token is stored)
CREATE TABLE email_tokens (
    token_hash bytea PRIMARY KEY,
    email_id   bigint NOT NULL REFERENCES user_emails(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);

-- further sign-ins of a user (the first one is users.provider/subject)
CREATE TABLE user_identities (
    provider      text NOT NULL,
    subject       text NOT NULL,
    user_id       bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    username      text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz,
    PRIMARY KEY (provider, subject)
);
CREATE INDEX user_identities_user ON user_identities(user_id);

-- sign-ins waiting for confirmation with the existing account (same e-mail
-- address, provider not trusted for automatic linking)
CREATE TABLE identity_links (
    token_hash bytea PRIMARY KEY,
    provider   text NOT NULL,
    subject    text NOT NULL,
    username   text NOT NULL,
    email      text NOT NULL,
    expires_at timestamptz NOT NULL
);
