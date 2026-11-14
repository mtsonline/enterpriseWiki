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
    -- who disabled the account: '' (an administrator) or 'directory' (the
    -- directory sync; it enables the account again when it reappears)
    disabled_by    text NOT NULL DEFAULT '' CHECK (disabled_by IN ('', 'directory')),
    failed_logins  int NOT NULL DEFAULT 0,
    locked_until   timestamptz,
    attributes     jsonb NOT NULL DEFAULT '{}',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    last_login_at  timestamptz,
    -- personal settings of the user interface (keys see auth.Prefs)
    prefs          jsonb NOT NULL DEFAULT '{}',
    -- e-mail preferences per kind, e.g. {"tasks": true, "comments": false}; missing keys mean "on"
    notify         jsonb NOT NULL DEFAULT '{}',
    feed_token     text UNIQUE,                   -- personal feed links (feed readers cannot sign in)
    -- profile ("business card") maintained by the user or admins; not
    -- overwritten by identity providers (unlike attributes)
    profile        jsonb NOT NULL DEFAULT '{}',
    -- optional wiki user name (unique, also for signing in); set by the
    -- person, an administrator or a sign-in provider (alias_source)
    alias          text CHECK (alias ~ '^[A-Za-z0-9][A-Za-z0-9._-]{1,63}$'),
    alias_source   text NOT NULL DEFAULT '',
    -- provisioned by a SCIM client (and its id for the person)
    scim_client    bigint REFERENCES scim_clients(id) ON DELETE SET NULL,
    external_id    text NOT NULL DEFAULT '',
    UNIQUE (provider, subject),
    UNIQUE (provider, username),
    CHECK (username ~ '^[A-Za-z0-9._@+-]{1,128}$')
);

CREATE UNIQUE INDEX users_alias ON users(lower(alias)) WHERE alias IS NOT NULL;

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

-- runs of the directory sync (users and groups from LDAP/AD and other
-- directories without a sign-in); the newest 100 per provider are kept
CREATE TABLE directory_sync_runs (
    id           bigserial PRIMARY KEY,
    provider     text NOT NULL,
    dry_run      boolean NOT NULL,
    status       text NOT NULL CHECK (status IN ('ok', 'failed', 'refused')),
    triggered_by text NOT NULL DEFAULT '',          -- user name or 'schedule'
    started_at   timestamptz NOT NULL,
    finished_at  timestamptz NOT NULL DEFAULT now(),
    summary      jsonb NOT NULL DEFAULT '{}',
    error        text NOT NULL DEFAULT ''
);
CREATE INDEX directory_sync_runs_provider ON directory_sync_runs(provider, started_at DESC);

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

-- ---------------------------------------------------------------- changes
-- Change log of accounts: every way a person is created, disabled,
-- deleted or put into a group (administration, SCIM, sign-in providers,
-- LDAP) is recorded here by triggers; each site turns it into the events
-- user.* and group.* (webhooks, workflow triggers) within seconds. Personal
-- data is kept only for deleted people (name, no e-mail) and only for a
-- day: the details of everybody else are read when the event is sent.
CREATE TABLE identity_changes (
    seq          bigserial PRIMARY KEY,
    event        text NOT NULL,
    user_id      bigint NOT NULL,
    group_id     bigint,
    username     text NOT NULL DEFAULT '',  -- user.deleted only
    display_name text NOT NULL DEFAULT '',  -- user.deleted only
    at           timestamptz NOT NULL DEFAULT now()
);

CREATE FUNCTION identity_changes_users() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        EXECUTE format('INSERT INTO %I.identity_changes(event, user_id) VALUES ($1,$2)', TG_TABLE_SCHEMA) USING 'user.created', NEW.id;
    ELSIF TG_OP = 'DELETE' THEN
        EXECUTE format('INSERT INTO %I.identity_changes(event, user_id, username, display_name) VALUES ($1,$2,$3,$4)', TG_TABLE_SCHEMA)
            USING 'user.deleted', OLD.id, OLD.username, OLD.display_name;
    ELSIF NEW.disabled IS DISTINCT FROM OLD.disabled THEN
        EXECUTE format('INSERT INTO %I.identity_changes(event, user_id) VALUES ($1,$2)', TG_TABLE_SCHEMA)
            USING CASE WHEN NEW.disabled THEN 'user.disabled' ELSE 'user.enabled' END, NEW.id;
    END IF;
    RETURN NULL;
END $$;
CREATE TRIGGER users_identity_changed AFTER INSERT OR UPDATE OF disabled OR DELETE ON users
    FOR EACH ROW EXECUTE FUNCTION identity_changes_users();

CREATE FUNCTION identity_changes_groups() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r record; ev text;
BEGIN
    IF TG_OP = 'INSERT' THEN r := NEW; ev := 'group.member_added'; ELSE r := OLD; ev := 'group.member_removed'; END IF;
    -- nothing when the person or the group itself is being deleted (user.deleted covers it)
    EXECUTE format('INSERT INTO %1$I.identity_changes(event, user_id, group_id)
        SELECT $1, u.id, g.id FROM %1$I.users u, %1$I.groups g WHERE u.id = $2 AND g.id = $3', TG_TABLE_SCHEMA)
        USING ev, r.user_id, r.group_id;
    RETURN NULL;
END $$;
CREATE TRIGGER user_groups_identity_changed AFTER INSERT OR DELETE ON user_groups
    FOR EACH ROW EXECUTE FUNCTION identity_changes_groups();
