-- Enterprise Wiki site schema (site_<name>)
--
-- All tables of one site. Created in the site schema; shared tables
-- (users, groups, roles, sessions) are found through the search path.
-- Later changes are added as further migrations (NNNN_name.sql) once
-- installations are in use.

-- ---------------------------------------------------------------- sign-in (per site)
CREATE TABLE sessions (
    token_hash   bytea PRIMARY KEY,
    id           bigserial UNIQUE,                 -- shown to the user ("my sessions"); the token never is
    user_id      bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind         text NOT NULL DEFAULT 'web',      -- web | ssh
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    ip           text NOT NULL DEFAULT '',
    user_agent   text NOT NULL DEFAULT '',
    -- single sign-on: provider, its session (OIDC sid, SAML SessionIndex)
    -- and what it needs to end it (OIDC id token, SAML NameID)
    sso_provider text NOT NULL DEFAULT '',
    sso_subject  text NOT NULL DEFAULT '',
    sso_session  text NOT NULL DEFAULT '',
    sso_hint     text NOT NULL DEFAULT ''
);
CREATE INDEX sessions_sso ON sessions(sso_provider, sso_session) WHERE sso_provider <> '';
CREATE INDEX sessions_user ON sessions(user_id);
CREATE INDEX sessions_expires ON sessions(expires_at);

-- One-time codes sent by e-mail (second factor at sign-in, enabling it).
CREATE TABLE mfa_codes (
    user_id    bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose    text NOT NULL CHECK (purpose IN ('login', 'setup')),
    code_hash  bytea NOT NULL,
    attempts   int NOT NULL DEFAULT 0,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, purpose)
);

CREATE TABLE auth_challenges (
    nonce      text PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);

-- Sign-ins waiting for the second factor (only the hash of the token is
-- stored); sso keeps what the session needs for single logout, data the
-- challenge state of the factors (e.g. WebAuthn session data)
CREATE TABLE mfa_pending (
    token_hash bytea PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sso        jsonb NOT NULL DEFAULT '{}',
    next       text NOT NULL DEFAULT '/',
    data       jsonb NOT NULL DEFAULT '{}',
    attempts   int NOT NULL DEFAULT 0,
    expires_at timestamptz NOT NULL
);

-- Password-less sign-ins in progress (passkeys): state of the method
CREATE TABLE passwordless_pending (
    token_hash bytea PRIMARY KEY,
    method     text NOT NULL,
    state      text NOT NULL,
    expires_at timestamptz NOT NULL
);

-- IDs of single-use messages of identity providers (OIDC logout token jti,
-- SAML LogoutRequest IDs): each is accepted once, on every instance
CREATE TABLE sso_replay (
    id         text PRIMARY KEY,
    expires_at timestamptz NOT NULL
);

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

-- ---------------------------------------------------------------- role assignments (per site)
CREATE TABLE user_roles (
    user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id bigint NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    source  text NOT NULL DEFAULT 'local',       -- synchronized from an identity provider
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE group_roles (
    group_id bigint NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    role_id  bigint NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, role_id)
);

-- global permissions of every signed-in user (no row = the declared defaults)
CREATE TABLE permission_defaults (
    subject     text PRIMARY KEY CHECK (subject IN ('authenticated')),
    permissions text[] NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
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
    trash_root         boolean NOT NULL DEFAULT false, -- the object the user deleted
    feed               text NOT NULL DEFAULT 'off' CHECK (feed IN ('off', 'on', 'recursive')), -- Atom/RSS feed
    short_url          text,                          -- e.g. /kalender; unique with elements.short_url (checked by the application)
    short_only         boolean NOT NULL DEFAULT false, -- only the short URL (no readable path /wiki/… for this category)
    hidden             boolean NOT NULL DEFAULT false, -- not in navigation, menus, listings and search (e.g. folders for media)
    -- translations of title and description (like elements): the original
    -- language, its revision (incremented when its texts change) and the
    -- revision of the original each translation is based on
    original_lang      text NOT NULL DEFAULT '',
    text_rev           int NOT NULL DEFAULT 1,
    translation_revs   jsonb NOT NULL DEFAULT '{}'    -- {"de": 3, "fr": 2}
);
CREATE INDEX categories_path ON categories USING gist(path);
CREATE INDEX categories_parent ON categories(parent_id, position);
CREATE INDEX categories_tags ON categories USING gin(tags);
CREATE UNIQUE INDEX categories_short_url ON categories(short_url) WHERE short_url IS NOT NULL;

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
    trash_root    boolean NOT NULL DEFAULT false,
    short_url     text,
    preview_image text NOT NULL DEFAULT '',          -- og:image for link previews (file hash or URL; empty = automatic)
    short_only    boolean NOT NULL DEFAULT false     -- only the short URL (no readable paths /wiki/…)
);
CREATE INDEX elements_category ON elements(category_id, position);
CREATE INDEX elements_tags ON elements USING gin(tags);
CREATE UNIQUE INDEX elements_short_url ON elements(short_url) WHERE short_url IS NOT NULL;

