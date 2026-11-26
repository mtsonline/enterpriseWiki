package server

import (
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/limits"
	"github.com/mtsonline/enterprisewiki/internal/mail"
	"github.com/mtsonline/enterprisewiki/internal/notify"
)

// routesNotificationSettings registers the own choice of notifications by
// e-mail (per kind).
func (s *Server) routesNotificationSettings() {
	// ---------------------------------------------------------------- notification preferences
	// action: core.me.notifications.show
	s.handle("GET /api/v1/me/notifications", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		us, err := s.app.Mail.Users(r.Context(), nil, []int64{p.ID()})
		if err != nil || len(us) == 0 {
			return nil, apperr.NotFound("user")
		}
		prefs := map[string]bool{}
		kinds := mail.NotificationKinds()
		for _, k := range kinds {
			v, ok := us[0].Notify[k]
			if !ok {
				v = mail.DefaultOn(k)
			}
			prefs[k] = v
		}
		return map[string]any{"email": us[0].Email, "prefs": prefs, "kinds": kinds, "labels": mail.KindLabels(), "enabled": s.app.Mail.Enabled()}, nil
	})
	// action: core.me.notifications.set
	s.handle("PUT /api/v1/me/notifications", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		var in map[string]bool
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Mail.SetPreferences(r.Context(), p.ID(), in)
	})
}

// routesNotifications registers the own notifications (the bell and the
// page "All notifications"): list them page by page, the pop-ups, mark them
// read and remove one.
func (s *Server) routesNotifications() {
	// action: core.me.inbox.list
	s.handle("GET /api/v1/me/inbox", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		list, unread, more, err := s.app.Notify.List(ctx, notify.ListOptions{Limit: h.QueryInt(r, "limit", 30),
			Before: int64(h.QueryInt(r, "before", 0)), Unread: r.URL.Query().Get("unread") == "1"})
		if err != nil {
			return nil, err
		}
		// who caused it: the id only for a recipient who may open the person
		var ids []int64
		for _, n := range list {
			if n.ActorUser != nil {
				ids = append(ids, *n.ActorUser)
			}
		}
		refs, err := s.app.Content.PeopleRefs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for i, n := range list {
			if n.ActorUser != nil {
				if ref, ok := refs[*n.ActorUser]; ok {
					list[i].ActorID = ref.ID
				}
			}
		}
		return map[string]any{"items": list, "unread": unread, "more": more}, nil
	})
	// action: core.me.inbox.popups
	s.handle("GET /api/v1/me/inbox/popups", func(w http.ResponseWriter, r *http.Request) (any, error) {
		list, err := s.app.Notify.Popups(r.Context(), int(limits.Value(s.app.Config.Get(), limits.NotifyPopups)))
		return map[string]any{"items": list}, err
	})
	// action: core.me.inbox.read
	s.handle("POST /api/v1/me/inbox/read", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			IDs []int64 `json:"ids"` // empty: all
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if len(in.IDs) == 0 {
			in.IDs = nil
		}
		return h.NoContent{}, s.app.Notify.MarkRead(r.Context(), in.IDs)
	})
	// action: core.me.inbox.delete
	s.handle("DELETE /api/v1/me/inbox/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Notify.Delete(r.Context(), id)
	})
}
