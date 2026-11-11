-- Carousels placed at positions of the template.
CREATE TABLE plugin_carousels (
    id          bigserial PRIMARY KEY,
    name        text NOT NULL,
    position    text NOT NULL DEFAULT 'content-top',
    audience    text NOT NULL DEFAULT 'all' CHECK (audience IN ('all', 'guests', 'users')),
    autoplay    int NOT NULL DEFAULT 0,         -- seconds per slide, 0 = no automatic rotation
    max_slides  int NOT NULL DEFAULT 10,
    feeds       text[] NOT NULL DEFAULT '{}',   -- external RSS/Atom feeds
    feed_items  int NOT NULL DEFAULT 5,         -- items per feed (and per source)
    sources     text NOT NULL DEFAULT '',       -- elements as slides: content sources ("category:12+sub; tag:news")
    enabled     boolean NOT NULL DEFAULT true,
    position_order int NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Slides: own ones (manual) and news (a published page).
CREATE TABLE plugin_carousel_slides (
    id          bigserial PRIMARY KEY,
    carousel_id bigint NOT NULL REFERENCES plugin_carousels(id) ON DELETE CASCADE,
    kind        text NOT NULL CHECK (kind IN ('manual', 'news')),
    element_id  bigint REFERENCES elements(id) ON DELETE CASCADE,
    title       text NOT NULL DEFAULT '',
    text        text NOT NULL DEFAULT '',
    link        text NOT NULL DEFAULT '',
    image       text NOT NULL DEFAULT '',
    image_alt   text NOT NULL DEFAULT '',
    starts_at   timestamptz,
    ends_at     timestamptz,
    position    int NOT NULL DEFAULT 0,
    created_by  bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX plugin_carousel_slides_carousel ON plugin_carousel_slides(carousel_id, position);
CREATE UNIQUE INDEX plugin_carousel_slides_news ON plugin_carousel_slides(carousel_id, element_id) WHERE kind = 'news';

-- Items of external feeds (fetched in the background).
CREATE TABLE plugin_carousel_feed_items (
    carousel_id  bigint NOT NULL REFERENCES plugin_carousels(id) ON DELETE CASCADE,
    feed         text NOT NULL,
    guid         text NOT NULL,
    title        text NOT NULL,
    link         text NOT NULL DEFAULT '',
    summary      text NOT NULL DEFAULT '',
    image        text NOT NULL DEFAULT '',
    published_at timestamptz,
    fetched_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (carousel_id, feed, guid)
);
CREATE TABLE plugin_carousel_feed_state (
    carousel_id bigint NOT NULL REFERENCES plugin_carousels(id) ON DELETE CASCADE,
    feed        text NOT NULL,
    fetched_at  timestamptz,
    error       text NOT NULL DEFAULT '',
    PRIMARY KEY (carousel_id, feed)
);
