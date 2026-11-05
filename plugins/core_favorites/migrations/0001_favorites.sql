-- Personal favorites (pages and categories), in the order the user chose.
CREATE TABLE plugin_favorites (
    user_id     bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type text NOT NULL CHECK (target_type IN ('element', 'category')),
    target_id   bigint NOT NULL,
    position    int NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, target_type, target_id)
);
CREATE INDEX plugin_favorites_target ON plugin_favorites(target_type, target_id);
