CREATE TABLE plugin_comments (
    id          bigserial PRIMARY KEY,
    element_id  bigint NOT NULL REFERENCES elements(id) ON DELETE CASCADE,
    parent_id   bigint REFERENCES plugin_comments(id) ON DELETE CASCADE,
    author_id   bigint REFERENCES users(id) ON DELETE SET NULL,
    author      text NOT NULL,
    body        text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    edited      boolean NOT NULL DEFAULT false,
    deleted_at  timestamptz,
    deleted_by  text
);
CREATE INDEX plugin_comments_element ON plugin_comments(element_id, id);
