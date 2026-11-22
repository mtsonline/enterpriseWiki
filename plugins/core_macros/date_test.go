package core_macros

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/content"
)

func TestDate(t *testing.T) {
	d := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct{ lang, format, want string }{
		{"de", "long", "1. März 2026"}, {"en", "long", "1 March 2026"},
		{"de", "short", "01.03.2026"}, {"en", "short", "2026-03-01"},
		{"de", "weekday", "Sonntag, 1. März 2026"}, {"en", "weekday", "Sunday, 1 March 2026"},
	} {
		if got := FormatDate(d, c.lang, c.format); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.lang, c.format, got, c.want)
		}
	}
	render := func(v, f string) (string, error) {
		return date{}.Render(context.Background(), content.MacroContext{RenderContext: content.RenderContext{Lang: "de"}, Params: map[string]string{"value": v, "format": f}})
	}
	got, err := render(" 2026-11-22 ", "")
	if err != nil || got != `<time class="ew-date" datetime="2026-11-22">22. November 2026</time>` {
		t.Fatalf("render: %q %v", got, err)
	}
	for _, bad := range []string{"", "22.11.2026", "2026-02-30", "2026-11-22T10:00", `2026-11-22"><script>`, "0000-01-01"} {
		if out, err := render(bad, "long"); err == nil || strings.Contains(out, "<script") {
			t.Errorf("date %q accepted: %q", bad, out)
		}
	}
}
