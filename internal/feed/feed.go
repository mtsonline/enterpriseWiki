// Package feed writes Atom 1.0 and RSS 2.0 feeds. Plugins and the core
// build a Feed and call Write; conditional requests (If-None-Match,
// If-Modified-Since) are answered with 304 so feed readers poll cheaply.
package feed

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Feed is a feed independent of the format.
type Feed struct {
	ID       string // unique, permanent (a URL or tag: URI)
	Title    string
	Subtitle string
	Link     string // HTML page of the feed
	Self     string // URL of the feed itself
	Lang     string
	Author   string
	Updated  time.Time
	Entries  []Entry
}

// Entry is an item of a feed.
type Entry struct {
	ID         string
	Title      string
	Link       string
	Published  time.Time
	Updated    time.Time
	Author     string
	Summary    string // plain text
	Content    string // HTML (optional)
	Categories []string
}

// Formats lists the supported formats.
var Formats = []string{"atom", "rss"}

// ---------------------------------------------------------------- Atom

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
	Type string `xml:"type,attr,omitempty"`
}
type atomText struct {
	Type string `xml:"type,attr,omitempty"`
	Body string `xml:",chardata"`
}
type atomPerson struct {
	Name string `xml:"name"`
}
type atomCategory struct {
	Term string `xml:"term,attr"`
}
type atomEntry struct {
	ID         string         `xml:"id"`
	Title      string         `xml:"title"`
	Link       []atomLink     `xml:"link"`
	Published  string         `xml:"published,omitempty"`
	Updated    string         `xml:"updated"`
	Author     *atomPerson    `xml:"author,omitempty"`
	Summary    *atomText      `xml:"summary,omitempty"`
	Content    *atomText      `xml:"content,omitempty"`
	Categories []atomCategory `xml:"category"`
}
type atomFeed struct {
	XMLName  xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Lang     string      `xml:"xml:lang,attr,omitempty"`
	ID       string      `xml:"id"`
	Title    string      `xml:"title"`
	Subtitle string      `xml:"subtitle,omitempty"`
	Updated  string      `xml:"updated"`
	Link     []atomLink  `xml:"link"`
	Author   *atomPerson `xml:"author,omitempty"`
	Gen      string      `xml:"generator"`
	Entries  []atomEntry `xml:"entry"`
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Atom renders the feed as Atom 1.0.
func (f Feed) Atom() ([]byte, error) {
	a := atomFeed{Lang: f.Lang, ID: f.ID, Title: f.Title, Subtitle: f.Subtitle, Updated: rfc3339(f.updated()), Gen: "Enterprise Wiki",
		Link: []atomLink{{Href: f.Link, Rel: "alternate", Type: "text/html"}, {Href: f.Self, Rel: "self", Type: "application/atom+xml"}}}
	if f.Author != "" {
		a.Author = &atomPerson{Name: f.Author}
	}
	for _, e := range f.Entries {
		ae := atomEntry{ID: e.ID, Title: e.Title, Link: []atomLink{{Href: e.Link, Rel: "alternate", Type: "text/html"}}, Updated: rfc3339(e.Updated)}
		if !e.Published.IsZero() {
			ae.Published = rfc3339(e.Published)
		}
		// Atom requires an author per entry or for the feed
		name := e.Author
		if name == "" {
			name = f.Author
		}
		if name == "" {
			name = f.Title
		}
		ae.Author = &atomPerson{Name: name}
		if e.Summary != "" {
			ae.Summary = &atomText{Type: "text", Body: e.Summary}
		}
		if e.Content != "" {
			ae.Content = &atomText{Type: "html", Body: e.Content}
		}
		for _, c := range e.Categories {
			ae.Categories = append(ae.Categories, atomCategory{Term: c})
		}
		a.Entries = append(a.Entries, ae)
	}
	return marshal(a)
}

// ---------------------------------------------------------------- RSS

type rssGUID struct {
	Perma string `xml:"isPermaLink,attr"`
	Value string `xml:",chardata"`
}
type rssItem struct {
	Title      string   `xml:"title"`
	Link       string   `xml:"link"`
	GUID       rssGUID  `xml:"guid"`
	PubDate    string   `xml:"pubDate"`
	Author     string   `xml:"dc:creator,omitempty"`
	Desc       string   `xml:"description,omitempty"`
	Content    *cdata   `xml:"content:encoded,omitempty"`
	Categories []string `xml:"category"`
}
type cdata struct {
	Body string `xml:",cdata"`
}
type rssAtomLink struct {
	XMLName xml.Name `xml:"atom:link"`
	Href    string   `xml:"href,attr"`
	Rel     string   `xml:"rel,attr"`
	Type    string   `xml:"type,attr"`
}
type rssChannel struct {
	Title   string      `xml:"title"`
	Link    string      `xml:"link"`
	Desc    string      `xml:"description"`
	Lang    string      `xml:"language,omitempty"`
	Updated string      `xml:"lastBuildDate"`
	Gen     string      `xml:"generator"`
	Self    rssAtomLink `xml:"atom:link"`
	Items   []rssItem   `xml:"item"`
}
type rss struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Atom    string     `xml:"xmlns:atom,attr"`
	Content string     `xml:"xmlns:content,attr"`
	DC      string     `xml:"xmlns:dc,attr"`
	Channel rssChannel `xml:"channel"`
}

