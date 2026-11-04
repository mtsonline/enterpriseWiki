-- who was already notified about a mention in an element or comment (no
-- second notification when the text is saved again)
CREATE TABLE plugin_mentions (
	entity_type text NOT NULL,
	entity_id   bigint NOT NULL,
	user_id     bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at  timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (entity_type, entity_id, user_id)
);
