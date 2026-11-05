-- Enterprise Wiki shared schema (public)
--
-- Data of the whole instance: the sites and the instance administrators.
-- The shared accounts (identity/0001_identity.sql) are created here too.
-- Sign-in (providers, sessions) is configured per site.
CREATE EXTENSION IF NOT EXISTS ltree;

-- ---------------------------------------------------------------- sites
-- Sites of the instance; each has its own schema (site_<name>) with all
-- content tables. The site "main" always exists.
CREATE TABLE sites (
    name       text PRIMARY KEY CHECK (name ~ '^[a-z][a-z0-9_]{0,30}$'),
    title      text NOT NULL DEFAULT '',
    hostnames  text[] NOT NULL DEFAULT '{}',
    disabled   boolean NOT NULL DEFAULT false,
    -- shared: accounts of the instance (schema public); separate: own
    -- accounts in the site schema. Fixed when the site is created.
    accounts   text NOT NULL DEFAULT 'shared' CHECK (accounts IN ('shared', 'separate')),
    created_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO sites(name, title) VALUES ('main', '');

-- Instance administrators: manage sites, users, groups, roles and the license.
CREATE TABLE instance_admins (
    user_id    bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);

