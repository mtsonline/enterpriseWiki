package server

import (
	"fmt"
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/feed"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// routesFeeds registers the feeds of categories and the personal feed key,
// with which feed readers and calendar apps read without signing in.
func (s *Server) routesFeeds() {
	// ---------------------------------------------------------------- feeds
	// action: core.me.feed_token.show
	s.handle("GET /api/v1/me/feed-token", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		tok, err := s.app.Auth.FeedToken(r.Context(), p.ID())
		return map[string]string{"token": tok}, err
	})
	// action: core.me.feed_token.renew
	s.handle("POST /api/v1/me/feed-token", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		tok, err := s.app.Auth.RotateFeedToken(r.Context(), p.ID())
		return map[string]string{"token": tok}, err
	})
	// action: core.me.feed_token.delete
	s.handle("DELETE /api/v1/me/feed-token", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, s.app.Auth.RevokeFeedToken(r.Context(), p.ID())
	})
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
