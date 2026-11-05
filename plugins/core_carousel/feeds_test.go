package core_carousel

import "testing"

func TestParseFeed(t *testing.T) {
	rss := `<?xml version="1.0"?><rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/"><channel><title>News</title>
<item><title>First &amp; best</title><link>/a</link><guid>1</guid><description>&lt;p&gt;Hello &lt;b&gt;world&lt;/b&gt;&lt;/p&gt;</description>
<pubDate>Mon, 02 Nov 2026 10:00:00 +0100</pubDate><media:thumbnail url="https://img.example.com/1.jpg"/></item>
<item><title>Second</title><link>javascript:alert(1)</link><enclosure url="/2.png" type="image/png"/></item></channel></rss>`
	items, err := ParseFeed([]byte(rss), "https://news.example.com/feed.xml")
	if err != nil || len(items) != 2 {
		t.Fatalf("%v %+v", err, items)
	}
	if items[0].Title != "First & best" || items[0].Link != "https://news.example.com/a" || items[0].Summary != "Hello world" || items[0].Image != "https://img.example.com/1.jpg" || items[0].Published == nil {
		t.Fatalf("item 1: %+v", items[0])
	}
	if items[1].Link != "" || items[1].Image != "https://news.example.com/2.png" {
		t.Fatalf("item 2: %+v", items[1])
	}
	atom := `<feed xmlns="http://www.w3.org/2005/Atom"><entry><title>Atom entry</title><id>urn:1</id><link href="https://b.example.org/x"/>
<summary>Short</summary><updated>2026-11-01T08:00:00Z</updated></entry></feed>`
	items, err = ParseFeed([]byte(atom), "https://b.example.org/atom")
	if err != nil || len(items) != 1 || items[0].Link != "https://b.example.org/x" || items[0].GUID != "urn:1" {
		t.Fatalf("atom: %v %+v", err, items)
	}
	if _, err := ParseFeed([]byte(`<html></html>`), "https://x.example"); err == nil {
		t.Fatal("html accepted as feed")
	}
}
