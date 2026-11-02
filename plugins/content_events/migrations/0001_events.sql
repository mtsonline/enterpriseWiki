-- Events of calendar elements (element type content.events).
CREATE TABLE plugin_events (
    id              bigserial PRIMARY KEY,
    calendar_id     bigint NOT NULL REFERENCES elements(id) ON DELETE CASCADE,
    uid             text NOT NULL UNIQUE,              -- iCalendar UID
    sequence        int NOT NULL DEFAULT 0,            -- iCalendar SEQUENCE (incremented on changes)
    title           text NOT NULL,
    description     text NOT NULL DEFAULT '',          -- Markdown
    location        text NOT NULL DEFAULT '',
    url             text NOT NULL DEFAULT '',
    category        text NOT NULL DEFAULT '',          -- id of a calendar category
    starts_at       timestamptz NOT NULL,
    ends_at         timestamptz NOT NULL,
    all_day         boolean NOT NULL DEFAULT false,
    tz              text NOT NULL DEFAULT 'UTC',       -- IANA time zone of the wall time
    rrule           text NOT NULL DEFAULT '',          -- recurrence (RRULE subset)
    exdates         date[] NOT NULL DEFAULT '{}',      -- cancelled occurrences (local dates)
    registration    boolean NOT NULL DEFAULT false,
    capacity        int NOT NULL DEFAULT 0,            -- 0 = unlimited
    waitlist        boolean NOT NULL DEFAULT true,
    reg_deadline    int NOT NULL DEFAULT 0,            -- minutes before start (0 = until start)
    reminders       int[] NOT NULL DEFAULT '{}',       -- minutes before start
    created_by      bigint REFERENCES users(id) ON DELETE SET NULL,
    updated_by      bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CHECK (ends_at >= starts_at)
);
CREATE INDEX plugin_events_calendar ON plugin_events(calendar_id, starts_at);

-- Registrations per occurrence (for single events: the start date).
-- status: registered | waitlist | interested (reminders only) | cancelled
CREATE TABLE plugin_event_registrations (
    id          bigserial PRIMARY KEY,
    event_id    bigint NOT NULL REFERENCES plugin_events(id) ON DELETE CASCADE,
    occurrence  date NOT NULL,
    user_id     bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status      text NOT NULL CHECK (status IN ('registered', 'waitlist', 'interested', 'cancelled')),
    note        text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (event_id, occurrence, user_id)
);
CREATE INDEX plugin_event_registrations_user ON plugin_event_registrations(user_id);

-- Reminders already queued (never twice).
CREATE TABLE plugin_event_reminders_sent (
    event_id    bigint NOT NULL REFERENCES plugin_events(id) ON DELETE CASCADE,
    occurrence  date NOT NULL,
    user_id     bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    minutes     int NOT NULL,
    sent_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, occurrence, user_id, minutes)
);
