// Package office_onlyoffice opens files of the media library in
// OnlyOffice Docs (Document Server), through the OnlyOffice API (default)
// or through WOPI.
//
// Configuration (config.yaml -> plugin_settings."office.onlyoffice"):
//
//	url: https://docs.example.com       # public address (browser)
//	internal_url: http://onlyoffice     # optional: address the wiki may download saved files from
//	mode: api                           # api (default) | wopi
//	jwt_secret: ${ONLYOFFICE_JWT}       # api: same as services.CoAuthoring.secret (required)
//	verify_proof: true                  # wopi: check the proof key (default)
//	readonly_without_download: true     # view-only users may view there (default)
package office_onlyoffice

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/media"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
	"github.com/mtsonline/enterprisewiki/pkg/wopi"

	_ "embed"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// Plugin is the OnlyOffice plugin (one instance per site).
type Plugin struct {
	host *sdk.Host
	wop  *wopi.Opener
	http *http.Client
}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Settings of the plugin.
type Settings struct {
	URL         string
	InternalURL string
	Mode        string
	Secret      string
	VerifyProof bool
	ReadOnly    bool
}

func (p *Plugin) settings() Settings {
	m := p.host.Settings()
	s := Settings{URL: strings.TrimSuffix(wopi.Str(m, "url"), "/"), InternalURL: strings.TrimSuffix(wopi.Str(m, "internal_url"), "/"),
		Mode: wopi.Str(m, "mode"), Secret: wopi.Str(m, "jwt_secret"),
		VerifyProof: wopi.Bool(m, "verify_proof", true), ReadOnly: wopi.Bool(m, "readonly_without_download", true)}
	if s.Mode != "wopi" {
		s.Mode = "api"
	}
	return s
}

func (p *Plugin) wopiSettings() wopi.Settings {
	s := p.settings()
	d := ""
	if s.Mode == "wopi" {
		d = wopi.DiscoveryURL(s.URL)
	}
	return wopi.Settings{Discovery: d, VerifyProof: s.VerifyProof, ReadOnlyWithoutDownload: s.ReadOnly}
}

// Setup registers the program, the endpoints of the Document Server and
// the connection test.
func (p *Plugin) Setup(host *sdk.Host) error {
	p.host = host
	p.http = &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	p.wop = &wopi.Opener{PluginID: host.Manifest.ID, Name: "OnlyOffice", Restrict: true, Settings: p.wopiSettings}
	host.Media.RegisterOpener(&opener{p})
	if p.settings().Mode == "wopi" {
		p.wop.Warm()
	}
	base := "/api/v1/x/" + host.Manifest.ID
	host.Media.RegisterTokenPath(base + "/file/")
	host.Media.RegisterTokenPath(base + "/callback/")
	host.Handle("GET", "/file/{id}", p.file)
	host.Handle("POST", "/callback/{id}", p.callback)
	host.Handle("POST", "/test", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := perm.Require(r.Context(), "core.settings.manage"); err != nil {
			return nil, err
		}
		return p.test(r.Context()), nil
	})
	return nil
}

// ---------------------------------------------------------------- formats

var docType = map[string]string{}

func init() {
	for t, exts := range map[string]string{
		"word":  "doc docm docx dot dotm dotx epub fb2 fodt htm html mht mhtml odt ott rtf txt",
		"cell":  "csv fods ods ots xls xlsb xlsm xlsx xlt xltm xltx",
		"slide": "fodp odp otp pot potm potx pps ppsm ppsx ppt pptm pptx",
		"pdf":   "pdf djvu xps oxps",
	} {
		for _, e := range strings.Fields(exts) {
			docType[e] = t
		}
	}
}

var editable = map[string]bool{}

func init() {
	for _, e := range strings.Fields("docx docm dotx odt ott rtf txt xlsx xlsm xltx ods ots csv pptx pptm potx odp otp") {
		editable[e] = true
	}
}

// ---------------------------------------------------------------- opener

type opener struct{ p *Plugin }

var _ media.Opener = (*opener)(nil)

func (o *opener) ID() string    { return o.p.host.Manifest.ID }
func (o *opener) Label() string { return "OnlyOffice" }

