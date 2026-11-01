-- Enterprise Wiki core schema (v7)
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
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_groups (
    user_id  bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id bigint NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    source   text NOT NULL DEFAULT 'local',
    PRIMARY KEY (user_id, group_id)
);
CREATE INDEX user_groups_group ON user_groups(group_id);

CREATE TABLE user_roles (
    user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id bigint NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE group_roles (
    group_id bigint NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    role_id  bigint NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, role_id)
);

CREATE TABLE sessions (
    token_hash   bytea PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind         text NOT NULL DEFAULT 'web',      -- web | ssh
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    ip           text NOT NULL DEFAULT '',
    user_agent   text NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions(user_id);
CREATE INDEX sessions_expires ON sessions(expires_at);

CREATE TABLE ssh_keys (
    id           bigserial PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         text NOT NULL,
    public_key   text NOT NULL,
    fingerprint  text NOT NULL UNIQUE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);

CREATE TABLE auth_challenges (
    nonce      text PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);

-- ---------------------------------------------------------------- content
CREATE TABLE categories (
    id                 bigserial PRIMARY KEY,
    parent_id          bigint REFERENCES categories(id) ON DELETE RESTRICT,
    path               ltree NOT NULL,              -- ancestor ids incl. self, e.g. 1.7.42
    position           int NOT NULL DEFAULT 0,
    slug               text NOT NULL DEFAULT '',
    titles             jsonb NOT NULL DEFAULT '{}', -- {"en": "...", "de": "..."}
    descriptions       jsonb NOT NULL DEFAULT '{}',
    tags               text[] NOT NULL DEFAULT '{}',
    default_element_id bigint,
    created_by         bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    deleted_at         timestamptz,
    deleted_by         bigint REFERENCES users(id) ON DELETE SET NULL,
    trash_root         boolean NOT NULL DEFAULT false -- the object the user deleted
);
CREATE INDEX categories_path ON categories USING gist(path);
CREATE INDEX categories_parent ON categories(parent_id, position);
CREATE INDEX categories_tags ON categories USING gin(tags);

CREATE TABLE elements (
    id            bigserial PRIMARY KEY,
    category_id   bigint NOT NULL REFERENCES categories(id) ON DELETE RESTRICT,
    type          text NOT NULL,
    position      int NOT NULL DEFAULT 0,
    original_lang text NOT NULL,
    titles        jsonb NOT NULL DEFAULT '{}',      -- cache of the latest title per language
    tags          text[] NOT NULL DEFAULT '{}',
    created_by    bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_by    bigint REFERENCES users(id) ON DELETE SET NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz,
    deleted_by    bigint REFERENCES users(id) ON DELETE SET NULL,
    trash_root    boolean NOT NULL DEFAULT false
);
CREATE INDEX elements_category ON elements(category_id, position);
CREATE INDEX elements_tags ON elements USING gin(tags);

ALTER TABLE categories ADD CONSTRAINT categories_default_element
    FOREIGN KEY (default_element_id) REFERENCES elements(id) ON DELETE SET NULL;

-- Every save creates a new immutable revision per language.
CREATE TABLE element_revisions (
    id          bigserial PRIMARY KEY,
    element_id  bigint NOT NULL REFERENCES elements(id) ON DELETE CASCADE,
    lang        text NOT NULL,
    rev         int NOT NULL,
    source_rev  int,          -- translations: revision of the original language this is based on
    title       text NOT NULL,
    body        jsonb NOT NULL,
    comment     text NOT NULL DEFAULT '',
    restored_from int,
    created_by  bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (element_id, lang, rev)
);
CREATE INDEX element_revisions_time ON element_revisions(element_id, created_at);

-- Full text index, one row per element and language (latest revision).
CREATE TABLE search_docs (
    element_id  bigint NOT NULL REFERENCES elements(id) ON DELETE CASCADE,
    lang        text NOT NULL,
    category_id bigint NOT NULL,
    title       text NOT NULL,
    body_text   text NOT NULL,
    tags        text[] NOT NULL DEFAULT '{}',
    tsv         tsvector NOT NULL, -- title (A), tags (B), body (C); maintained by the application
    PRIMARY KEY (element_id, lang)
);
CREATE INDEX search_docs_tsv ON search_docs USING gin(tsv);

-- ---------------------------------------------------------------- permissions
CREATE TABLE acl_rules (
    id           bigserial PRIMARY KEY,
    category_id  bigint NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    subject_type text NOT NULL CHECK (subject_type IN ('user','group','role','guest','authenticated')),
    subject_id   bigint,
    permissions  int NOT NULL CHECK (permissions > 0),
    inherit      boolean NOT NULL DEFAULT true,     -- also applies to all subcategories
    created_by   bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CHECK ((subject_type IN ('guest','authenticated')) = (subject_id IS NULL))
);
CREATE INDEX acl_rules_category ON acl_rules(category_id);

CREATE TABLE api_keys (
    id           bigserial PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         text NOT NULL,
    prefix       text NOT NULL,
    token_hash   bytea NOT NULL UNIQUE,
    permissions  int NOT NULL,                     -- upper bound of category permissions
    category_id  bigint REFERENCES categories(id) ON DELETE CASCADE, -- optional subtree restriction
    allow_admin  boolean NOT NULL DEFAULT false,
    expires_at   timestamptz NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at   timestamptz
);
CREATE INDEX api_keys_user ON api_keys(user_id);

-- ---------------------------------------------------------------- history / i18n
CREATE TABLE audit_log (
    id          bigserial PRIMARY KEY,
    at          timestamptz NOT NULL DEFAULT now(),
    user_id     bigint REFERENCES users(id) ON DELETE SET NULL,
    actor       text NOT NULL DEFAULT '',
    action      text NOT NULL,
    entity_type text NOT NULL,
    entity_id   bigint,
    before      jsonb,
    after       jsonb
);
CREATE INDEX audit_entity ON audit_log(entity_type, entity_id, at);
CREATE INDEX audit_at ON audit_log(at);

CREATE TABLE ui_translations (
    lang       text NOT NULL,
    key        text NOT NULL,
    value      text NOT NULL,
    updated_by bigint REFERENCES users(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (lang, key)
);
