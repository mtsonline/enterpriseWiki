// Package macro_http provides the "http" macro: the result of an HTTP GET
// request, inserted into a page.
//
// Only URLs below a prefix listed in plugins.macros.http_allow (config.yaml)
// can be fetched. Connections to loopback, private, link-local and other
// internal addresses are refused at connect time (after DNS resolution, so
// DNS rebinding does not help) unless plugins.macros.http_allow_private is
// set. Redirects are checked against the same rules.
package macro_http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/microcosm-cc/bluemonday"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/netguard"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"

	_ "embed"
)

//go:embed plugin.json
var manifest []byte

// MaxBody is the maximum response size.
const MaxBody = 1 << 20

// Plugin is the HTTP macro plugin.
type Plugin struct {
	cfg   *config.Store
	mu    sync.Mutex
	cache map[string]entry
}

type entry struct {
	out     string
	expires time.Time
}

func init() { sdk.Register(newPlugin()) }

func newPlugin() *Plugin { return &Plugin{cache: map[string]entry{}} }

// NewInstance implements sdk.Factory (one cache per site).
func (p *Plugin) NewInstance() sdk.Plugin { return newPlugin() }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Setup registers the macro.
func (p *Plugin) Setup(h *sdk.Host) error {
	p.cfg = h.Config
	h.RegisterMacro(&macro{p: p})
	return nil
}

// Allowed, Internal: see internal/netguard.
var (
	Allowed  = netguard.Allowed
	Internal = netguard.Internal
)

func client(allow []string, allowPrivate bool) *http.Client {
	return netguard.Client(allow, allowPrivate, 15*time.Second)
}

// ---------------------------------------------------------------- macro

type macro struct{ p *Plugin }

func lm(en, de string) map[string]string { return map[string]string{"en": en, "de": de} }

func (m *macro) Name() string              { return "http" }
func (m *macro) Labels() map[string]string { return lm("HTTP request", "HTTP-Abruf") }
func (m *macro) Description() map[string]string {
	return lm("Inserts the result of a web request (only allowed addresses).", "Fügt das Ergebnis eines Web-Abrufs ein (nur freigegebene Adressen).")
}
func (m *macro) HasBody() bool { return false }
func (m *macro) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "url", Labels: lm("URL", "URL"), Type: "url", Required: true},
		{Name: "format", Labels: lm("Format", "Format"), Type: "select", Options: []string{"text", "html", "json"}, Default: "text"},
		{Name: "path", Labels: lm("JSON path (e.g. data.items)", "JSON-Pfad (z. B. data.items)"), Type: "text"},
		{Name: "cache", Labels: lm("Cache (seconds)", "Zwischenspeichern (Sekunden)"), Type: "number", Default: "300"},
		{Name: "title", Labels: lm("Title", "Titel"), Type: "text"},
	}
}

func note(lang, en, de string) string {
	msg := en
	if lang == "de" {
		msg = de
	}
	return `<p class="ew-macro-error" role="note">` + html.EscapeString(msg) + `</p>`
}

var ugc = bluemonday.UGCPolicy()

func (m *macro) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	sec := m.p.cfg.Get().Plugins.Macros
	raw := strings.TrimSpace(mc.Params["url"])
	u, err := url.Parse(raw)
	if raw == "" || err != nil {
		return note(mc.Lang, "HTTP macro: invalid URL.", "HTTP-Makro: ungültige URL."), nil
	}
	if !Allowed(u, sec.HTTPAllow) {
		return note(mc.Lang, "HTTP macro: this address is not on the allow list (plugins.macros.http_allow).",
			"HTTP-Makro: diese Adresse ist nicht freigegeben (plugins.macros.http_allow)."), nil
	}
	format := mc.Params["format"]
	if format != "html" && format != "json" {
		format = "text"
	}
	ttl, err := strconv.Atoi(mc.Params["cache"])
	if err != nil || ttl < 0 || ttl > 86400 {
		ttl = 300
	}
	key := fmt.Sprintf("%s\x00%s\x00%s\x00%v", u.String(), format, mc.Params["path"], sec.HTTPAllowPrivate)
	m.p.mu.Lock()
	if e, ok := m.p.cache[key]; ok && time.Now().Before(e.expires) {
		m.p.mu.Unlock()
		return wrap(mc, e.out), nil
	}
	m.p.mu.Unlock()

	out, err := fetch(ctx, u, format, mc.Params["path"], sec.HTTPAllow, sec.HTTPAllowPrivate)
	if err != nil {
		return note(mc.Lang, "HTTP macro: "+err.Error(), "HTTP-Makro: "+err.Error()), nil
	}
	if ttl > 0 {
		m.p.mu.Lock()
		if len(m.p.cache) > 1000 {
			m.p.cache = map[string]entry{}
		}
		m.p.cache[key] = entry{out: out, expires: time.Now().Add(time.Duration(ttl) * time.Second)}
		m.p.mu.Unlock()
	}
	return wrap(mc, out), nil
}