func (o *opener) Caps() media.OpenerCaps {
	return media.OpenerCaps{Restrict: true, Script: o.p.settings().Mode == "api"}
}

func (o *opener) Options() media.OpenerOptions {
	return media.OpenerOptions{ReadOnlyWithoutDownload: o.p.settings().ReadOnly}
}

func (o *opener) Supports(ctx context.Context, name, mime string) (bool, bool) {
	s := o.p.settings()
	if s.URL == "" {
		return false, false
	}
	if s.Mode == "wopi" {
		return o.p.wop.Supports(ctx, name, mime)
	}
	if s.Secret == "" {
		return false, false // never without JWT
	}
	ext := media.Ext(name)
	return docType[ext] != "", editable[ext]
}

func (o *opener) Origins() []string {
	s := o.p.settings()
	if s.Mode == "wopi" {
		return o.p.wop.Origins()
	}
	if origin := wopi.Origin(s.URL); origin != "" {
		return []string{origin}
	}
	return nil
}

func (o *opener) VerifyProof(ctx context.Context, r *http.Request, token, fullURL string) error {
	if o.p.settings().Mode != "wopi" {
		return errors.New("WOPI is switched off for OnlyOffice")
	}
	return o.p.wop.VerifyProof(ctx, r, token, fullURL)
}

func (o *opener) Launch(ctx context.Context, l media.Launch) (*media.Action, error) {
	s := o.p.settings()
	if s.Mode == "wopi" {
		return o.p.wop.Launch(ctx, l)
	}
	if s.Secret == "" {
		return nil, errors.New("no JWT secret configured")
	}
	m := l.Medium
	ext := media.Ext(m.Name)
	base := l.BaseURL + "/api/v1/x/" + o.ID()
	q := "?access_token=" + url.QueryEscape(l.Token)
	id := strconv.FormatInt(m.ID, 10)
	edit := l.Mode == media.ModeEdit
	free := !l.Restricted
	user := map[string]any{"id": "guest", "name": l.UserName}
	if l.UserID > 0 {
		user["id"] = strconv.FormatInt(l.UserID, 10)
	}
	mode := "view"
	if edit {
		mode = "edit"
	}
	cfg := map[string]any{
		"documentType": docType[ext],
		"document": map[string]any{
			"fileType": ext,
			"key":      fmt.Sprintf("ew-%s-%d-v%d-%d", o.p.host.Site, m.ID, m.Version, m.UpdatedAt.Unix()),
			"title":    m.Name,
			"url":      base + "/file/" + id + q,
			"permissions": map[string]any{
				"edit": edit, "download": free, "print": free, "copy": free, "comment": edit, "review": edit,
				"fillForms": edit, "modifyContentControl": edit, "modifyFilter": edit,
			},
		},
		"editorConfig": map[string]any{
			"mode":        mode,
			"lang":        l.Lang,
			"user":        user,
			"callbackUrl": base + "/callback/" + id + q,
			"customization": map[string]any{
				"forcesave": false, "uiTheme": "default-light",
			},
		},
		"exp": l.Expires.Unix(),
	}
	tok, err := signJWT(cfg, s.Secret)
	if err != nil {
		return nil, err
	}
	delete(cfg, "exp")
	cfg["token"] = tok
	return &media.Action{Kind: "onlyoffice", URL: s.URL, Script: s.URL + "/web-apps/apps/api/documents/api.js", Config: cfg, Origin: wopi.Origin(s.URL)}, nil
}

// ---------------------------------------------------------------- Document Server endpoints

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

// bearer verifies the JWT the Document Server sends in the Authorization
// header (inbox / outbox requests).
func bearer(r *http.Request, secret string, v any) error {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return errJWT
	}
	return verifyJWT(strings.TrimSpace(tok), secret, v)
}

