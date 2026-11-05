package core_carousel

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/netguard"
)

// External feeds are fetched every 10 minutes (and when a carousel is
// saved). Only public addresses are reached unless the plugin setting
// allow_private is set (netguard); at most 2 MB per feed.

const maxFeedBytes = 2 << 20

type feedItem struct {
	GUID, Title, Link, Summary, Image string
	Published                         *time.Time
}

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
		items, err := fetchFeed(ctx, netguard.Client([]string{f}, allowPrivate, 15*time.Second), f)
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

func fetchFeed(ctx context.Context, client *http.Client, feed string) ([]feedItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml;q=0.9, text/xml;q=0.8")
	req.Header.Set("User-Agent", "EnterpriseWiki-Carousel/1.0")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxFeedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFeedBytes {
		return nil, fmt.Errorf("feed larger than 2 MB")
	}
	return ParseFeed(b, feed)
}

// RSS 2.0 and Atom 1.0 (the parts needed for slides).
type rssDoc struct {
	XMLName xml.Name `xml:"rss"`
	Items   []struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		GUID        string `xml:"guid"`
		Description string `xml:"description"`
		PubDate     string `xml:"pubDate"`
		Enclosure   []struct {
			URL  string `xml:"url,attr"`
			Type string `xml:"type,attr"`
		} `xml:"enclosure"`
		Media []struct {
			URL string `xml:"url,attr"`
		} `xml:"http://search.yahoo.com/mrss/ thumbnail"`
		MediaContent []struct {
			URL    string `xml:"url,attr"`
			Medium string `xml:"medium,attr"`
			Type   string `xml:"type,attr"`
		} `xml:"http://search.yahoo.com/mrss/ content"`
	} `xml:"channel>item"`
}

type atomDoc struct {
	XMLName xml.Name `xml:"http://www.w3.org/2005/Atom feed"`
	Entries []struct {
		Title string `xml:"title"`
		ID    string `xml:"id"`
		Links []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
			Type string `xml:"type,attr"`
		} `xml:"link"`
		Summary   string `xml:"summary"`
		Content   string `xml:"content"`
		Updated   string `xml:"updated"`
		Published string `xml:"published"`
	} `xml:"entry"`
}

// ParseFeed reads an RSS 2.0 or Atom feed. Links and images are resolved
// against the feed address; only http(s) addresses are kept.
func ParseFeed(b []byte, base string) ([]feedItem, error) {
	bu, _ := url.Parse(base)
	abs := func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" || bu == nil {
			return ""
		}
		u, err := bu.Parse(s)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return ""
		}
		return u.String()
	}
	dec := func(v any) error {
		d := xml.NewDecoder(bytes.NewReader(b))
		d.Strict = false
		d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
		return d.Decode(v)
	}
	var out []feedItem
	var rss rssDoc
	if err := dec(&rss); err == nil && rss.XMLName.Local == "rss" {
		for _, it := range rss.Items {
			fi := feedItem{GUID: strings.TrimSpace(it.GUID), Title: clean(it.Title, 200), Link: abs(it.Link), Summary: clean(it.Description, 240)}
			if fi.GUID == "" {
				fi.GUID = fi.Link + "|" + fi.Title
			}
			for _, m := range it.Media {
				if fi.Image == "" {
					fi.Image = abs(m.URL)
				}
			}
			for _, m := range it.MediaContent {
				if fi.Image == "" && (m.Medium == "image" || strings.HasPrefix(m.Type, "image/")) {
					fi.Image = abs(m.URL)
				}
			}
			for _, e := range it.Enclosure {
				if fi.Image == "" && strings.HasPrefix(e.Type, "image/") {
					fi.Image = abs(e.URL)
				}
			}
			if fi.Image == "" {
				fi.Image = abs(firstImage(html.UnescapeString(it.Description)))
			}
			if t, err := parseDate(it.PubDate); err == nil {
				fi.Published = &t
			}
			if fi.Title != "" {
				out = append(out, fi)
			}
		}
		return out, nil
	}
	var atom atomDoc
	if err := dec(&atom); err == nil && atom.XMLName.Local == "feed" {
		for _, e := range atom.Entries {
			fi := feedItem{GUID: strings.TrimSpace(e.ID), Title: clean(e.Title, 200)}
			for _, l := range e.Links {
				switch {
				case (l.Rel == "" || l.Rel == "alternate") && fi.Link == "":
					fi.Link = abs(l.Href)
				case l.Rel == "enclosure" && strings.HasPrefix(l.Type, "image/") && fi.Image == "":
					fi.Image = abs(l.Href)
				}
			}
			sum := e.Summary
			if sum == "" {
				sum = e.Content
			}
			fi.Summary = clean(sum, 240)
			if fi.Image == "" {
				fi.Image = abs(firstImage(html.UnescapeString(e.Content)))
			}
			d := e.Published
			if d == "" {
				d = e.Updated
			}
			if t, err := time.Parse(time.RFC3339, strings.TrimSpace(d)); err == nil {
				fi.Published = &t
			}
			if fi.GUID == "" {
				fi.GUID = fi.Link + "|" + fi.Title
			}
			if fi.Title != "" {
				out = append(out, fi)
			}
		}
		return out, nil
	}
	return nil, errNoFeed
}

// clean turns (possibly HTML) text into plain text of at most max characters.
func clean(s string, max int) string {
	s = html.UnescapeString(s)
	if strings.Contains(s, "<") {
		s = summary(s, max)
	}
	return cut(s, max)
}

func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, f := range []string{time.RFC1123Z, time.RFC1123, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST", "2 Jan 2006 15:04:05 -0700", time.RFC3339} {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unknown date %q", s)
}