func wrap(mc content.MacroContext, out string) string {
	t := strings.TrimSpace(mc.Params["title"])
	if t == "" {
		return `<div class="ew-http">` + out + `</div>`
	}
	return `<figure class="ew-http"><figcaption>` + html.EscapeString(t) + `</figcaption>` + out + `</figure>`
}

// fetch performs the request and renders the result as safe HTML.
func fetch(ctx context.Context, u *url.URL, format, path string, allow []string, allowPrivate bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "enterprise-wiki-http-macro")
	if format == "json" {
		req.Header.Set("Accept", "application/json")
	}
	resp, err := client(allow, allowPrivate).Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("server answered %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return "", err
	}
	if len(body) > MaxBody {
		return "", errors.New("response larger than 1 MB")
	}
	switch format {
	case "html":
		return ugc.Sanitize(string(body)), nil
	case "json":
		var v any
		if err := json.Unmarshal(body, &v); err != nil {
			return "", errors.New("response is not valid JSON")
		}
		v = JSONPath(v, path)
		return JSONToHTML(v), nil
	}
	return `<pre class="ew-http-text" tabindex="0">` + html.EscapeString(strings.ToValidUTF8(string(body), "�")) + `</pre>`, nil
}

// JSONPath follows a dot separated path (object keys and array indexes).
func JSONPath(v any, path string) any {
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		switch x := v.(type) {
		case map[string]any:
			v = x[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(x) {
				return nil
			}
			v = x[i]
		default:
			return nil
		}
	}
	return v
}

func scalar(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// JSONToHTML renders scalars as text, arrays of objects as a table, other
// arrays as a list and objects as a definition list.
func JSONToHTML(v any) string {
	esc := html.EscapeString
	switch x := v.(type) {
	case []any:
		objs := len(x) > 0
		cols := []string{}
		seen := map[string]bool{}
		for _, it := range x {
			o, ok := it.(map[string]any)
			if !ok {
				objs = false
				break
			}
			keys := make([]string, 0, len(o))
			for k := range o {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if !seen[k] {
					seen[k] = true
					cols = append(cols, k)
				}
			}
		}
		var b strings.Builder
		if objs {
			b.WriteString(`<div class="ew-table-wrap" tabindex="0"><table><thead><tr>`)
			for _, c := range cols {
				b.WriteString(`<th scope="col">` + esc(c) + `</th>`)
			}
			b.WriteString(`</tr></thead><tbody>`)
			for i, it := range x {
				if i >= 500 {
					break
				}
				b.WriteString(`<tr>`)
				for _, c := range cols {
					b.WriteString(`<td>` + esc(scalar(it.(map[string]any)[c])) + `</td>`)
				}
				b.WriteString(`</tr>`)
			}
			b.WriteString(`</tbody></table></div>`)
			return b.String()
		}
		b.WriteString(`<ul>`)
		for _, it := range x {
			b.WriteString(`<li>` + esc(scalar(it)) + `</li>`)
		}
		b.WriteString(`</ul>`)
		return b.String()
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString(`<dl class="ew-http-object">`)
		for _, k := range keys {
			b.WriteString(`<dt>` + esc(k) + `</dt><dd>` + esc(scalar(x[k])) + `</dd>`)
		}
		b.WriteString(`</dl>`)
		return b.String()
	}
	return `<span class="ew-http-value">` + esc(scalar(v)) + `</span>`
}
