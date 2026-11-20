-- Website templates of data (core.template): the design is checked against
-- limits.json before it is stored. Versions keep every saved design; a
-- template in the trash (deleted_at) can be restored.
CREATE TABLE plugin_templates (
    id          bigserial PRIMARY KEY,
    names       jsonb NOT NULL DEFAULT '{}',      -- name per language
    design      jsonb NOT NULL,
    based_on    text NOT NULL DEFAULT '',         -- the template it was copied from
    -- who may change this template / manage it (rights, trash, own CSS);
    -- empty: everybody with core.template.design. Lists of acl.Subject.
    edit_subjects   jsonb NOT NULL DEFAULT '[]',
    manage_subjects jsonb NOT NULL DEFAULT '[]',
    version     int NOT NULL DEFAULT 1,
    created_by  bigint,
    updated_by  bigint,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz
);
CREATE TABLE plugin_template_versions (
    template_id bigint NOT NULL REFERENCES plugin_templates(id) ON DELETE CASCADE,
    version     int NOT NULL,
    names       jsonb NOT NULL,
    design      jsonb NOT NULL,
    author      bigint,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (template_id, version)
);
-- Uploaded fonts (served unchanged by the wiki itself): the person uploading
-- confirmed the right to use it on the web (audit) and named the license.
-- A font has 1 to 8 files (faces); a font used by a template cannot be removed.
CREATE TABLE plugin_template_fonts (
    id           bigserial PRIMARY KEY,
    name         text NOT NULL,
    license      text NOT NULL CHECK (license IN ('OFL-1.1', 'Apache-2.0', 'MIT', 'other')),
    license_note text NOT NULL DEFAULT '',
    created_by   bigint,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE plugin_template_font_files (
    font_id bigint NOT NULL REFERENCES plugin_template_fonts(id) ON DELETE CASCADE,
    n       int NOT NULL CHECK (n BETWEEN 1 AND 8),
    weight  text NOT NULL CHECK (weight ~ '^[1-9]00( [1-9]00)?$'),
    style   text NOT NULL CHECK (style IN ('normal', 'italic')),
    format  text NOT NULL CHECK (format IN ('woff2', 'woff', 'truetype', 'opentype')),
    data    bytea NOT NULL,
    sha256  text NOT NULL,
    PRIMARY KEY (font_id, n),
    UNIQUE (font_id, weight, style)
);
