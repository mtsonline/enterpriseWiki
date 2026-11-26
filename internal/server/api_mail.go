package server

import (
	"net/http"
	"strconv"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/mail"
)

// routesMail registers the administration of e-mail: settings check, test
// message and the outbox (retry, delete).
func (s *Server) routesMail() {
	// ---------------------------------------------------------------- e-mail (admin)
	// action: core.mail.status
	s.handle("GET /api/v1/admin/mail", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return s.app.Mail.GetStatus(r.Context())
	})
	// POST /api/v1/admin/mail/test {"to": "..."} sends a test message right away.
	// action: core.mail.test
	s.handle("POST /api/v1/admin/mail/test", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		var in struct {
			To string `json:"to"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		site := s.app.Config.Get().UI.SiteName
		err := s.app.Mail.SendNow(r.Context(), mail.Message{To: []string{in.To}, Source: "test",
			Subject: site + ": test message", Text: "This is a test message from " + site + " (" + s.app.Config.Get().Server.BaseURL + ").\n\nOutgoing e-mail works."})
		if err != nil {
			var ae *apperr.Error
			if asAppErr(err, &ae) {
				return nil, err
			}
			return nil, apperr.BadRequest("sending failed: %s", err.Error())
		}
		s.auditSettings(r, "mail.test", in.To)
		return map[string]string{"status": "sent"}, nil
	})
	// action: core.mail.list
	s.handle("GET /api/v1/admin/mail/outbox", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return s.app.Mail.Outbox(r.Context(), r.URL.Query().Get("status"), r.URL.Query().Get("q"), h.QueryInt(r, "limit", 100), h.QueryInt(r, "offset", 0))
	})
	// GET /api/v1/admin/mail/outbox/{id} returns a message with the delivery state of every recipient.
	// action: core.mail.get
	s.handle("GET /api/v1/admin/mail/outbox/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Mail.Get(r.Context(), id)
	})
	// action: core.mail.retry
	s.handle("POST /api/v1/admin/mail/outbox/{id}/retry", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Mail.Retry(r.Context(), id)
	})
	// action: core.mail.delete
	s.handle("DELETE /api/v1/admin/mail/outbox/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		if err := s.app.Mail.Delete(r.Context(), id); err != nil {
			return nil, err
		}
		s.auditSettings(r, "mail.delete", strconv.FormatInt(id, 10))
		return h.NoContent{}, nil
	})
}
