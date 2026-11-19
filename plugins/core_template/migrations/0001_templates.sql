-- Website templates of data (core.template): the design is checked against
-- limits.json before it is stored. Versions keep every saved design; a
-- template in the trash (deleted_at) can be restored.
CREATE TABLE plugin_templates (
    id          bigserial PRIMARY KEY,
    names       jsonb NOT NULL DEFAULT '{}',      -- name per language
    design      jsonb NOT NULL,
    based_on    text NOT NULL DEFAULT '',         -- the template it was copied from
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
