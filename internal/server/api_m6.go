package server

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/feed"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/watch"
)

func (s *Server) routesM6() {
	// ---------------------------------------------------------------- watches
	s.handle("GET /api/v1/me/watches", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := r.URL.Query()
		if t := q.Get("target_type"); t != "" {
			id, err := strconv.ParseInt(q.Get("target_id"), 10, 64)
			if err != nil {
				return nil, apperr.BadRequest("target_id required")
			}
			wt, err := s.app.Watch.Get(r.Context(), t, id)
			if err != nil {
				return nil, err
			}
			return map[string]any{"watch": wt, "enabled": s.app.Mail.Enabled()}, nil
		}
		list, err := s.app.Watch.List(r.Context())
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": list, "enabled": s.app.Mail.Enabled()}, nil
	})
	s.handle("PUT /api/v1/me/watches", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in watch.Input
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Watch.Set(r.Context(), in)
	})
	s.handle("DELETE /api/v1/me/watches/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Watch.Delete(r.Context(), id)
	})

	// ---------------------------------------------------------------- feeds
	s.handle("GET /api/v1/me/feed-token", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		tok, err := s.app.Auth.FeedToken(r.Context(), p.ID())
		return map[string]string{"token": tok}, err
	})
	s.handle("POST /api/v1/me/feed-token", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		tok, err := s.app.Auth.RotateFeedToken(r.Context(), p.ID())
		return map[string]string{"token": tok}, err
	})
	s.handle("DELETE /api/v1/me/feed-token", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.RevokeFeedToken(r.Context(), p.ID())
	})
	s.handle("GET /api/v1/feeds/categories/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx, private, err := s.app.Auth.FeedContext(r)
		if err != nil {
			return nil, err
		}
		c, err := s.app.Content.GetCategory(ctx, id)
		if err != nil {
			return nil, err
		}
		if c.Feed == "off" {
			return nil, apperr.NotFound("feed (switch it on in the category settings)")
		}
		scope, err := s.app.Content.Scope(ctx, id, c.Feed == "recursive")
		if err != nil {
			return nil, err
		}
		q := r.URL.Query()
		lang := q.Get("lang")
		if lang == "" {
			lang = s.app.Content.DefaultLanguage()
		}
		list, _, err := s.app.Content.ListIn(ctx, content.ListOptions{Categories: scope, Lang: lang, Order: "updated", Limit: h.QueryInt(r, "limit", 30), WithContent: q.Get("content") == "1"})
		if err != nil {
			return nil, err
		}
		base := feed.BaseURL(s.app.Config.Get().Server.BaseURL, r)
		f := feed.Feed{ID: fmt.Sprintf("%s/c/%d", base, id), Title: feed.Pick(c.Titles, lang) + " – " + s.app.Config.Get().UI.SiteName,
			Subtitle: feed.Pick(c.Descriptions, lang), Link: fmt.Sprintf("%s/c/%d", base, id), Self: feed.SelfURL(base, r), Lang: lang, Updated: c.UpdatedAt}
		f.Entries = feed.FromListing(base, list)
		if err := feed.Write(w, r, f, q.Get("format"), private); err != nil {
			return nil, apperr.BadRequest("%v", err)
		}
		return h.Handled{}, nil
	})

	// ---------------------------------------------------------------- profile (business card)
	s.handle("GET /api/v1/me/profile", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		prof, err := s.app.Auth.Profile(r.Context(), p.ID())
		return map[string]any{"profile": prof, "fields": auth.ProfileFields, "photo_policy": s.app.Auth.PhotoPolicy()}, err
	})
	s.handle("PUT /api/v1/me/profile", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		var in map[string]string
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Auth.SetProfile(r.Context(), p.ID(), in)
	})
	s.handle("GET /api/v1/admin/users/{id}/profile", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		prof, err := s.app.Auth.Profile(r.Context(), id)
		return map[string]any{"profile": prof, "fields": auth.ProfileFields, "photo_policy": s.app.Auth.PhotoPolicy()}, err
	})
	s.handle("PUT /api/v1/admin/users/{id}/profile", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in map[string]string
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return s.app.Auth.SetProfile(r.Context(), id, in)
	})

	// ---------------------------------------------------------------- guest cache
	s.handle("GET /api/v1/admin/cache", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return s.app.Cache.GetStatus(), nil
	})
	s.handle("DELETE /api/v1/admin/cache", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		s.app.Cache.Invalidate(r.Context(), "admin")
		return h.NoContent{}, nil
	})
}
