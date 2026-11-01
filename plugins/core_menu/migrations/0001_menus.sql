CREATE TABLE plugin_menus (
    id         bigserial PRIMARY KEY,
    name       text NOT NULL,
    location   text NOT NULL,          -- template position, e.g. header, sidebar, footer
    position   int NOT NULL DEFAULT 0,
    items      jsonb NOT NULL DEFAULT '[]',
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX plugin_menus_location ON plugin_menus(location, position);
