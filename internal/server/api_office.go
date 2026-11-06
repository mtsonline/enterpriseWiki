package server

// Office programs: list of programs, opening a medium and the WOPI host
// (https://learn.microsoft.com/microsoft-365/cloud-storage-partner-program/rest/)
// that Collabora Online, OnlyOffice Docs and Microsoft 365 for the web call.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/media"
)

// WOPIPath is the path of the WOPI files endpoint.
const WOPIPath = media.WOPIPath

type viewerInfo struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Restrict bool   `json:"restrict"`
}

func (s *Server) routesOffice() {
	s.handle("GET /api/v1/media/viewers", func(w http.ResponseWriter, r *http.Request) (any, error) {
		cfg := s.app.Config.Get()
		out := []viewerInfo{{ID: media.Internal, Label: "Browser"}}
		for _, o := range s.app.Media.Openers() {
			out = append(out, viewerInfo{ID: o.ID(), Label: o.Label(), Restrict: o.Caps().Restrict})
		}
		def := cfg.Media.Viewer
		if def == "" || s.app.Media.Opener(def) == nil {
			def = media.Internal
		}
		return map[string]any{"viewers": out, "default": def, "open_external": cfg.Media.OpenExternal && def != media.Internal}, nil
	})
	s.handle("GET /api/v1/media/{id}/open", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		cfg := s.app.Config.Get()
		return s.app.Media.OpenOptions(r.Context(), id, cfg.Media.Viewer, cfg.Media.OpenExternal)
	})
	s.handle("POST /api/v1/media/{id}/open", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, err
		}
		var in media.OpenInput
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		cfg := s.app.Config.Get()
		in.BaseURL = cfg.Server.BaseURL
		in.TTL = config.Duration(cfg.Media.TokenTTL, 10*time.Hour)
		in.Lang = cfg.I18n.DefaultLanguage
		if p := identity.From(r.Context()); p != nil && p.Language != "" {
			in.Lang = p.Language
		}
		return s.app.Media.Open(r.Context(), id, in)
	})

	// WOPI host: called by the office server, authenticated by access_token
	// (plus proof keys when the plugin verifies them). Never cookies.
	s.wopiRoute("GET /api/v1/wopi/files/{id}", s.wopiCheckFileInfo)
	s.wopiRoute("POST /api/v1/wopi/files/{id}", s.wopiFileOp)
	s.wopiRoute("GET /api/v1/wopi/files/{id}/contents", s.wopiGetFile)
	s.wopiRoute("POST /api/v1/wopi/files/{id}/contents", s.wopiPutFile)
}

func (s *Server) wopiRoute(pattern string, fn wopiHandler) {
	if _, path, _ := strings.Cut(pattern, " "); !strings.HasPrefix(path, WOPIPath) {
		panic("WOPI route outside " + WOPIPath)
	}
	h.RecordRoute(pattern)
	s.app.Mux.HandleFunc(pattern, s.wopi(fn))
}

type wopiReq struct {
	ctx context.Context
	tok *media.Token
	m   *media.Medium
	raw string
}

type wopiHandler func(w http.ResponseWriter, r *http.Request, q *wopiReq) error

