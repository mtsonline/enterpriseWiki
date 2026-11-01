-- Step types: timers, reminders, webhooks.
ALTER TABLE plugin_workflow_instances ADD COLUMN step_entered_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE plugin_workflow_instances ADD COLUMN wake_at timestamptz;
ALTER TABLE plugin_workflow_instances ADD COLUMN reminded_at timestamptz;
ALTER TABLE plugin_workflow_instances ADD COLUMN attempts int NOT NULL DEFAULT 0;
ALTER TABLE plugin_workflow_instances ADD COLUMN last_error text NOT NULL DEFAULT '';
CREATE INDEX plugin_workflow_instances_wake ON plugin_workflow_instances(wake_at) WHERE status = 'open' AND wake_at IS NOT NULL;
-- votes of approval steps are events with from_step = to_step
CREATE INDEX plugin_workflow_events_votes ON plugin_workflow_events(instance_id, from_step, action);
