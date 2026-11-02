-- Changed single occurrences of a series, keyed by the original local date
-- ({"2026-05-04": {"title": "…", "location": "…", "starts_at": "…", "ends_at": "…"}}).
ALTER TABLE plugin_events ADD COLUMN overrides jsonb NOT NULL DEFAULT '{}';
