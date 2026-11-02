-- Short URLs (e.g. /kalender) for elements and categories; unique across both
-- tables (checked by the application under an advisory lock).
ALTER TABLE elements ADD COLUMN short_url text;
ALTER TABLE categories ADD COLUMN short_url text;
CREATE UNIQUE INDEX elements_short_url ON elements(short_url) WHERE short_url IS NOT NULL;
CREATE UNIQUE INDEX categories_short_url ON categories(short_url) WHERE short_url IS NOT NULL;
