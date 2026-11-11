package content_events

import (
	"strings"
	"testing"
)

func TestParseICSExternal(t *testing.T) {
	doc := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:a\r\nDTSTART:20261201T090000Z\r\nDTEND:20261201T100000Z\r\nSUMMARY:Market\\, open\r\n" +
		"DESCRIPTION:Line 1\\nLine <b>2</b>\r\nURL:javascript:alert(1)\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:b\r\nDTSTART;VALUE=DATE:20261224\r\nSUMMARY:Holiday\r\nURL:https://example.org/h\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	ev := ParseICS(doc, "https://example.org/cal.ics")
	if len(ev) != 2 {
		t.Fatalf("events: %d", len(ev))
	}
	a, b := ev[0], ev[1]
	if a.Title != "Market, open" || !a.External || a.ID >= 0 || a.URL != "" || a.Source != "https://example.org/cal.ics" {
		t.Fatalf("first: %+v", a)
	}
	if b.URL != "https://example.org/h" || !b.AllDay || a.ID == b.ID {
		t.Fatalf("second: %+v", b)
	}
	if h := plainHTML(a.Description); h != "<p>Line 1<br>Line &lt;b&gt;2&lt;/b&gt;</p>" {
		t.Fatalf("plain: %s", h)
	}
	if ParseICS(strings.Repeat("x", 100), "u") != nil && len(ParseICS(strings.Repeat("x", 100), "u")) != 0 {
		t.Fatal("garbage parsed")
	}
}