// RSS renders the feed as RSS 2.0 (with content:encoded for the HTML).
func (f Feed) RSS() ([]byte, error) {
	desc := f.Subtitle
	if desc == "" {
		desc = f.Title
	}
	r := rss{Version: "2.0", Atom: "http://www.w3.org/2005/Atom", Content: "http://purl.org/rss/1.0/modules/content/", DC: "http://purl.org/dc/elements/1.1/",
		Channel: rssChannel{Title: f.Title, Link: f.Link, Desc: desc, Lang: f.Lang, Updated: f.updated().UTC().Format(time.RFC1123Z), Gen: "Enterprise Wiki",
			Self: rssAtomLink{Href: f.Self, Rel: "self", Type: "application/rss+xml"}}}
	for _, e := range f.Entries {
		pub := e.Published
		if pub.IsZero() {
			pub = e.Updated
		}
		it := rssItem{Title: e.Title, Link: e.Link, GUID: rssGUID{Perma: "false", Value: e.ID}, PubDate: pub.UTC().Format(time.RFC1123Z),
			Author: e.Author, Desc: e.Summary, Categories: e.Categories}
		if e.Content != "" {
			it.Content = &cdata{Body: strings.ReplaceAll(e.Content, "]]>", "]]]]><![CDATA[>")}
		}
		r.Channel.Items = append(r.Channel.Items, it)
	}
	return marshal(r)
}

func marshal(v any) ([]byte, error) {
	b, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(b, '\n')...), nil
}

func (f Feed) updated() time.Time {
	u := f.Updated
	for _, e := range f.Entries {
		if e.Updated.After(u) {
			u = e.Updated
		}
	}
	if u.IsZero() {
		u = time.Unix(0, 0)
	}
	return u
}

// Write sends the feed in format (atom or rss) with caching headers.
// private marks feeds that depend on the reader (personal feed link).
func Write(w http.ResponseWriter, r *http.Request, f Feed, format string, private bool) error {
	var body []byte
	var err error
	ctype := "application/atom+xml; charset=utf-8"
	switch format {
	case "", "atom":
		body, err = f.Atom()
	case "rss":
		body, err = f.RSS()
		ctype = "application/rss+xml; charset=utf-8"
	default:
		return fmt.Errorf("unknown feed format %q (atom, rss)", format)
	}
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:12]) + `"`
	hdr := w.Header()
	hdr.Set("ETag", etag)
	hdr.Set("Last-Modified", f.updated().UTC().Format(http.TimeFormat))
	if private {
		hdr.Set("Cache-Control", "private, max-age=300")
	} else {
		hdr.Set("Cache-Control", "public, max-age=300")
	}
	hdr.Set("X-Content-Type-Options", "nosniff")
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	hdr.Set("Content-Type", ctype)
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(body)
	return err
}
