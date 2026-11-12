package server

import (
	"fmt"
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/webhook"
)

func (s *Server) routesM8() {
	// GET /api/v1/prerender?path=/e/1 – the page as guests and search engines receive it
	s.handle("GET /api/v1/prerender", s.prerenderAPI)

	// ---------------------------------------------------------------- webhooks (administrators)
	admin := func(fn func(w http.ResponseWriter, r *http.Request) (any, error)) func(w http.ResponseWriter, r *http.Request) (any, error) {
		return func(w http.ResponseWriter, r *http.Request) (any, error) {
			if err := requireAdmin(r); err != nil {
				return nil, err
			}
			return fn(w, r)
		}
	}
	s.handle("GET /api/v1/admin/webhooks", admin(func(w http.ResponseWriter, r *http.Request) (any, error) {
		list, err := s.app.Webhooks.List(r.Context())
		return map[string]any{"webhooks": list, "events": webhook.Events, "formats": webhook.Formats,
			"accounts_in_all": s.app.Config.Get().Webhooks.AccountsInAll}, err
	}))
	// PUT /api/v1/admin/webhooks/options {"accounts_in_all": false}: whether
	// webhooks with "all events" also get the events of accounts
	s.handle("PUT /api/v1/admin/webhooks/options", admin(func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			AccountsInAll bool `json:"accounts_in_all"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if err := s.app.Config.Update(func(c *config.Config) error { c.Webhooks.AccountsInAll = in.AccountsInAll; return nil }); err != nil {
			return nil, err
		}
		s.auditSettings(r, "webhooks.options", fmt.Sprintf("accounts_in_all=%v", in.AccountsInAll))
		return map[string]bool{"accounts_in_all": in.AccountsInAll}, nil
	}))
	s.handle("POST /api/v1/admin/webhooks", admin(func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in webhook.Input
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		c, err := s.app.Webhooks.Create(r.Context(), in)
		if err != nil {
			return nil, err
		}
		_ = audit.Log(r.Context(), s.app.DB, "webhook.create", "webhook", c.ID, nil, c.Hook)
		return h.Created{V: c}, nil
	}))
	s.handle("GET /api/v1/admin/webhooks/{id}", admin(func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		hook, err := s.app.Webhooks.Get(r.Context(), id)
		if err != nil {
			return nil, err
		}
		del, err := s.app.Webhooks.Deliveries(r.Context(), id, h.QueryInt(r, "limit", 50))
		return map[string]any{"webhook": hook, "deliveries": del}, err
	}))
	s.handle("PUT /api/v1/admin/webhooks/{id}", admin(func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in webhook.Input
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		before, err := s.app.Webhooks.Get(r.Context(), id)
		if err != nil {
			return nil, err
		}
		c, err := s.app.Webhooks.Update(r.Context(), id, in)
		if err != nil {
			return nil, err
		}
		_ = audit.Log(r.Context(), s.app.DB, "webhook.update", "webhook", id, before, c.Hook)
		return c, nil
	}))
	s.handle("DELETE /api/v1/admin/webhooks/{id}", admin(func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		before, err := s.app.Webhooks.Get(r.Context(), id)
		if err != nil {
			return nil, err
		}
		if err := s.app.Webhooks.Delete(r.Context(), id); err != nil {
			return nil, err
		}
		_ = audit.Log(r.Context(), s.app.DB, "webhook.delete", "webhook", id, before, nil)
		return h.NoContent{}, nil
	}))
	// POST /api/v1/admin/webhooks/{id}/test sends a test event
	s.handle("POST /api/v1/admin/webhooks/{id}/test", admin(func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Webhooks.Test(r.Context(), id)
	}))
	// POST /api/v1/admin/webhooks/{id}/deliveries/{did}/retry sends a delivery again
	s.handle("POST /api/v1/admin/webhooks/{id}/deliveries/{did}/retry", admin(func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		did, err := h.PathID(r, "did")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Webhooks.Redeliver(r.Context(), id, did)
	}))
}
