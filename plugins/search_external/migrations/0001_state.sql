-- Synchronization state of the external search index.
CREATE TABLE plugin_search_state (
    id         int PRIMARY KEY CHECK (id = 1),
    signature  text NOT NULL DEFAULT '',   -- engine + url + index; a change triggers a rebuild
    last_seq   bigint NOT NULL DEFAULT 0,  -- last processed search_changes.seq
    ready      boolean NOT NULL DEFAULT false,
    last_sync  timestamptz,
    last_error text NOT NULL DEFAULT '',
    indexed    bigint NOT NULL DEFAULT 0   -- documents sent in the last rebuild
);
INSERT INTO plugin_search_state(id) VALUES (1);
