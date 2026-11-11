package core_carousel

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/feedfetch"
)

// External feeds are fetched every 10 minutes (and when a carousel is
// saved). Only public addresses are reached unless the plugin setting
// allow_private is set (netguard); at most 2 MB per feed.

func (p *Plugin) fetchAll(ctx context.Context) error {
	rows, err := p.host.DB.Query(ctx, `SELECT `+carCols+` FROM plugin_carousels WHERE enabled AND cardinality(feeds) > 0`)
	if err != nil {
		return err
	}
	cars, err := pgx.CollectRows(rows, scanCarousel)
	if err != nil {
		return err
	}
	for _, c := range cars {
		if err := p.fetchCarousel(ctx, c); err != nil {
			p.host.Log.Warn("carousel feeds", "carousel", c.ID, "err", err)
		}
	}
	return nil
}

func (p *Plugin) fetchCarousel(ctx context.Context, c Carousel) error {
	allowPrivate, _ := p.host.Settings()["allow_private"].(bool)
	var firstErr error
	for _, f := range c.Feeds {
		items, err := feedfetch.Fetch(ctx, f, allowPrivate)
		msg := ""
		if err != nil {
			msg = err.Error()
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", f, err)
			}
		}
		err = pgx.BeginFunc(ctx, p.host.DB, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `INSERT INTO plugin_carousel_feed_state(carousel_id, feed, fetched_at, error) VALUES ($1,$2,now(),$3)
				ON CONFLICT (carousel_id, feed) DO UPDATE SET fetched_at=now(), error=EXCLUDED.error`, c.ID, f, msg); err != nil {
				return err
			}
			if msg != "" {
				return nil // keep the old items
			}
			if _, err := tx.Exec(ctx, `DELETE FROM plugin_carousel_feed_items WHERE carousel_id=$1 AND feed=$2`, c.ID, f); err != nil {
				return err
			}
			for i, it := range items {
				if i >= 20 {
					break
				}
				if _, err := tx.Exec(ctx, `INSERT INTO plugin_carousel_feed_items(carousel_id, feed, guid, title, link, summary, image, published_at)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, c.ID, f, it.GUID, it.Title, it.Link, it.Summary, it.Image, it.Published); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return firstErr
}