// wopiError writes a WOPI error: 401 for token problems, 500 otherwise
// (no details leave the server).
func (s *Server) wopiError(w http.ResponseWriter, err error) {
	var lm *media.ErrLockMismatch
	var ae *apperr.Error
	switch {
	case errors.As(err, &lm):
		w.Header().Set("X-WOPI-Lock", lm.Current)
		w.WriteHeader(http.StatusConflict)
	case errors.Is(err, media.ErrBadToken):
		w.WriteHeader(http.StatusUnauthorized)
	case errors.As(err, &ae) && (ae.Status == http.StatusNotFound || ae.Status == http.StatusRequestEntityTooLarge || ae.Status == http.StatusNotImplemented || ae.Status == http.StatusUnauthorized):
		w.WriteHeader(ae.Status)
	default:
		s.log.Warn("wopi", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (s *Server) wopi(fn wopiHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if identity.From(r.Context()) != nil {
			// a browser session must never drive the WOPI host
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		id, err := pathID(r)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		tok := r.URL.Query().Get("access_token")
		ctx, t, m, err := s.app.Media.TokenContext(r.Context(), id, tok)
		if err != nil {
			s.wopiError(w, err)
			return
		}
		if pv, ok := s.app.Media.Opener(t.Viewer).(media.ProofVerifier); ok {
			full := strings.TrimSuffix(s.app.Config.Get().Server.BaseURL, "/") + r.URL.RequestURI()
			if err := pv.VerifyProof(ctx, r, tok, full); err != nil {
				s.log.Warn("wopi proof failed", "viewer", t.Viewer, "media", id, "ip", s.clientIP(r))
				w.WriteHeader(http.StatusInternalServerError) // as the WOPI spec asks
				return
			}
		}
		if err := fn(w, r.WithContext(ctx), &wopiReq{ctx: ctx, tok: t, m: m, raw: tok}); err != nil {
			s.wopiError(w, err)
		}
	}
}

func wopiJSON(w http.ResponseWriter, v any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	return json.NewEncoder(w).Encode(v)
}

func (s *Server) wopiCheckFileInfo(w http.ResponseWriter, r *http.Request, q *wopiReq) error {
	m, t := q.m, q.tok
	p := identity.From(q.ctx)
	edit := t.Mode == media.ModeEdit
	base := strings.TrimSuffix(s.app.Config.Get().Server.BaseURL, "/")
	owner := ""
	if m.CreatedBy != nil {
		owner = strconv.FormatInt(*m.CreatedBy, 10)
	}
	userID, name := "guest", "Guest"
	if p != nil {
		userID = strconv.FormatInt(p.UserID, 10)
		name = p.DisplayName
		if name == "" {
			name = p.Username
		}
	}
	info := map[string]any{
		"BaseFileName":               m.Name,
		"BreadcrumbDocName":          m.Name,
		"OwnerId":                    owner,
		"Size":                       m.Size,
		"UserId":                     userID,
		"UserFriendlyName":           name,
		"IsAnonymousUser":            p == nil,
		"Version":                    media.ItemVersion(m),
		"LastModifiedTime":           m.UpdatedAt.UTC().Format(time.RFC3339),
		"UserCanWrite":               edit,
		"ReadOnly":                   !edit,
		"SupportsUpdate":             true,
		"SupportsLocks":              true,
		"SupportsGetLock":            true,
		"SupportsExtendedLockLength": true,
		"SupportsRename":             false,
		"UserCanRename":              false,
		"UserCanNotWriteRelative":    true,
		"PostMessageOrigin":          base,
		"HostEditUrl":                base + "/media?open=" + strconv.FormatInt(m.ID, 10),
		"DisablePrint":               t.Restricted,
		"DisableExport":              t.Restricted,
		"DisableCopy":                t.Restricted,
		"HidePrintOption":            t.Restricted,
		"HideExportOption":           t.Restricted,
		"HideSaveOption":             t.Restricted,
		"DisableTranslation":         t.Restricted,
	}
	if wm := s.app.Media.OfficeWatermark(q.ctx, m); wm != "" {
		info["WatermarkText"] = wm
	}
	if fx, ok := s.app.Media.Opener(t.Viewer).(media.FileInfoExtender); ok {
		fx.ExtendFileInfo(info, t, m)
	}
	return wopiJSON(w, info)
}

func (s *Server) wopiGetFile(w http.ResponseWriter, r *http.Request, q *wopiReq) error {
	data, err := s.app.Media.Content(q.ctx, q.m)
	if err != nil {
		return err
	}
	if limit := r.Header.Get("X-WOPI-MaxExpectedSize"); limit != "" {
		if n, err := strconv.ParseInt(limit, 10, 64); err == nil && int64(len(data)) > n {
			w.WriteHeader(http.StatusPreconditionFailed)
			return nil
		}
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-WOPI-ItemVersion", media.ItemVersion(q.m))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, err = w.Write(data)
	return err
}

func (s *Server) wopiPutFile(w http.ResponseWriter, r *http.Request, q *wopiReq) error {
	if r.Header.Get("X-WOPI-Override") != "PUT" {
		return apperr.New(http.StatusNotImplemented, "not_implemented", "unsupported operation")
	}
	if q.tok.Mode != media.ModeEdit {
		return media.ErrBadToken
	}
	cur, err := s.app.Media.CurrentLock(q.ctx, q.m.ID)
	if err != nil {
		return err
	}
	if lock := r.Header.Get("X-WOPI-Lock"); cur != "" && cur != lock {
		return &media.ErrLockMismatch{Current: cur}
	}
	// Collabora: refuse to overwrite a version changed meanwhile
	if ts := r.Header.Get("X-COOL-WOPI-Timestamp"); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil && !t.Equal(q.m.UpdatedAt.UTC().Truncate(time.Second)) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, err := w.Write([]byte(`{"COOLStatusCode":1010}`))
			return err
		}
	}
	m, err := s.app.Media.SaveFromOffice(q.ctx, q.m, q.tok, r.Body)
	if err != nil {
		return err
	}
	w.Header().Set("X-WOPI-ItemVersion", media.ItemVersion(m))
	return wopiJSON(w, map[string]any{"LastModifiedTime": m.UpdatedAt.UTC().Format(time.RFC3339)})
}

func (s *Server) wopiFileOp(w http.ResponseWriter, r *http.Request, q *wopiReq) error {
	op := r.Header.Get("X-WOPI-Override")
	lock := r.Header.Get("X-WOPI-Lock")
	id := q.m.ID
	switch op {
	case "LOCK", "REFRESH_LOCK", "UNLOCK", "GET_LOCK":
	default:
		return apperr.New(http.StatusNotImplemented, "not_implemented", "unsupported operation")
	}
	if q.tok.Mode != media.ModeEdit {
		return media.ErrBadToken
	}
	w.Header().Set("X-WOPI-ItemVersion", media.ItemVersion(q.m))
	switch op {
	case "GET_LOCK":
		cur, err := s.app.Media.CurrentLock(q.ctx, id)
		if err != nil {
			return err
		}
		w.Header().Set("X-WOPI-Lock", cur)
	case "LOCK":
		if err := s.app.Media.Lock(q.ctx, id, lock, r.Header.Get("X-WOPI-OldLock")); err != nil {
			return err
		}
	case "REFRESH_LOCK":
		cur, err := s.app.Media.CurrentLock(q.ctx, id)
		if err != nil {
			return err
		}
		if cur != lock {
			return &media.ErrLockMismatch{Current: cur}
		}
		if err := s.app.Media.Lock(q.ctx, id, lock, ""); err != nil {
			return err
		}
	case "UNLOCK":
		if err := s.app.Media.Unlock(q.ctx, id, lock); err != nil {
			return err
		}
	}
	w.WriteHeader(http.StatusOK)
	return nil
}

// officeCSP returns the CSP additions for the active office programs.
func (s *Server) officeCSP() (frame, script string) {
	var f, sc bytes.Buffer
	for _, o := range s.app.Media.Openers() {
		for _, origin := range o.Origins() {
			if !validOrigin(origin) {
				continue
			}
			f.WriteString(" " + origin)
			if o.Caps().Script {
				sc.WriteString(" " + origin)
			}
		}
	}
	return f.String(), sc.String()
}

// validOrigin accepts only scheme://host[:port] (https or http) without paths
// or characters that could break the header.
func validOrigin(o string) bool {
	rest, ok := strings.CutPrefix(o, "https://")
	if !ok {
		if rest, ok = strings.CutPrefix(o, "http://"); !ok {
			return false
		}
	}
	if rest == "" || len(rest) > 253 {
		return false
	}
	for _, c := range rest {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == ':' || c == '[' || c == ']') {
			return false
		}
	}
	return true
}