-- Segments of readable paths /wiki/<categories>/<page>, one per language
-- (from the title in that language; the original language is always
-- there). A segment belongs to one target among the children of a parent
-- category (0 = top level); a target may use the same segment in several
-- languages.
CREATE TABLE path_slugs (
    target_type text NOT NULL CHECK (target_type IN ('element', 'category')),
    target_id   bigint NOT NULL,
    lang        text NOT NULL,
    parent_id   bigint NOT NULL DEFAULT 0,
    slug        text NOT NULL,
    PRIMARY KEY (target_type, target_id, lang)
);
CREATE INDEX path_slugs_lookup ON path_slugs(parent_id, slug);

-- Former addresses: readable path segments (scope "path": slug below a
-- parent category, 0 = top level) and short URLs (scope "short"). Old links
-- are redirected to the current address of the target.
CREATE TABLE url_aliases (
    scope       text NOT NULL CHECK (scope IN ('path', 'short')),
    parent_id   bigint NOT NULL DEFAULT 0,
    slug        text NOT NULL,
    target_type text NOT NULL CHECK (target_type IN ('element', 'category')),
    target_id   bigint NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (scope, parent_id, slug)
);
CREATE INDEX url_aliases_target ON url_aliases(target_type, target_id);

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

-- Change log of the search index. External search engines (plugins) follow
-- it to stay in sync with search_docs; PostgreSQL stays the source of truth.
CREATE TABLE search_changes (
    seq        bigserial PRIMARY KEY,
    element_id bigint NOT NULL,
    at         timestamptz NOT NULL DEFAULT now()
);

CREATE FUNCTION search_changes_docs() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        INSERT INTO search_changes(element_id) VALUES (OLD.element_id);
    ELSE
        INSERT INTO search_changes(element_id) VALUES (NEW.element_id);
    END IF;
    RETURN NULL;
END $$;
CREATE TRIGGER search_docs_changed AFTER INSERT OR UPDATE OR DELETE ON search_docs
    FOR EACH ROW EXECUTE FUNCTION search_changes_docs();

CREATE FUNCTION search_changes_elements() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        INSERT INTO search_changes(element_id) VALUES (OLD.id);
    ELSIF NEW.deleted_at IS DISTINCT FROM OLD.deleted_at OR NEW.tags IS DISTINCT FROM OLD.tags
          OR NEW.type IS DISTINCT FROM OLD.type OR NEW.category_id IS DISTINCT FROM OLD.category_id THEN
        INSERT INTO search_changes(element_id) VALUES (NEW.id);
    END IF;
    RETURN NULL;
END $$;
CREATE TRIGGER elements_search_changed AFTER UPDATE OR DELETE ON elements
    FOR EACH ROW EXECUTE FUNCTION search_changes_elements();

