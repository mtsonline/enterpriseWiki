-- Calendar invitations (iTIP) as text/calendar part of an e-mail.
ALTER TABLE mail_outbox ADD COLUMN body_calendar text NOT NULL DEFAULT '';
-- Preview image (og:image) of an element for link previews (file hash; empty = automatic).
ALTER TABLE elements ADD COLUMN preview_image text NOT NULL DEFAULT '';
