// Package feedfetch reads external RSS/Atom feeds for reading plugins and
// macros (carousel, news, lists): parsing, fetching with the SSRF
// protection of netguard (public addresses only unless feeds.allow_private
// is set), at most 2 MB, and a cache per site that a worker refreshes every
// 10 minutes for feeds in use. Rendering never waits for the network: a new
// feed is fetched in the background and shown at the next view.
package feedfetch

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	nethtml "golang.org/x/net/html"

	"github.com/mtsonline/enterprisewiki/internal/netguard"
)

// Item is an entry of a feed.
type Item struct {
	GUID      string     `json:"guid"`
	Title     string     `json:"title"`
	Link      string     `json:"link"`
	Summary   string     `json:"summary"`
	Image     string     `json:"image"`
	Published *time.Time `json:"published,omitempty"`
}

// ErrNoFeed: the document is neither RSS nor Atom.
var ErrNoFeed = errors.New("not an RSS or Atom feed")

// MaxBytes is the largest feed read.
const MaxBytes = 2 << 20

// MaxItems is the most items kept per feed.
const MaxItems = 30

// Fetch reads a feed with an SSRF-safe client.
func Fetch(ctx context.Context, feed string, allowPrivate bool) ([]Item, error) {
	if err := CheckURL(feed); err != nil {
		return nil, err
	}
	client := netguard.Client([]string{feed}, allowPrivate, 15*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml;q=0.9, text/xml;q=0.8")
	req.Header.Set("User-Agent", "EnterpriseWiki-Feeds/1.0")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxBytes {
		return nil, fmt.Errorf("feed larger than 2 MB")
	}
	items, err := Parse(b, feed)
	if len(items) > MaxItems {
		items = items[:MaxItems]
	}
	return items, err
}

// CheckURL accepts absolute http(s) addresses without user information.
func CheckURL(s string) error {
	if len(s) > 2000 {
		return errors.New("feed address too long")
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return errors.New("a feed needs an http(s) address")
	}
	return nil
}

// Cache keeps the items of feeds in use (table feed_cache of the site).
type Cache struct {
	pool         *pgxpool.Pool
	log          *slog.Logger
	allowPrivate func() bool
	mu           sync.Mutex
	running      map[string]bool
}

// NewCache returns the cache of a site.
func NewCache(pool *pgxpool.Pool, log *slog.Logger, allowPrivate func() bool) *Cache {
	return &Cache{pool: pool, log: log, allowPrivate: allowPrivate, running: map[string]bool{}}
}

const staleAfter = 10 * time.Minute

// Items returns the cached items of a feed (newest first, at most limit)
// and marks it as in use; a missing or old feed is fetched in the
// background.
func (c *Cache) Items(ctx context.Context, feed string, limit int) ([]Item, error) {
	if err := CheckURL(feed); err != nil {
		return nil, err
	}
	var raw []byte
	var fetched *time.Time
	err := c.pool.QueryRow(ctx, `INSERT INTO feed_cache(url, kind) VALUES ($1, 'rss') ON CONFLICT (kind, url) DO UPDATE SET used_at = now()
		RETURNING items, fetched_at`, feed).Scan(&raw, &fetched)
	if err != nil {
		return nil, err
	}
	if fetched == nil || time.Since(*fetched) > staleAfter {
		c.refreshAsync("rss", feed)
	}
	var items []Item
	_ = json.Unmarshal(raw, &items)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (c *Cache) refreshAsync(kind, feed string) {
	key := kind + " " + feed
	c.mu.Lock()
	if c.running[key] || len(c.running) > 20 {
		c.mu.Unlock()
		return
	}
	c.running[key] = true
	c.mu.Unlock()
	go func() {
		defer func() { c.mu.Lock(); delete(c.running, key); c.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := c.Refresh(ctx, kind, feed); err != nil {
			c.log.Info("feed", "url", feed, "err", err)
		}
	}()
}

// Calendar returns the cached iCalendar document of an address (empty
// until fetched in the background) and marks it as in use.
func (c *Cache) Calendar(ctx context.Context, address string) (string, *time.Time, error) {
	if err := CheckURL(address); err != nil {
		return "", nil, err
	}
	var body string
	var fetched *time.Time
	err := c.pool.QueryRow(ctx, `INSERT INTO feed_cache(url, kind) VALUES ($1, 'ical') ON CONFLICT (kind, url) DO UPDATE SET used_at = now()
		RETURNING body, fetched_at`, address).Scan(&body, &fetched)
	if err != nil {
		return "", nil, err
	}
	if fetched == nil || time.Since(*fetched) > staleAfter {
		c.refreshAsync("ical", address)
	}
	return body, fetched, nil
}

// FetchCalendar reads an iCalendar document with an SSRF-safe client.
func FetchCalendar(ctx context.Context, address string, allowPrivate bool) (string, error) {
	if err := CheckURL(address); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/calendar, */*;q=0.5")
	req.Header.Set("User-Agent", "EnterpriseWiki-Feeds/1.0")
	res, err := netguard.Client([]string{address}, allowPrivate, 15*time.Second).Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, MaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(b) > MaxBytes {
		return "", fmt.Errorf("calendar larger than 2 MB")
	}
	if !bytes.Contains(b[:min(len(b), 512)], []byte("BEGIN:VCALENDAR")) || !utf8.Valid(b) {
		return "", errors.New("not an iCalendar document")
	}
	return string(b), nil
}

// Refresh fetches a feed ("rss") or calendar ("ical") now and stores it
// (the old content stays on errors).
func (c *Cache) Refresh(ctx context.Context, kind, feed string) error {
	if kind == "ical" {
		body, err := FetchCalendar(ctx, feed, c.allowPrivate())
		if err != nil {
			_, _ = c.pool.Exec(ctx, `UPDATE feed_cache SET fetched_at = now(), error = $2 WHERE kind = 'ical' AND url = $1`, feed, err.Error())
			return err
		}
		_, err = c.pool.Exec(ctx, `UPDATE feed_cache SET body = $2, fetched_at = now(), error = '' WHERE kind = 'ical' AND url = $1`, feed, body)
		return err
	}
	items, err := Fetch(ctx, feed, c.allowPrivate())
	if err != nil {
		_, _ = c.pool.Exec(ctx, `UPDATE feed_cache SET fetched_at = now(), error = $2 WHERE kind = 'rss' AND url = $1`, feed, err.Error())
		return err
	}
	b, _ := json.Marshal(items)
	_, err = c.pool.Exec(ctx, `UPDATE feed_cache SET items = $2, fetched_at = now(), error = '' WHERE kind = 'rss' AND url = $1`, feed, b)
	return err
}

// RefreshAll refreshes the feeds used in the last week and forgets those
// unused for 30 days (worker).
func (c *Cache) RefreshAll(ctx context.Context) error {
	if _, err := c.pool.Exec(ctx, `DELETE FROM feed_cache WHERE used_at < now() - interval '30 days'`); err != nil {
		return err
	}
	rows, err := c.pool.Query(ctx, `SELECT kind, url FROM feed_cache WHERE used_at > now() - interval '7 days'
		AND (fetched_at IS NULL OR fetched_at < now() - interval '9 minutes') ORDER BY fetched_at NULLS FIRST LIMIT 200`)
	if err != nil {
		return err
	}
	var urls [][2]string
	for rows.Next() {
		var k, u string
		if err := rows.Scan(&k, &u); err == nil {
			urls = append(urls, [2]string{k, u})
		}
	}
	rows.Close()
	for _, x := range urls {
		k, u := x[0], x[1]
		if err := c.Refresh(ctx, k, u); err != nil {
			c.log.Info("feed", "url", u, "err", err)
		}
	}
	return nil
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

// Parse reads an RSS 2.0 or Atom feed. Links and images are resolved
// against the feed address; only http(s) addresses are kept.
func Parse(b []byte, base string) ([]Item, error) {
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
	var out []Item
	var rss rssDoc
	if err := dec(&rss); err == nil && rss.XMLName.Local == "rss" {
		for _, it := range rss.Items {
			fi := Item{GUID: strings.TrimSpace(it.GUID), Title: clean(it.Title, 200), Link: abs(it.Link), Summary: clean(it.Description, 240)}
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
				fi.Image = abs(FirstImage(html.UnescapeString(it.Description)))
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
			fi := Item{GUID: strings.TrimSpace(e.ID), Title: clean(e.Title, 200)}
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
				fi.Image = abs(FirstImage(html.UnescapeString(e.Content)))
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
	return nil, ErrNoFeed
}

// clean turns (possibly HTML) text into plain text of at most max characters.
func clean(s string, max int) string {
	s = html.UnescapeString(s)
	if strings.Contains(s, "<") {
		s = Summary(s, max)
	}
	return Cut(s, max)
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

// summary returns the first characters of the text of an HTML fragment.
func Summary(src string, max int) string {
	var b strings.Builder
	z := nethtml.NewTokenizer(strings.NewReader(src))
	skip := 0
	for {
		tt := z.Next()
		switch tt {
		case nethtml.ErrorToken:
			return Cut(b.String(), max)
		case nethtml.StartTagToken:
			if n, _ := z.TagName(); string(n) == "script" || string(n) == "style" || string(n) == "nav" {
				skip++
			}
		case nethtml.EndTagToken:
			if n, _ := z.TagName(); (string(n) == "script" || string(n) == "style" || string(n) == "nav") && skip > 0 {
				skip--
			}
			b.WriteByte(' ')
		case nethtml.TextToken:
			if skip == 0 {
				b.Write(z.Text())
			}
		}
		if b.Len() > max*4 {
			return Cut(b.String(), max)
		}
	}
}

func Cut(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)[:max]
	if i := strings.LastIndexByte(string(r), ' '); i > max/2 {
		return string(r)[:i] + " …"
	}
	return string(r) + " …"
}

var imgRe = regexp.MustCompile(`(?i)<img\b[^>]*?\ssrc="([^"]+)"`)

func FirstImage(src string) string {
	if m := imgRe.FindStringSubmatch(src); m != nil && (strings.HasPrefix(m[1], "/") || strings.HasPrefix(m[1], "https://")) {
		return m[1]
	}
	return ""
}
