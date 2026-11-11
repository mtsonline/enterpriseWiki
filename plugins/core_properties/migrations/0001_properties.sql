-- Definitions of properties: global (category_id NULL) or of a category
-- (inherited by its subcategories; a nearer definition of the same key wins).
CREATE TABLE plugin_prop_defs (
    id            bigserial PRIMARY KEY,
    category_id   bigint REFERENCES categories(id) ON DELETE CASCADE,
    key           text NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]{0,62}$'),
    labels        jsonb NOT NULL DEFAULT '{}',
    help          jsonb NOT NULL DEFAULT '{}',
    type          text NOT NULL,
    options       jsonb NOT NULL DEFAULT '{}',
    required      boolean NOT NULL DEFAULT false,
    default_value jsonb,
    element_types text[] NOT NULL DEFAULT '{}',   -- empty: all types
    grp           text NOT NULL DEFAULT '',        -- group in forms
    position      int NOT NULL DEFAULT 0,
    show_on_page  boolean NOT NULL DEFAULT true,
    view_subjects jsonb NOT NULL DEFAULT '[]',     -- empty: everyone who sees the element
    edit_subjects jsonb NOT NULL DEFAULT '[]',     -- empty: everyone who may edit the element
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX plugin_prop_defs_key ON plugin_prop_defs(COALESCE(category_id, 0), key);

-- Values: one row per element and key with the typed columns of its
-- storage, each indexed per key (filters, sorting and sums in SQL).
CREATE TABLE plugin_prop_values (
    element_id bigint NOT NULL REFERENCES elements(id) ON DELETE CASCADE,
    key        text NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]{0,62}$'),
    type       text NOT NULL,
    def_id     bigint REFERENCES plugin_prop_defs(id) ON DELETE SET NULL, -- NULL: a free property
    label      text NOT NULL DEFAULT '',          -- free properties: their name
    position   int NOT NULL DEFAULT 0,
    raw        jsonb NOT NULL,
    txt        text,
    num        numeric,
    ts         timestamptz,
    ts_end     timestamptz,
    arr        text[],
    refs       bigint[],
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by bigint,
    PRIMARY KEY (element_id, key)
);
CREATE INDEX plugin_prop_values_txt ON plugin_prop_values(key, lower(txt)) WHERE txt IS NOT NULL;
CREATE INDEX plugin_prop_values_num ON plugin_prop_values(key, num) WHERE num IS NOT NULL;
CREATE INDEX plugin_prop_values_ts ON plugin_prop_values(key, ts, ts_end) WHERE ts IS NOT NULL;
CREATE INDEX plugin_prop_values_arr ON plugin_prop_values USING gin(arr) WHERE arr IS NOT NULL;
CREATE INDEX plugin_prop_values_refs ON plugin_prop_values USING gin(refs) WHERE refs IS NOT NULL;
CREATE INDEX plugin_prop_values_def ON plugin_prop_values(def_id);

-- Changes of values (for trends and the history of an element).
CREATE TABLE plugin_prop_history (
    id         bigserial PRIMARY KEY,
    element_id bigint NOT NULL REFERENCES elements(id) ON DELETE CASCADE,
    key        text NOT NULL,
    raw        jsonb,                              -- NULL: removed
    changed_at timestamptz NOT NULL DEFAULT now(),
    changed_by bigint
);
CREATE INDEX plugin_prop_history_element ON plugin_prop_history(element_id, changed_at);
CREATE INDEX plugin_prop_history_key ON plugin_prop_history(key, changed_at);

-- Free properties per category: allow, deny or inherit (global setting "free").
CREATE TABLE plugin_prop_settings (
    category_id bigint PRIMARY KEY REFERENCES categories(id) ON DELETE CASCADE,
    free        text NOT NULL DEFAULT 'inherit' CHECK (free IN ('inherit', 'allow', 'deny'))
);

-- Counters of automatic numbers.
CREATE TABLE plugin_prop_seq (
    def_id bigint PRIMARY KEY REFERENCES plugin_prop_defs(id) ON DELETE CASCADE,
    next   bigint NOT NULL DEFAULT 1
);

-- Saved views of property databases: search, view, grouping, filters and
-- sorting; personal (owner_id) or for all readers of the database (NULL).
CREATE TABLE plugin_prop_views (
    id         bigserial PRIMARY KEY,
    element_id bigint NOT NULL REFERENCES elements(id) ON DELETE CASCADE,
    owner_id   bigint REFERENCES users(id) ON DELETE CASCADE,
    name       text NOT NULL,
    state      jsonb NOT NULL,
    created_by bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX plugin_prop_views_element ON plugin_prop_views(element_id);

-- Subscriptions of reports: the report of a page by e-mail, daily or weekly.
CREATE TABLE plugin_prop_subs (
    id          bigserial PRIMARY KEY,
    user_id     bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    element_id  bigint NOT NULL REFERENCES elements(id) ON DELETE CASCADE,
    category_id bigint NOT NULL,
    params      jsonb NOT NULL,
    title       text NOT NULL DEFAULT '',
    frequency   text NOT NULL CHECK (frequency IN ('daily', 'weekly')),
    next_at     timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX plugin_prop_subs_due ON plugin_prop_subs(next_at);
CREATE INDEX plugin_prop_subs_user ON plugin_prop_subs(user_id);
