-- Cases (instances) of a workflow element. Each case keeps a snapshot of the
-- process definition it was started with, so later edits of the process do
-- not change running cases.
CREATE TABLE plugin_workflow_instances (
    id          bigserial PRIMARY KEY,
    element_id  bigint NOT NULL REFERENCES elements(id) ON DELETE CASCADE,
    lang        text NOT NULL,
    title       text NOT NULL,
    data        jsonb NOT NULL DEFAULT '{}',
    definition  jsonb NOT NULL,
    step        text NOT NULL,
    status      text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'done', 'cancelled')),
    started_by  bigint REFERENCES users(id) ON DELETE SET NULL,
    started_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    closed_at   timestamptz
);
CREATE INDEX plugin_workflow_instances_element ON plugin_workflow_instances(element_id, started_at DESC);
CREATE INDEX plugin_workflow_instances_open ON plugin_workflow_instances(status) WHERE status = 'open';

-- Every transition of a case (never deleted).
CREATE TABLE plugin_workflow_events (
    id           bigserial PRIMARY KEY,
    instance_id  bigint NOT NULL REFERENCES plugin_workflow_instances(id) ON DELETE CASCADE,
    from_step    text,
    to_step      text,
    action       text NOT NULL,
    action_label text NOT NULL DEFAULT '',
    note         text NOT NULL DEFAULT '',
    actor_id     bigint REFERENCES users(id) ON DELETE SET NULL,
    actor        text NOT NULL,
    at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX plugin_workflow_events_instance ON plugin_workflow_events(instance_id, id);
