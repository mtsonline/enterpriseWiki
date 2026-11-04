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
