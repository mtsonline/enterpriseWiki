-- Shell commands of the shell macro and of workflow steps and their
-- approval state (internal/shellcmd).
-- A command is identified by the SHA-256 of its exact text.
CREATE TABLE plugin_shell_commands (
    hash          text PRIMARY KEY,
    command       text NOT NULL,
    status        text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'revoked')),
    element_id    bigint,
    -- where the command is used: "page" (macro in element_id) or
    -- "workflow:<step id>" (step of the workflow element_id)
    source        text NOT NULL DEFAULT '',
    requested_by  bigint REFERENCES users(id) ON DELETE SET NULL,
    requested_at  timestamptz NOT NULL DEFAULT now(),
    decided_by    text,
    decided_at    timestamptz,
    last_run_at   timestamptz,
    last_exit     int,
    last_ms       int
);
CREATE INDEX plugin_shell_commands_status ON plugin_shell_commands(status);