-- drafts: changes of an element (one per language, shared by all who may
-- edit it) or a new element (element_id NULL) that is not published yet
CREATE TABLE element_drafts (
    id              bigserial PRIMARY KEY,
    element_id      bigint REFERENCES elements(id) ON DELETE CASCADE,
    category_id     bigint NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    type            text NOT NULL,
    lang            text NOT NULL,
    title           text NOT NULL,
    body            jsonb NOT NULL,
    tags            text[],                       -- NULL: keep the tags of the element
    comment         text NOT NULL DEFAULT '',
    base_rev        int NOT NULL DEFAULT 0,
    created_by      bigint REFERENCES users(id) ON DELETE SET NULL,
    updated_by      bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    publish_at      timestamptz,                   -- scheduled publication
    publish_by      bigint REFERENCES users(id) ON DELETE SET NULL,
    publish_options jsonb NOT NULL DEFAULT '{}'
);
CREATE UNIQUE INDEX element_drafts_element ON element_drafts(element_id, lang) WHERE element_id IS NOT NULL;
CREATE INDEX element_drafts_category ON element_drafts(category_id);
CREATE INDEX element_drafts_due ON element_drafts(publish_at) WHERE publish_at IS NOT NULL;

-- templates for new elements: personal (owner_id) or for everybody (NULL)
CREATE TABLE element_templates (
    id          bigserial PRIMARY KEY,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    type        text NOT NULL,
    lang        text NOT NULL,
    title       text NOT NULL DEFAULT '',
    body        jsonb NOT NULL,
    tags        text[] NOT NULL DEFAULT '{}',
    owner_id    bigint REFERENCES users(id) ON DELETE CASCADE,
    created_by  bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX element_templates_owner ON element_templates(owner_id);

-- Uploaded files (content addressed: the SHA-256 is the key and the storage name).
CREATE TABLE files (
    hash       text PRIMARY KEY,
    size       bigint NOT NULL,
    mime       text NOT NULL,
    name       text NOT NULL,
    created_by bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------- media library
-- A medium is a file in a category with versions (image editor, new uploads),
-- its own rights (optional), a download switch and a watermark mode.
-- See docs/developer/media.md.
CREATE TABLE media (
    id          bigserial PRIMARY KEY,
    category_id bigint NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    name        text NOT NULL,                       -- file name
    title       text NOT NULL DEFAULT '',
    alt         text NOT NULL DEFAULT '',            -- alternative text for images
    description text NOT NULL DEFAULT '',
    current     int NOT NULL DEFAULT 1,              -- current version
    download    boolean NOT NULL DEFAULT true,       -- false: view only (also for users with the right)
    own_acl     boolean NOT NULL DEFAULT false,      -- only media_acl counts (not category / embedding elements)
    watermark   jsonb NOT NULL DEFAULT '{"mode":"none"}',
    created_by  bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    deleted_by  bigint REFERENCES users(id) ON DELETE SET NULL
);
CREATE INDEX media_category ON media(category_id) WHERE deleted_at IS NULL;

CREATE TABLE media_versions (
    media_id   bigint NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    version    int NOT NULL,
    hash       text NOT NULL REFERENCES files(hash),
    mime       text NOT NULL,
    size       bigint NOT NULL,
    width      int NOT NULL DEFAULT 0,
    height     int NOT NULL DEFAULT 0,
    ops        jsonb NOT NULL DEFAULT '[]',          -- image editor steps that produced this version
    note       text NOT NULL DEFAULT '',
    created_by bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (media_id, version)
);
CREATE INDEX media_versions_hash ON media_versions(hash);

-- own rights of a medium (media.own_acl): bits view 1, download 2, edit 4, manage 8
CREATE TABLE media_acl (
    id           bigserial PRIMARY KEY,
    media_id     bigint NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    subject_type text NOT NULL CHECK (subject_type IN ('user','group','role','guest','authenticated')),
    subject_id   bigint,
    perms        int NOT NULL CHECK (perms > 0 AND perms < 16),
    CHECK ((subject_type IN ('guest','authenticated')) = (subject_id IS NULL))
);
CREATE INDEX media_acl_media ON media_acl(media_id);

-- elements that embed a medium (derived from their content on every change)
CREATE TABLE media_usage (
    media_id   bigint NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    element_id bigint NOT NULL REFERENCES elements(id) ON DELETE CASCADE,
    PRIMARY KEY (media_id, element_id)
);
CREATE INDEX media_usage_element ON media_usage(element_id);

-- access tokens for office programs (WOPI / OnlyOffice): one file, one
-- user, one mode; only the SHA-256 of the token is stored
CREATE TABLE media_access_tokens (
    token_hash bytea PRIMARY KEY,
    media_id   bigint NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    user_id    bigint REFERENCES users(id) ON DELETE CASCADE,  -- NULL: guest
    viewer     text NOT NULL,                                  -- plugin id
    mode       text NOT NULL CHECK (mode IN ('edit','view')),
    restricted boolean NOT NULL DEFAULT false,                 -- no download, print or copy
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX media_access_tokens_expiry ON media_access_tokens(expires_at);

-- WOPI locks (one per medium)
CREATE TABLE media_locks (
    media_id   bigint PRIMARY KEY REFERENCES media(id) ON DELETE CASCADE,
    lock_id    text NOT NULL CHECK (length(lock_id) <= 1024),
    expires_at timestamptz NOT NULL
);

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
    actions      text[] NOT NULL DEFAULT '{}',      -- named category permissions of plugins (in addition to the bits)
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
    after       jsonb,
    -- who: IP address, browser, channel (session, api, cli, worker, ...)
    ip          text,
    user_agent  text,
    via         text
);
CREATE INDEX audit_entity ON audit_log(entity_type, entity_id, at);
CREATE INDEX audit_at ON audit_log(at);
CREATE INDEX audit_action ON audit_log(action, at);
CREATE INDEX audit_user ON audit_log(user_id, at);

-- position of each forwarding destination (Administration → Audit log → Forwarding)
CREATE TABLE audit_forward_state (
    id         text PRIMARY KEY,
    last_id    bigint NOT NULL DEFAULT 0,
    last_at    timestamptz,
    last_error text NOT NULL DEFAULT '',
    error_at   timestamptz,
    errors     int NOT NULL DEFAULT 0
);

-- UI texts translated in the administration (override the shipped files)
CREATE TABLE ui_translations (
    lang        text NOT NULL,
    key         text NOT NULL,
    value       text NOT NULL,
    source_hash text NOT NULL DEFAULT '',  -- hash of the English text it translates (changed = outdated)
    updated_by  bigint REFERENCES users(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (lang, key)
);

-- ---------------------------------------------------------------- e-mail
-- Outgoing e-mail. Messages are written in the same transaction as the change
-- that causes them and sent by a background worker with retries.
CREATE TABLE mail_outbox (
    id              bigserial PRIMARY KEY,
    status          text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'sent', 'failed', 'partial', 'held')),
    from_addr       text NOT NULL,
    from_name       text NOT NULL DEFAULT '',
    reply_to        text NOT NULL DEFAULT '',
    to_addrs        text[] NOT NULL,
    cc_addrs        text[] NOT NULL DEFAULT '{}',
    subject         text NOT NULL,
    body_text       text NOT NULL,
    body_html       text NOT NULL DEFAULT '',      -- optional HTML part
    body_calendar   text NOT NULL DEFAULT '',      -- calendar invitation (iTIP) as text/calendar part
    source          text NOT NULL DEFAULT '',      -- e.g. workflow.process, core.comments, test
    ref             text NOT NULL DEFAULT '',      -- e.g. case:42
    attempts        int NOT NULL DEFAULT 0,
    last_error      text NOT NULL DEFAULT '',
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    created_at      timestamptz NOT NULL DEFAULT now(),
    sent_at         timestamptz,
    owner_id        bigint REFERENCES users(id) ON DELETE SET NULL, -- who caused it (gets failure reports)
    report          boolean NOT NULL DEFAULT true,  -- send failure reports
    lease_until     timestamptz,                    -- claimed by a worker until
    priority        int NOT NULL DEFAULT 0
);
CREATE INDEX mail_outbox_due ON mail_outbox(next_attempt_at) WHERE status = 'queued';
CREATE INDEX mail_outbox_created ON mail_outbox(created_at DESC);

-- One row per recipient: delivered, still pending (temporary errors are
-- retried without limit) or failed permanently (5xx or bounce).
CREATE TABLE mail_recipients (
    id              bigserial PRIMARY KEY,
    message_id      bigint NOT NULL REFERENCES mail_outbox(id) ON DELETE CASCADE,
    addr            text NOT NULL,
    kind            text NOT NULL DEFAULT 'to' CHECK (kind IN ('to', 'cc')),
    status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts        int NOT NULL DEFAULT 0,
    last_code       int NOT NULL DEFAULT 0,
    last_error      text NOT NULL DEFAULT '',
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    sent_at         timestamptz,
    failed_at       timestamptz,
    bounced         boolean NOT NULL DEFAULT false
);
CREATE INDEX mail_recipients_message ON mail_recipients(message_id);
CREATE INDEX mail_recipients_pending ON mail_recipients(next_attempt_at) WHERE status = 'pending';
CREATE INDEX mail_recipients_addr ON mail_recipients(lower(addr));

-- ---------------------------------------------------------------- watches and notifications
-- Watches: users follow categories (optionally with subcategories) or elements.
CREATE TABLE watches (
    id          bigserial PRIMARY KEY,
    user_id     bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type text NOT NULL CHECK (target_type IN ('category', 'element')),
    target_id   bigint NOT NULL,
    mode        text NOT NULL DEFAULT 'link' CHECK (mode IN ('link', 'content')),
    recursive   boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    frequency   text NOT NULL DEFAULT 'instant' CHECK (frequency IN ('instant', 'daily', 'weekly')), -- e-mail or digest
    UNIQUE (user_id, target_type, target_id)
);
CREATE INDEX watches_target ON watches(target_type, target_id);

-- Changes waiting to be sent to watchers (coalesced per user and object;
-- sent as one e-mail per user after a quiet period).
CREATE TABLE watch_events (
    user_id     bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entity_type text NOT NULL CHECK (entity_type IN ('category', 'element')),
    entity_id   bigint NOT NULL,
    event       text NOT NULL,
    actor_id    bigint,
    mode        text NOT NULL,
    first_at    timestamptz NOT NULL DEFAULT now(),
    last_at     timestamptz NOT NULL DEFAULT now(),
    freq        smallint NOT NULL DEFAULT 0,       -- 0 = instant, 1 = daily, 2 = weekly (the most urgent watch of a user wins)
    PRIMARY KEY (user_id, entity_type, entity_id)
);
CREATE INDEX watch_events_due ON watch_events(last_at);

-- watches assigned centrally to users, groups or roles (dynamic: later
-- members watch too); optional ones can be unsubscribed per user
CREATE TABLE watch_assignments (
    id           bigserial PRIMARY KEY,
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'group', 'role')),
    subject_id   bigint NOT NULL,
    target_type  text NOT NULL CHECK (target_type IN ('category', 'element')),
    target_id    bigint NOT NULL,
    mode         text NOT NULL DEFAULT 'link' CHECK (mode IN ('link', 'content')),
    recursive    boolean NOT NULL DEFAULT true,
    frequency    text NOT NULL DEFAULT 'instant' CHECK (frequency IN ('instant', 'daily', 'weekly')),
    mandatory    boolean NOT NULL DEFAULT false,
    note         text NOT NULL DEFAULT '',
    created_by   bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (subject_type, subject_id, target_type, target_id)
);
CREATE TABLE watch_optouts (
    assignment_id bigint NOT NULL REFERENCES watch_assignments(id) ON DELETE CASCADE,
    user_id       bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (assignment_id, user_id)
);
-- own watches plus assigned ones (without unsubscribed optional ones)
CREATE VIEW watch_effective AS
    SELECT w.user_id, w.target_type, w.target_id, w.mode, w.recursive, w.frequency, NULL::bigint AS assignment_id FROM watches w
    UNION ALL
    SELECT m.user_id, a.target_type, a.target_id, a.mode, a.recursive, a.frequency, a.id FROM watch_assignments a
    JOIN LATERAL (
        SELECT a.subject_id AS user_id WHERE a.subject_type = 'user'
        UNION SELECT ug.user_id FROM user_groups ug WHERE a.subject_type = 'group' AND ug.group_id = a.subject_id
        UNION SELECT ur.user_id FROM user_roles ur WHERE a.subject_type = 'role' AND ur.role_id = a.subject_id
        UNION SELECT ug.user_id FROM group_roles gr JOIN user_groups ug ON ug.group_id = gr.group_id WHERE a.subject_type = 'role' AND gr.role_id = a.subject_id
    ) m ON true
    WHERE a.mandatory OR NOT EXISTS (SELECT 1 FROM watch_optouts o WHERE o.assignment_id = a.id AND o.user_id = m.user_id);

