// Package core_waf is the web application firewall of the wiki: every
// request goes through the OWASP Core Rule Set (CRS 4, in Coraza) before the
// wiki handles it, with the exclusions the wiki needs (the same file as
// docker/waf/ewiki-crs-exclusions.conf). Shipped but off (default_off): it
// is meant for installations without a firewall in front.
//
// Modes: "detect" (default) only logs what would be blocked, "block"
// refuses it with 403. Not checked: static files (/assets/) and the bodies
// of file uploads; a body is checked up to core.waf.body_kb, the rest
// passes unchecked (the wiki checks every input itself).
package core_waf

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	coreruleset "github.com/corazawaf/coraza-coreruleset/v4"
	"github.com/corazawaf/coraza/v3"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// Exclusions are the rules the wiki needs before the CRS (the same as
// docker/waf/ewiki-crs-exclusions.conf; a test keeps both equal).
//
//go:embed exclusions.conf
var Exclusions string

// Plugin is the web application firewall.
type Plugin struct {
	host *coreplugin.Host

	mu    sync.Mutex
	waf   coraza.WAF
	built string // settings the WAF was built with
	err   error
}

func init() { coreplugin.Register(&Plugin{}) }

// Manifest implements coreplugin.Plugin.
func (p *Plugin) Manifest() coreplugin.Manifest { return coreplugin.ParseManifest(manifest) }

// Locales implements coreplugin.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Setup implements coreplugin.SetupPlugin.
func (p *Plugin) Setup(h *coreplugin.Host) error {
	p.host = h
	h.RegisterRequestFilter(p)
	// GET /api/v1/x/core.waf/status – mode and whether the rules loaded.
	// action: core.waf.status
	h.Handle("GET", "/status", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := h.RequirePerm(r, "core.waf.manage"); err != nil {
			return nil, err
		}
		mode, level := p.settings()
		_, err := p.get()
		st := map[string]any{"mode": mode, "paranoia": level, "rules": "CRS 4 (Coraza)", "body_kb": h.Limit("core.waf.body_kb"), "loaded": err == nil}
		if err != nil {
			st["error"] = err.Error()
		}
		return st, nil
	})
	return nil
}

// settings reads mode and paranoia level; anything unknown falls back to
// the safe defaults (detect, level 1).
func (p *Plugin) settings() (mode string, level int) {
	s := p.host.Settings()
	mode = "detect"
	if v, _ := s["mode"].(string); v == "block" {
		mode = "block"
	}
	level = 1
	switch v := s["paranoia"].(type) {
	case float64:
		if v == 2 {
			level = 2
		}
	case int:
		if v == 2 {
			level = 2
		}
	case string:
		if v == "2" {
			level = 2
		}
	}
	return mode, level
}

// Directives returns the configuration of the WAF for a paranoia level and
// a body limit (bytes).
func Directives(level int, bodyLimit int64) string {
	return fmt.Sprintf(`
SecRuleEngine On
SecRequestBodyAccess On
SecRequestBodyLimit %[2]d
SecRequestBodyInMemoryLimit %[2]d
SecRequestBodyNoFilesLimit %[2]d
SecRequestBodyLimitAction ProcessPartial
SecResponseBodyAccess Off
SecRule REQUEST_HEADERS:Content-Type "@rx ^application/json" "id:200001,phase:1,t:none,t:lowercase,pass,nolog,ctl:requestBodyProcessor=JSON"
Include @crs-setup.conf.example
SecAction "id:900000,phase:1,pass,t:none,nolog,setvar:tx.blocking_paranoia_level=%[1]d"
%[3]s
Include @owasp_crs/*.conf
`, level, bodyLimit, Exclusions)
}

// get returns the WAF for the current settings, built once per change.
func (p *Plugin) get() (coraza.WAF, error) {
	_, level := p.settings()
	limit := p.host.Limit("core.waf.body_kb") << 10
	key := fmt.Sprint(level, "/", limit)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.built == key && (p.waf != nil || p.err != nil) {
		return p.waf, p.err
	}
	p.waf, p.err = coraza.NewWAF(coraza.NewWAFConfig().WithRootFS(coreruleset.FS).WithDirectives(Directives(level, limit)))
	p.built = key
	if p.err != nil {
		p.host.Log.Error("waf: rules not loaded", "err", p.err)
	}
	return p.waf, p.err
}

// FilterRequest implements coreplugin.RequestFilter.
func (p *Plugin) FilterRequest(r *http.Request, clientIP string) error {
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && strings.HasPrefix(r.URL.Path, "/assets/") {
		return nil // static files of the wiki itself
	}
	waf, err := p.get()
	if err != nil {
		// the rules are compiled in; if they cannot load, the wiki keeps
		// working (it checks every input itself) and the error is logged
		return nil
	}
	mode, _ := p.settings()
	limit := p.host.Limit("core.waf.body_kb") << 10
	tx := waf.NewTransaction()
	defer func() {
		tx.ProcessLogging()
		_ = tx.Close()
	}()
	tx.ProcessConnection(clientIP, 0, "", 0)
	tx.ProcessURI(r.URL.RequestURI(), r.Method, r.Proto)
	tx.AddRequestHeader("Host", r.Host)
	for k, vs := range r.Header {
		for _, v := range vs {
			tx.AddRequestHeader(k, v)
		}
	}
	if r.ContentLength > 0 {
		tx.AddRequestHeader("Content-Length", fmt.Sprint(r.ContentLength))
	}
	it := tx.ProcessRequestHeaders()
	if it == nil {
		ct := strings.ToLower(r.Header.Get("Content-Type"))
		if r.Body != nil && r.Body != http.NoBody && !strings.HasPrefix(ct, "multipart/") {
			buf, err := io.ReadAll(io.LimitReader(r.Body, limit))
			// the handler reads the same bytes, then the rest
			r.Body = readCloser{io.MultiReader(bytes.NewReader(buf), r.Body), r.Body}
			if err != nil {
				return err // e.g. the body is larger than the request limit
			}
			if _, _, err := tx.WriteRequestBody(buf); err != nil {
				return nil
			}
		}
		if it, err = tx.ProcessRequestBody(); err != nil {
			return nil
		}
	}
	if it == nil {
		return nil
	}
	var rules []string
	for _, m := range tx.MatchedRules() {
		if id := m.Rule().ID(); id < 949000 && m.Message() != "" {
			rules = append(rules, fmt.Sprintf("%d %s", id, m.Message()))
		}
	}
	p.host.Log.Warn("waf: request "+map[bool]string{true: "blocked", false: "would be blocked (mode detect)"}[mode == "block"],
		"id", tx.ID(), "ip", clientIP, "method", r.Method, "path", r.URL.Path, "rules", strings.Join(rules, "; "), "at", time.Now().UTC().Format(time.RFC3339))
	if mode != "block" {
		return nil
	}
	return &apperr.Error{Status: http.StatusForbidden, Code: "waf_blocked", Msg: "the web application firewall refused this request"}
}

type readCloser struct {
	io.Reader
	io.Closer
}
