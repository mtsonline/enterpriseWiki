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
    closed_at   timestamptz,
    -- step types: timers, reminders, webhooks
    step_entered_at timestamptz NOT NULL DEFAULT now(),
    wake_at         timestamptz,
    reminded_at     timestamptz,
    attempts        int NOT NULL DEFAULT 0,
    last_error      text NOT NULL DEFAULT '',
    -- automatic steps act with the rights of the person who saved the
    -- version of the workflow the case was started with (or of the starter)
    definer_id      bigint REFERENCES users(id) ON DELETE SET NULL,
    -- what started the case (trigger, event, object), flat text values
    trigger         jsonb NOT NULL DEFAULT '{}',
    -- chains of workflows started by other workflows end after a few links
    depth           int NOT NULL DEFAULT 0,
    -- sub-workflow: the waiting case and its step
    parent_id       bigint REFERENCES plugin_workflow_instances(id) ON DELETE SET NULL,
    parent_step     text NOT NULL DEFAULT '',
    -- deadline of the current step, warnings sent, escalation done
    due_at          timestamptz,
    warned          int NOT NULL DEFAULT 0,
    escalated       boolean NOT NULL DEFAULT false,
    check_at        timestamptz,        -- next warning or the deadline
    -- assignee of the current step after an escalation (reassignment)
    override        jsonb
);
CREATE INDEX plugin_workflow_instances_element ON plugin_workflow_instances(element_id, started_at DESC);
CREATE INDEX plugin_workflow_instances_open ON plugin_workflow_instances(status) WHERE status = 'open';
CREATE INDEX plugin_workflow_instances_wake ON plugin_workflow_instances(wake_at) WHERE status = 'open' AND wake_at IS NOT NULL;
CREATE INDEX plugin_workflow_instances_check ON plugin_workflow_instances(check_at) WHERE status = 'open' AND check_at IS NOT NULL;
CREATE INDEX plugin_workflow_instances_parent ON plugin_workflow_instances(parent_id) WHERE parent_id IS NOT NULL;

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
-- votes of approval steps are events with from_step = to_step
CREATE INDEX plugin_workflow_events_votes ON plugin_workflow_events(instance_id, from_step, action);

-- Triggers of the workflows (copied from the current version of each
-- workflow when it is saved): which events start cases.
CREATE TABLE plugin_workflow_triggers (
    element_id  bigint NOT NULL REFERENCES elements(id) ON DELETE CASCADE,
    trigger_id  text NOT NULL,
    type        text NOT NULL,          -- schedule | content | event | webhook
    events      text[] NOT NULL DEFAULT '{}',
    spec        jsonb NOT NULL,
    PRIMARY KEY (element_id, trigger_id)
);
CREATE INDEX plugin_workflow_triggers_events ON plugin_workflow_triggers USING gin(events);

-- Events that may start cases, written with the change itself and handled
-- by the worker (rights, sources and conditions are checked there).
CREATE TABLE plugin_workflow_queue (
    id          bigserial PRIMARY KEY,
    element_id  bigint NOT NULL REFERENCES elements(id) ON DELETE CASCADE,
    trigger_id  text NOT NULL,
    event       jsonb NOT NULL,
    actor_id    bigint REFERENCES users(id) ON DELETE SET NULL,
    depth       int NOT NULL DEFAULT 0,
    attempts    int NOT NULL DEFAULT 0,
    next_at     timestamptz NOT NULL DEFAULT now(),
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX plugin_workflow_queue_due ON plugin_workflow_queue(next_at);

-- Starts that happened (a scheduled time, a change of a property, a
-- delivery of a webhook): nothing starts twice.
CREATE TABLE plugin_workflow_fired (
    element_id  bigint NOT NULL REFERENCES elements(id) ON DELETE CASCADE,
    trigger_id  text NOT NULL,
    ref         text NOT NULL,
    at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (element_id, trigger_id, ref)
);
CREATE INDEX plugin_workflow_fired_at ON plugin_workflow_fired(at);

-- Secrets of incoming webhooks: never part of the workflow (whoever may
-- edit the page would read them), shown once when generated.
CREATE TABLE plugin_workflow_hook_secrets (
    element_id  bigint NOT NULL REFERENCES elements(id) ON DELETE CASCADE,
    trigger_id  text NOT NULL,
    secret      bytea NOT NULL,
    created_by  bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (element_id, trigger_id)
);