-- notifications in the wiki (bell); e-mails per kind as set in the profile (users.notify)
CREATE TABLE notifications (
    id          bigserial PRIMARY KEY,
    user_id     bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind        text NOT NULL,
    title       text NOT NULL,
    body        text NOT NULL DEFAULT '',
    link        text NOT NULL DEFAULT '',
    actor_id    bigint REFERENCES users(id) ON DELETE SET NULL,
    entity_type text NOT NULL DEFAULT '',
    entity_id   bigint,
    created_at  timestamptz NOT NULL DEFAULT now(),
    read_at     timestamptz
);
CREATE INDEX notifications_user ON notifications(user_id, created_at DESC);
CREATE INDEX notifications_unread ON notifications(user_id) WHERE read_at IS NULL;

-- ---------------------------------------------------------------- webhooks
-- HTTP notifications about events (Teams, Slack, generic JSON).
CREATE TABLE webhooks (
    id           bigserial PRIMARY KEY,
    name         text NOT NULL,
    url          text NOT NULL,
    format       text NOT NULL DEFAULT 'json' CHECK (format IN ('json', 'slack', 'teams')),
    secret       text NOT NULL DEFAULT '',          -- HMAC-SHA256 signing key (json format)
    events       text[] NOT NULL DEFAULT '{}',      -- e.g. element.update, event.registration; empty = all
    category_id  bigint REFERENCES categories(id) ON DELETE CASCADE, -- only changes below this category
    enabled      boolean NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    last_status  int NOT NULL DEFAULT 0,
    last_error   text NOT NULL DEFAULT '',
    last_at      timestamptz
);

