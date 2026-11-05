-- Properties of elements (key/value with a display type), in their order.
CREATE TABLE plugin_properties (
    element_id bigint NOT NULL REFERENCES elements(id) ON DELETE CASCADE,
    key        text NOT NULL,
    value      text NOT NULL DEFAULT '',
    type       text NOT NULL DEFAULT 'text' CHECK (type IN ('text', 'date', 'number', 'url', 'status', 'user')),
    position   int NOT NULL DEFAULT 0,
    PRIMARY KEY (element_id, key)
);
CREATE INDEX plugin_properties_key ON plugin_properties(key, value);
