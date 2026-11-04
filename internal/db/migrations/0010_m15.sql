-- global permissions of roles (package perm); administrators have all
ALTER TABLE roles ADD COLUMN permissions text[] NOT NULL DEFAULT '{}';
-- global permissions of every signed-in user (NULL = the declared defaults)
CREATE TABLE permission_defaults (
	subject     text PRIMARY KEY CHECK (subject IN ('authenticated')),
	permissions text[] NOT NULL,
	updated_at  timestamptz NOT NULL DEFAULT now()
);
-- named category permissions of plugins (in addition to the bits)
ALTER TABLE acl_rules ADD COLUMN actions text[] NOT NULL DEFAULT '{}';

-- audit log: who (IP address, browser, sign-in method) and searching by area
ALTER TABLE audit_log ADD COLUMN ip text, ADD COLUMN user_agent text, ADD COLUMN via text;
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