-- Deliveries waiting or retried (oldest first).
CREATE TABLE webhook_deliveries (
    id           bigserial PRIMARY KEY,
    webhook_id   bigint NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event        text NOT NULL,
    payload      jsonb NOT NULL,
    status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'failed')),
    attempts     int NOT NULL DEFAULT 0,
    next_at      timestamptz NOT NULL DEFAULT now(),
    last_status  int NOT NULL DEFAULT 0,
    last_error   text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    delivered_at timestamptz
);
CREATE INDEX webhook_deliveries_due ON webhook_deliveries(next_at) WHERE status = 'pending';
CREATE INDEX webhook_deliveries_hook ON webhook_deliveries(webhook_id, created_at DESC);

-- ---------------------------------------------------------------- system
-- Generation of the guest cache: incremented on every change so all nodes
-- (and changes made by CLI processes) invalidate their in-memory caches.
CREATE TABLE cache_state (
    id          int PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    generation  bigint NOT NULL DEFAULT 0
);
INSERT INTO cache_state DEFAULT VALUES;

-- One-time notifications already sent (e.g. "PostgreSQL 19 recommended",
-- "PostgreSQL 18 support ends in 30 days").
CREATE TABLE system_notices (
    key      text PRIMARY KEY,
    sent_at  timestamptz NOT NULL DEFAULT now()
);

-- one node of a cluster restarts at a time (rolling restart after plugin builds)
CREATE TABLE cluster_lease (
    name   text PRIMARY KEY,
    holder text NOT NULL,
    until  timestamptz NOT NULL
);

-- ---------------------------------------------------------------- demo content
-- everything the demo created (Administration -> Demo), removed without a
-- trace by "remove demo"; kind: category, element, medium, user, group,
-- menu, block, carousel, event, comment, file (ref = hash) ...
CREATE TABLE demo_objects (
    seq        bigserial PRIMARY KEY,
    kind       text NOT NULL,
    ref        text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (kind, ref)
);

