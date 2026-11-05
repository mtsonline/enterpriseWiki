-- Emoji reactions of users on pages, categories and comments.
CREATE TABLE plugin_reactions (
    target_type text NOT NULL CHECK (target_type IN ('element', 'category', 'comment')),
    target_id   bigint NOT NULL,
    user_id     bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    emoji       text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (target_type, target_id, user_id, emoji)
);
CREATE INDEX plugin_reactions_target ON plugin_reactions(target_type, target_id, created_at);
CREATE INDEX plugin_reactions_user ON plugin_reactions(user_id);
