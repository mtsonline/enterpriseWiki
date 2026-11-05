-- Enterprise Wiki shared schema (public)
--
-- Identities used by all sites of an instance: users, groups, role
-- definitions and SSH keys, plus the sites themselves. Sign-in (providers,
-- sessions) is configured per site.
CREATE EXTENSION IF NOT EXISTS ltree;

-- ---------------------------------------------------------------- identities
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
    -- e-mail preferences per kind, e.g. {"tasks": true, "comments": false}; missing keys mean "on"
    notify         jsonb NOT NULL DEFAULT '{}',
    feed_token     text UNIQUE,                   -- personal feed links (feed readers cannot sign in)
    -- profile ("business card") maintained by the user or admins; not
    -- overwritten by identity providers (unlike attributes)
    profile        jsonb NOT NULL DEFAULT '{}',
    UNIQUE (provider, subject),
    UNIQUE (provider, username),
    CHECK (username ~ '^[A-Za-z0-9._@+-]{1,128}$')
);

CREATE TABLE groups (
    id          bigserial PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    source      text NOT NULL DEFAULT 'local',
    created_at  timestamptz NOT NULL DEFAULT now()
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

-- ---------------------------------------------------------------- sites
-- Sites of the instance; each has its own schema (site_<name>) with all
-- content tables. The site "main" always exists.
CREATE TABLE sites (
    name       text PRIMARY KEY CHECK (name ~ '^[a-z][a-z0-9_]{0,30}$'),
    title      text NOT NULL DEFAULT '',
    hostnames  text[] NOT NULL DEFAULT '{}',
    disabled   boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO sites(name, title) VALUES ('main', '');

-- Instance administrators: manage sites, users, groups, roles and the license.
CREATE TABLE instance_admins (
    user_id    bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);

