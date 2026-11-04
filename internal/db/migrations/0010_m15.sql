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
	tags            text[] NOT NULL DEFAULT '{}',
	comment         text NOT NULL DEFAULT '',
	base_rev        int NOT NULL DEFAULT 0,
	created_by      bigint REFERENCES users(id) ON DELETE SET NULL,
	updated_by      bigint REFERENCES users(id) ON DELETE SET NULL,
	created_at      timestamptz NOT NULL DEFAULT now(),
	updated_at      timestamptz NOT NULL DEFAULT now(),
	publish_at      timestamptz, -- scheduled publication
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
