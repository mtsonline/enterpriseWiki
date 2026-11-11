package server

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/collab"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// CollabPath is the prefix of the live editing connections (WebSocket).
const CollabPath = "/api/v1/collab/"

// routesCollab: live editing of a page.
//
//	GET /api/v1/collab/elements/{id}/{lang}   WebSocket (y-protocols)
//
// Only signed-in users with the right to edit the element (session cookie,
// same origin) connect; the session and the right are checked again every
// collab.RecheckEvery while connected.
func (s *Server) routesCollab() {
	pattern := "GET " + CollabPath + "elements/{id}/{lang}"
	h.RecordRoute(pattern)
	s.app.Mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if err := s.serveCollab(w, r); err != nil {
			h.WriteError(w, r, err)
		}
	})
}

func (s *Server) serveCollab(w http.ResponseWriter, r *http.Request) error {
	p := identity.From(r.Context())
	if p == nil || p.UserID == 0 {
		return apperr.ErrUnauthorized
	}
	if !s.app.Config.Get().UI.Collaboration {
		return apperr.Forbidden("live editing is turned off")
	}
	if p.Via != "session" {
		return apperr.Forbidden("live editing needs a browser session")
	}
	id, err := h.PathID(r, "id")
	if err != nil {
		return err
	}
	lang := r.PathValue("lang")
	if !slices.Contains(s.app.Config.Get().I18n.Languages, lang) {
		return apperr.BadRequest("unknown language %q", lang)
	}
	if _, err := s.app.Content.ElementCategory(r.Context(), id, acl.Edit); err != nil {
		return err
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return apperr.BadRequest("WebSocket upgrade required")
	}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return apperr.ErrUnauthorized
	}
	// the connection lives longer than the read/write timeouts of the server
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
	// Accept checks that the Origin is this host (no cross-site WebSocket);
	// compression stays off (memory per connection, compression side channels)
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return nil // Accept has answered
	}
	defer func() { _ = c.CloseNow() }()
	key := s.app.Site + "/element/" + strconv.FormatInt(id, 10) + "/" + lang
	user := collab.User{ID: p.UserID, Name: displayName(p)}
	token := cookie.Value
	allowed := func(ctx context.Context) bool {
		cp, err := s.app.Auth.Authenticate(ctx, token)
		if err != nil || cp.UserID != p.UserID {
			return false
		}
		_, err = s.app.Content.ElementCategory(identity.With(ctx, cp), id, acl.Edit)
		return err == nil
	}
	ctx, stop := context.WithCancel(r.Context())
	defer stop()
	defer context.AfterFunc(s.collabDone, stop)()
	err = s.collab.Serve(ctx, c, key, user, allowed)
	if err != nil && !errors.Is(err, collab.ErrFull) && !errors.Is(err, context.Canceled) {
		s.log.Debug("live editing ended", "element", id, "err", err)
	}
	return nil
}

func displayName(p *identity.Principal) string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.Username
}
