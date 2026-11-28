package server

import (
	"fmt"
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/feed"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// routesFeeds registers the feeds of categories and the personal feed keys,
// with which feed readers and calendar apps read without signing in.
func (s *Server) routesFeeds() {
	// ---------------------------------------------------------------- feed keys
	// A person's keys for personal feed links (auth/feedkeys.go): only with a
	// session, never with an API key or a feed key; the key is in the answer
	// of POST only.
	// action: core.me.feed_keys.list
	s.handle("GET /api/v1/me/feed-keys", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.app.Auth.FeedKeys(r.Context())
	})
	// action: core.me.feed_keys.create
	s.handle("POST /api/v1/me/feed-keys", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Name string `json:"name"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		k, err := s.app.Auth.CreateFeedKey(r.Context(), in.Name)
		return h.Created{V: k}, err
	})
	// action: core.me.feed_keys.delete
	s.handle("DELETE /api/v1/me/feed-keys/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.DeleteFeedKey(r.Context(), id)
	})
	// action: core.me.feed_keys.delete_all
	s.handle("DELETE /api/v1/me/feed-keys", func(w http.ResponseWriter, r *http.Request) (any, error) {
		n, err := s.app.Auth.DeleteFeedKeys(r.Context())
		return map[string]int64{"deleted": n}, err
	})
	// action: core.users.feed_keys.list
	s.handle("GET /api/v1/admin/users/{id}/feed-keys", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Auth.UserFeedKeys(r.Context(), id)
	})
	// action: core.users.feed_keys.delete_all
	s.handle("DELETE /api/v1/admin/users/{id}/feed-keys", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		n, err := s.app.Auth.DeleteUserFeedKeys(r.Context(), id)
		return map[string]int64{"deleted": n}, err
	})
	// ---------------------------------------------------------------- feeds
	// action: core.feeds.category
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
		list, _, err := s.app.Content.ListIn(ctx, content.ListOptions{Categories: scope, Lang: lang, Order: "updated", Limit: min(max(h.QueryInt(r, "limit", 30), 1), 200), WithContent: q.Get("content") == "1", NoConfidential: true})
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
}