// file serves the file to the Document Server.
func (p *Plugin) file(w http.ResponseWriter, r *http.Request) (any, error) {
	s := p.settings()
	if s.Mode != "api" || s.Secret == "" {
		return nil, apperr.NotFound("endpoint")
	}
	if err := bearer(r, s.Secret, nil); err != nil {
		return nil, apperr.ErrUnauthorized
	}
	ctx, _, m, err := p.host.Media.TokenContext(r.Context(), pathID(r), r.URL.Query().Get("access_token"))
	if err != nil {
		return nil, err
	}
	data, err := p.host.Media.Content(ctx, m)
	if err != nil {
		return nil, err
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data) // the Document Server retries on a broken connection
	return h.Handled{}, nil
}

type callbackBody struct {
	Key    string `json:"key"`
	Status int    `json:"status"`
	URL    string `json:"url"`
	Token  string `json:"token"`
}

// callback receives the status of a document; status 2 (ready to save) and
// 6 (force save) store the edited file as a new version.
func (p *Plugin) callback(w http.ResponseWriter, r *http.Request) (any, error) {
	s := p.settings()
	if s.Mode != "api" || s.Secret == "" {
		return nil, apperr.NotFound("endpoint")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var body callbackBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, apperr.BadRequest("invalid body")
	}
	// the signed copy is authoritative: from the body or the header
	var signed callbackBody
	if body.Token != "" {
		if err := verifyJWT(body.Token, s.Secret, &signed); err != nil {
			return nil, apperr.ErrUnauthorized
		}
	} else {
		var wrapped struct {
			Payload callbackBody `json:"payload"`
		}
		if err := bearer(r, s.Secret, &wrapped); err != nil {
			return nil, apperr.ErrUnauthorized
		}
		signed = wrapped.Payload
	}
	ctx, tok, m, err := p.host.Media.TokenContext(r.Context(), pathID(r), r.URL.Query().Get("access_token"))
	if err != nil {
		return nil, err
	}
	if signed.Status != 2 && signed.Status != 6 {
		return map[string]int{"error": 0}, nil
	}
	if tok.Mode != media.ModeEdit {
		return nil, apperr.ErrForbidden
	}
	data, err := p.download(ctx, signed.URL, s)
	if err != nil {
		p.host.Log.Warn("onlyoffice: download of the saved file failed", "media", m.ID, "err", err)
		return map[string]int{"error": 1}, nil
	}
	if _, err := p.host.Media.SaveFromOffice(ctx, m, tok, strings.NewReader(string(data))); err != nil {
		p.host.Log.Warn("onlyoffice: save failed", "media", m.ID, "err", err)
		return map[string]int{"error": 1}, nil
	}
	return map[string]int{"error": 0}, nil
}

// download fetches a saved file, only from the configured Document Server.
func (p *Plugin) download(ctx context.Context, raw string, s Settings) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return nil, errors.New("invalid URL")
	}
	origin := u.Scheme + "://" + u.Host
	if origin != wopi.Origin(s.URL) && (s.InternalURL == "" || origin != wopi.Origin(s.InternalURL)) {
		return nil, fmt.Errorf("refused: %s is not the configured Document Server", origin)
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.http.Do(req) // #nosec G704 -- origin checked against the configured Document Server
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	limit := p.host.Files.MaxBytes()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("file too large")
	}
	return data, nil
}

// ---------------------------------------------------------------- test

type testResult struct {
	wopi.Status
	Mode   string `json:"mode"`
	Secret bool   `json:"secret"`
}

func (p *Plugin) test(ctx context.Context) testResult {
	s := p.settings()
	if s.Mode == "wopi" {
		return testResult{Status: p.wop.Test(ctx), Mode: s.Mode}
	}
	res := testResult{Mode: s.Mode, Secret: s.Secret != "", Status: wopi.Status{View: []string{}, Edit: []string{}, Origins: []string{}}}
	if s.URL == "" {
		return res
	}
	res.Configured = true
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL+"/healthcheck", nil)
	if err == nil {
		var resp *http.Response
		if resp, err = p.http.Do(req); err == nil { // #nosec G704 -- URL from the administrator's settings
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(b)) != "true" {
				err = fmt.Errorf("healthcheck: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
			}
		}
	}
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.OK = true
	for e := range docType {
		res.View = append(res.View, e)
		if editable[e] {
			res.Edit = append(res.Edit, e)
		}
	}
	res.Origins = []string{wopi.Origin(s.URL)}
	return res
}
