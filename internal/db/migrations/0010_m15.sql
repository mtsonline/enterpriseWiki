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

-- notifications in the wiki (bell); e-mails per kind as set in the profile (users.notify)
CREATE TABLE notifications (
	id          bigserial PRIMARY KEY,
	user_id     bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind        text NOT NULL,
	title       text NOT NULL,
	body        text NOT NULL DEFAULT '',
	link        text NOT NULL DEFAULT '',
	actor_id    bigint REFERENCES users(id) ON DELETE SET NULL,
	entity_type text NOT NULL DEFAULT '',
	entity_id   bigint,
	created_at  timestamptz NOT NULL DEFAULT now(),
	read_at     timestamptz
);
CREATE INDEX notifications_user ON notifications(user_id, created_at DESC);
CREATE INDEX notifications_unread ON notifications(user_id) WHERE read_at IS NULL;

-- watches assigned centrally to users, groups or roles (dynamic: later
-- members watch too); optional ones can be unsubscribed per user
CREATE TABLE watch_assignments (
	id           bigserial PRIMARY KEY,
	subject_type text NOT NULL CHECK (subject_type IN ('user', 'group', 'role')),
	subject_id   bigint NOT NULL,
	target_type  text NOT NULL CHECK (target_type IN ('category', 'element')),
	target_id    bigint NOT NULL,
	mode         text NOT NULL DEFAULT 'link' CHECK (mode IN ('link', 'content')),
	recursive    boolean NOT NULL DEFAULT true,
	frequency    text NOT NULL DEFAULT 'instant' CHECK (frequency IN ('instant', 'daily', 'weekly')),
	mandatory    boolean NOT NULL DEFAULT false,
	note         text NOT NULL DEFAULT '',
	created_by   bigint REFERENCES users(id) ON DELETE SET NULL,
	created_at   timestamptz NOT NULL DEFAULT now(),
	UNIQUE (subject_type, subject_id, target_type, target_id)
);
CREATE TABLE watch_optouts (
	assignment_id bigint NOT NULL REFERENCES watch_assignments(id) ON DELETE CASCADE,
	user_id       bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	PRIMARY KEY (assignment_id, user_id)
);
-- own watches plus assigned ones (without unsubscribed optional ones)
CREATE VIEW watch_effective AS
	SELECT w.user_id, w.target_type, w.target_id, w.mode, w.recursive, w.frequency, NULL::bigint AS assignment_id FROM watches w
	UNION ALL
	SELECT m.user_id, a.target_type, a.target_id, a.mode, a.recursive, a.frequency, a.id FROM watch_assignments a
	JOIN LATERAL (
		SELECT a.subject_id AS user_id WHERE a.subject_type = 'user'
		UNION SELECT ug.user_id FROM user_groups ug WHERE a.subject_type = 'group' AND ug.group_id = a.subject_id
		UNION SELECT ur.user_id FROM user_roles ur WHERE a.subject_type = 'role' AND ur.role_id = a.subject_id
		UNION SELECT ug.user_id FROM group_roles gr JOIN user_groups ug ON ug.group_id = gr.group_id WHERE a.subject_type = 'role' AND gr.role_id = a.subject_id
	) m ON true
	WHERE a.mandatory OR NOT EXISTS (SELECT 1 FROM watch_optouts o WHERE o.assignment_id = a.id AND o.user_id = m.user_id);
