package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/notify"
)

// Access requests: who may not see a page or category asks for access; the
// people who may manage the category get a notification and grant (a
// normal permission rule, visible in the permission management) or decline.
// The answer to the request never reveals whether the target exists.

func init() {
	notify.RegisterKind("access_request", "")
	notify.RegisterKind("access_answer", "")
}

const maxAccessRequestsPerDay = 10

// dayLimiter counts attempts per user in the last 24 hours (per node).
type dayLimiter struct {
	mu   sync.Mutex
	hits map[int64][]time.Time
}

func (l *dayLimiter) allow(uid int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := time.Now().Add(-24 * time.Hour)
	if len(l.hits) > 10000 { // forget old entries now and then
		for k, v := range l.hits {
			if len(v) == 0 || v[len(v)-1].Before(cut) {
				delete(l.hits, k)
			}
		}
	}
	keep := l.hits[uid][:0]
	for _, t := range l.hits[uid] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	if len(keep) >= maxAccessRequestsPerDay {
		l.hits[uid] = keep
		return false
	}
	l.hits[uid] = append(keep, time.Now())
	return true
}

type accessRequest struct {
	ID         int64     `json:"id"`
	UserID     int64     `json:"user_id"`
	User       string    `json:"user"`
	CategoryID int64     `json:"category_id"`
	Message    string    `json:"message"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Server) routesAccess() {
	limiter := &dayLimiter{hits: map[int64][]time.Time{}} // per site
	// POST /api/v1/access-requests {"type": "category"|"element", "id": 12, "message": "…"}
	s.handle("POST /api/v1/access-requests", func(w http.ResponseWriter, r *http.Request) (any, error) {
		me, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		var in struct {
			Type    string `json:"type"`
			ID      int64  `json:"id"`
			Message string `json:"message"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		in.Message = strings.TrimSpace(in.Message)
		if utf8.RuneCountInString(in.Message) > 1000 {
			return nil, apperr.BadRequest("the message has at most 1000 characters")
		}
		if in.Type != "category" && in.Type != "element" {
			return nil, apperr.BadRequest("type must be category or element")
		}
		ctx := r.Context()
		// every attempt counts (also for targets that do not exist)
		if !limiter.allow(me.ID()) {
			return nil, apperr.TooMany("at most %d access requests per day", maxAccessRequestsPerDay)
		}
		if err := s.requestAccess(ctx, me, in.Type, in.ID, in.Message); err != nil {
			s.log.Warn("access request", "err", err)
		}
		// the same answer whether the target exists or not
		return map[string]any{"sent": true}, nil
	})
	// GET /api/v1/categories/{id}/access-requests – open requests (Manage)
	s.handle("GET /api/v1/categories/{id}/access-requests", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		if err := s.app.Content.Require(r.Context(), id, acl.Manage); err != nil {
			return nil, err
		}
		rows, err := s.app.DB.Query(r.Context(), `SELECT a.id, a.user_id, COALESCE(NULLIF(u.display_name, ''), u.username), a.category_id, a.message, a.status, a.created_at
			FROM access_requests a JOIN users u ON u.id = a.user_id WHERE a.category_id=$1 AND a.status='open' ORDER BY a.created_at`, id)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowToStructByPos[accessRequest])
	})
	// POST /api/v1/access-requests/{id} {"grant": true, "preset": "reader"|"editor"} or {"grant": false}
	s.handle("POST /api/v1/access-requests/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in struct {
			Grant  bool   `json:"grant"`
			Preset string `json:"preset"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return h.NoContent{}, s.answerAccess(r.Context(), id, in.Grant, in.Preset)
	})
}

// requestAccess stores a request and notifies the managers (silently does
// nothing for missing targets or readers who already have access).
func (s *Server) requestAccess(ctx context.Context, me *identity.Principal, typ string, id int64, msg string) error {
	cat := id
	if typ == "element" {
		if err := s.app.DB.QueryRow(ctx, `SELECT category_id FROM elements WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&cat); err != nil {
			return nil
		}
	}
	var titles map[string]string
	if err := s.app.DB.QueryRow(ctx, `SELECT titles FROM categories WHERE id=$1 AND deleted_at IS NULL`, cat).Scan(&titles); err != nil {
		return nil
	}
	if s.app.ACL.Can(ctx, me, cat, acl.View) {
		return nil
	}
	var open bool
	if err := s.app.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM access_requests WHERE user_id=$1 AND category_id=$2 AND status='open')`, me.ID(), cat).Scan(&open); err != nil || open {
		return err
	}
	var rid int64
	if err := s.app.DB.QueryRow(ctx, `INSERT INTO access_requests(user_id, category_id, message) VALUES ($1,$2,$3) RETURNING id`, me.ID(), cat, msg).Scan(&rid); err != nil {
		return err
	}
	_ = audit.Log(ctx, s.app.DB, "access.request", "category", cat, nil, map[string]any{"request": rid})
	managers, err := s.managersOf(ctx, cat)
	if err != nil || len(managers) == 0 {
		return err
	}
	name := me.DisplayName
	if name == "" {
		name = me.Username
	}
	def := s.app.Config.Get().I18n.DefaultLanguage
	link := fmt.Sprintf("/c/%d/settings?tab=permissions", cat)
	_, err = s.app.Notify.Send(ctx, managers, notify.Message{Kind: "access_request", EntityType: "category", EntityID: cat, Link: link,
		Text: func(lang string) (string, string) {
			body, title := msg, pickTitle(titles, lang, def)
			if lang == "de" {
				return fmt.Sprintf("%s bittet um Zugriff auf „%s“", name, title), body
			}
			return fmt.Sprintf("%s asks for access to “%s”", name, title), body
		}})
	return err
}

// managersOf returns the users who may manage a category through rules on it
// or above (users, groups, roles; at most 50), without everybody-rules.
func (s *Server) managersOf(ctx context.Context, cat int64) ([]int64, error) {
	rows, err := s.app.DB.Query(ctx, `SELECT DISTINCT m.user_id FROM categories c
		JOIN categories a ON a.path @> c.path
		JOIN acl_rules r ON r.category_id = a.id AND (r.inherit OR a.id = c.id) AND r.permissions & 16 <> 0
		JOIN LATERAL (
			SELECT r.subject_id AS user_id WHERE r.subject_type = 'user'
			UNION SELECT ug.user_id FROM user_groups ug WHERE r.subject_type = 'group' AND ug.group_id = r.subject_id
			UNION SELECT ur.user_id FROM user_roles ur WHERE r.subject_type = 'role' AND ur.role_id = r.subject_id
		) m ON true
		JOIN users u ON u.id = m.user_id AND NOT u.disabled
		WHERE c.id = $1 LIMIT 200`, cat)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	out := []int64{}
	for _, uid := range ids {
		p, err := s.app.Auth.LoadPrincipal(ctx, uid)
		if err == nil && s.app.ACL.Can(ctx, p, cat, acl.Manage) {
			out = append(out, uid)
		}
		if len(out) == 50 {
			break
		}
	}
	return out, nil
}

// answerAccess grants (a permission rule for the user) or declines a request.
func (s *Server) answerAccess(ctx context.Context, id int64, grant bool, preset string) error {
	var req accessRequest
	err := s.app.DB.QueryRow(ctx, `SELECT a.id, a.user_id, '', a.category_id, a.message, a.status, a.created_at FROM access_requests a WHERE a.id=$1`, id).
		Scan(&req.ID, &req.UserID, &req.User, &req.CategoryID, &req.Message, &req.Status, &req.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("access request")
	}
	if err != nil {
		return err
	}
	if err := s.app.Content.Require(ctx, req.CategoryID, acl.Manage); err != nil {
		return err
	}
	if req.Status != "open" {
		return apperr.Conflict("the request was already answered")
	}
	status := "declined"
	if grant {
		if preset != "editor" {
			preset = "reader"
		}
		uid := req.UserID
		if _, err := s.app.Content.AddRule(ctx, req.CategoryID, content.RuleInput{SubjectType: "user", SubjectID: &uid, Preset: preset}); err != nil {
			return err
		}
		status = "granted"
	}
	if _, err := s.app.DB.Exec(ctx, `UPDATE access_requests SET status=$2, handled_by=$3, handled_at=now() WHERE id=$1 AND status='open'`, id, status, identity.From(ctx).ID()); err != nil {
		return err
	}
	_ = audit.Log(ctx, s.app.DB, "access."+status, "category", req.CategoryID, nil, map[string]any{"request": id, "user": req.UserID, "preset": preset})
	var titles map[string]string
	_ = s.app.DB.QueryRow(ctx, `SELECT titles FROM categories WHERE id=$1`, req.CategoryID).Scan(&titles)
	def := s.app.Config.Get().I18n.DefaultLanguage
	link := ""
	if grant {
		link = fmt.Sprintf("/c/%d", req.CategoryID)
	}
	_, _ = s.app.Notify.Send(ctx, []int64{req.UserID}, notify.Message{Kind: "access_answer", EntityType: "category", EntityID: req.CategoryID, Link: link,
		Text: func(lang string) (string, string) {
			title := pickTitle(titles, lang, def)
			switch {
			case grant && lang == "de":
				return fmt.Sprintf("Zugriff auf „%s“ gewährt", title), ""
			case grant:
				return fmt.Sprintf("Access to “%s” granted", title), ""
			case lang == "de":
				return "Ihre Zugriffsanfrage wurde abgelehnt", ""
			default:
				return "Your access request was declined", ""
			}
		}})
	return nil
}
