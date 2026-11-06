package server

// Demo content (Administration -> Demo): one click creates a demo area that
// shows as many features as possible; "remove demo" deletes everything it
// created without a trace. "Reset site" brings the whole site back to the
// state of its first start.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// demoTables maps the kinds of demo objects to their tables, in the order
// they are removed (dependent objects first).
var demoTables = []struct{ kind, table string }{
	{"comment", "plugin_comments"},
	{"event", "plugin_events"},
	{"carousel", "plugin_carousels"},
	{"block", "plugin_blocks"},
	{"menu", "plugin_menus"},
	{"medium", "media"},
	{"element", "elements"},
	{"category", "categories"},
	{"group", "groups"},
	{"user", "users"},
}

// demoEntity maps kinds to the entity types of the audit log.
var demoEntity = map[string][]string{
	"element": {"element"}, "category": {"category"}, "medium": {"medium", "media"},
	"user": {"user"}, "group": {"group"}, "menu": {"menu"}, "block": {"block"},
	"carousel": {"carousel"}, "event": {"event"}, "comment": {"comment"},
}

type demoStatus struct {
	Installed bool           `json:"installed"`
	CreatedAt *time.Time     `json:"created_at,omitempty"`
	Objects   map[string]int `json:"objects"`
	SiteName  string         `json:"site_name"`
}

func (s *Server) routesDemo() {
	s.handle("GET /api/v1/admin/demo", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		return s.demoStatus(r.Context())
	})
	s.handle("POST /api/v1/admin/demo", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		if err := s.createDemo(r.Context()); err != nil {
			return nil, err
		}
		return s.demoStatus(r.Context())
	})
	s.handle("DELETE /api/v1/admin/demo", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		if err := s.removeDemo(r.Context()); err != nil {
			return nil, err
		}
		return s.demoStatus(r.Context())
	})
	s.handle("POST /api/v1/admin/reset", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		var in struct {
			Confirm  string `json:"confirm"`
			Password string `json:"password"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if err := s.resetSite(r.Context(), in.Confirm, in.Password); err != nil {
			return nil, err
		}
		return map[string]string{"status": "reset"}, nil
	})
}

func (s *Server) demoStatus(ctx context.Context) (*demoStatus, error) {
	st := &demoStatus{Objects: map[string]int{}, SiteName: s.app.Config.Get().UI.SiteName}
	rows, err := s.app.DB.Query(ctx, `SELECT kind, count(*), min(created_at) FROM demo_objects GROUP BY kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		var at time.Time
		if err := rows.Scan(&k, &n, &at); err != nil {
			return nil, err
		}
		st.Objects[k] = n
		if st.CreatedAt == nil || at.Before(*st.CreatedAt) {
			st.CreatedAt = &at
		}
	}
	st.Installed = len(st.Objects) > 0
	return st, rows.Err()
}

// ---------------------------------------------------------------- creating

// demoRun executes API calls in-process with the rights of the
// administrator who started the demo and records what they created.
type demoRun struct {
	s       *Server
	ctx     context.Context
	langs   []string // active languages, default first
	err     error
	status  int      // HTTP status of the failed call
	skipped []string // optional parts that were not possible
}

// optional runs a part of the demo that depends on plugins, settings or
// permissions: if it is refused (plugin off, no permission), the demo goes
// on without it.
func (d *demoRun) optional(name string, fn func()) {
	if d.err != nil {
		return
	}
	fn()
	if d.err != nil && (d.status == http.StatusForbidden || d.status == http.StatusNotFound || d.status == http.StatusBadRequest) {
		d.s.log.Info("demo: part skipped", "part", name, "err", d.err)
		d.skipped = append(d.skipped, name)
		d.err, d.status = nil, 0
	}
}

func (d *demoRun) track(kind string, ref any) {
	if d.err != nil {
		return
	}
	_, err := d.s.app.DB.Exec(d.ctx, `INSERT INTO demo_objects(kind, ref) VALUES ($1,$2) ON CONFLICT DO NOTHING`, kind, fmt.Sprint(ref))
	if err != nil {
		d.err = err
	}
}

// call performs one API request; out receives the JSON answer.
func (d *demoRun) call(method, path string, body any, out any) {
	if d.err != nil {
		return
	}
	var rd io.Reader
	ct := "application/json"
	switch b := body.(type) {
	case nil:
	case *multipartBody:
		rd, ct = b.buf, b.ct
	default:
		data, err := json.Marshal(b)
		if err != nil {
			d.err = err
			return
		}
		rd = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, rd).WithContext(d.ctx)
	req.Header.Set("Content-Type", ct)
	if host := d.s.app.Config.Get().Server.BaseURL; host != "" {
		if i := strings.Index(host, "://"); i >= 0 {
			req.Host = strings.SplitN(host[i+3:], "/", 2)[0]
		}
	}
	rec := httptest.NewRecorder()
	d.s.Handler().ServeHTTP(rec, req)
	if rec.Code >= 300 {
		d.status = rec.Code
		d.err = fmt.Errorf("demo: %s %s: %d %s", method, path, rec.Code, strings.TrimSpace(rec.Body.String()))
		return
	}
	if out != nil && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			d.err = fmt.Errorf("demo: %s %s: %w", method, path, err)
		}
	}
}

type idResp struct {
	ID int64 `json:"id"`
}

// create calls POST and records the new object.
func (d *demoRun) create(kind, path string, body any) int64 {
	var r idResp
	d.call(http.MethodPost, path, body, &r)
	if d.err == nil {
		if r.ID == 0 {
			d.err = fmt.Errorf("demo: POST %s returned no id", path)
			return 0
		}
		d.track(kind, r.ID)
	}
	return r.ID
}

type multipartBody struct {
	buf *bytes.Buffer
	ct  string
}

func multipartFile(meta any, name string, data []byte) *multipartBody {
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	if meta != nil {
		m, _ := json.Marshal(meta)
		_ = mw.WriteField("meta", string(m))
	}
	fw, _ := mw.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = mw.Close()
	return &multipartBody{buf: buf, ct: mw.FormDataContentType()}
}

func (s *Server) createDemo(ctx context.Context) error {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return apperr.BadRequest("the demo needs an administrator account (CLI: --as USER)")
	}
	st, err := s.demoStatus(ctx)
	if err != nil {
		return err
	}
	if st.Installed {
		return apperr.New(http.StatusConflict, "demo_installed", "the demo is already installed")
	}
	dp := *p
	dp.Via = "demo" // in-process, exactly the administrator's rights
	d := &demoRun{s: s, ctx: identity.With(ctx, &dp)}
	cfg := s.app.Config.Get()
	d.langs = append([]string{cfg.I18n.DefaultLanguage}, slices.DeleteFunc(slices.Clone(cfg.I18n.Languages), func(l string) bool { return l == cfg.I18n.DefaultLanguage })...)
	start := time.Now().Add(-time.Second)
	buildDemo(d)
	// files the demo uploaded (removed together with the demo)
	if d.err == nil {
		rows, err := s.app.DB.Query(ctx, `SELECT hash FROM files WHERE created_by=$1 AND created_at >= $2`, p.UserID, start)
		if err == nil {
			hashes, cerr := pgx.CollectRows(rows, pgx.RowTo[string])
			err = cerr
			for _, hs := range hashes {
				d.track("file", hs)
			}
		}
		if err != nil && d.err == nil {
			d.err = err
		}
	}
	if d.err != nil {
		s.log.Warn("demo: creation failed, removing the partial demo", "err", d.err)
		if rerr := s.removeDemo(ctx); rerr != nil {
			s.log.Error("demo: removing the partial demo failed", "err", rerr)
		}
		return apperr.New(http.StatusInternalServerError, "demo_failed", d.err.Error())
	}
	s.app.Bus.Publish(ctx, "*", "")
	return audit.Log(ctx, s.app.DB, "demo.create", "site", 0, nil, nil)
}

// ---------------------------------------------------------------- removing

func (s *Server) removeDemo(ctx context.Context) error {
	refs := map[string][]string{}
	rows, err := s.app.DB.Query(ctx, `SELECT kind, ref FROM demo_objects ORDER BY seq`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k, r string
		if err := rows.Scan(&k, &r); err != nil {
			rows.Close()
			return err
		}
		refs[k] = append(refs[k], r)
	}
	rows.Close()
	if len(refs) == 0 {
		return nil
	}
	ids := func(k string) []int64 {
		var out []int64
		for _, r := range refs[k] {
			if n, err := strconv.ParseInt(r, 10, 64); err == nil {
				out = append(out, n)
			}
		}
		return out
	}
	err = pgx.BeginFunc(ctx, s.app.DB, func(tx pgx.Tx) error {
		// the whole demo area: also what others added inside it
		if cats := ids("category"); len(cats) > 0 {
			tree := `WITH RECURSIVE t AS (SELECT id FROM categories WHERE id = ANY($1)
				UNION SELECT c.id FROM categories c JOIN t ON c.parent_id = t.id) `
			for _, q := range []struct{ kind, sql string }{
				{"category", tree + `SELECT id::text FROM t`},
				{"element", tree + `SELECT e.id::text FROM elements e WHERE e.category_id IN (SELECT id FROM t)`},
			} {
				rows, err := tx.Query(ctx, q.sql, cats)
				if err != nil {
					return err
				}
				more, err := pgx.CollectRows(rows, pgx.RowTo[string])
				if err != nil {
					return err
				}
				for _, m := range more {
					if !slices.Contains(refs[q.kind], m) {
						refs[q.kind] = append(refs[q.kind], m)
					}
				}
			}
		}
		// references by type and id (no foreign keys)
		if err := content.RemoveRefs(ctx, tx, ids("element"), ids("category")); err != nil {
			return err
		}
		for _, t := range demoTables {
			list := ids(t.kind)
			if len(list) == 0 {
				continue
			}
			if t.kind == "category" {
				// children first; the whole subtree belongs to the demo
				// (also what others added inside the demo area)
				tree := `WITH RECURSIVE t AS (SELECT id FROM categories WHERE id = ANY($1)
					UNION SELECT c.id FROM categories c JOIN t ON c.parent_id = t.id) `
				if _, err := tx.Exec(ctx, tree+`DELETE FROM elements WHERE category_id IN (SELECT id FROM t)`, list); err != nil {
					return fmt.Errorf("demo elements of categories: %w", err)
				}
				if _, err := tx.Exec(ctx, tree+`DELETE FROM categories WHERE id IN (SELECT id FROM t)`, list); err != nil {
					return fmt.Errorf("demo categories: %w", err)
				}
				continue
			}
			q := `DELETE FROM ` + pgx.Identifier{t.table}.Sanitize() + ` WHERE id = ANY($1)`
			if t.kind == "user" {
				q += ` AND provider = 'demo'` // never a real account
			}
			if _, err := tx.Exec(ctx, q, list); err != nil {
				return fmt.Errorf("demo %s: %w", t.kind, err)
			}
		}
		// the change log of the search: one entry per removed element, so an
		// external search engine removes it too (none without one)
		if list := ids("element"); len(list) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM search_changes WHERE element_id = ANY($1)`, list); err != nil {
				return err
			}
			var external bool
			if err := tx.QueryRow(ctx, `SELECT to_regclass('plugin_search_state') IS NOT NULL AND EXISTS (SELECT 1 FROM plugin_search_state)`).Scan(&external); err != nil {
				return err
			}
			if external {
				if _, err := tx.Exec(ctx, `INSERT INTO search_changes(element_id) SELECT unnest($1::bigint[])`, list); err != nil {
					return err
				}
			}
		}
		// traces in the audit log
		for kind, types := range demoEntity {
			if list := ids(kind); len(list) > 0 {
				if _, err := tx.Exec(ctx, `DELETE FROM audit_log WHERE entity_type = ANY($1) AND entity_id = ANY($2)`, types, list); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM audit_log WHERE action = 'demo.create'`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM demo_objects`)
		return err
	})
	if err != nil {
		return err
	}
	if _, err := s.app.Files.RemoveUnreferenced(ctx, refs["file"]); err != nil {
		return err
	}
	s.app.Bus.Publish(ctx, "*", "")
	s.app.Cache.Invalidate(ctx, "demo")
	return audit.Log(ctx, s.app.DB, "demo.remove", "site", 0, nil, nil)
}

// ---------------------------------------------------------------- site reset

// resetSite deletes all content, accounts (except the caller's) and
// settings of the site, as at its first start. confirm must be the site's
// name; local accounts confirm with their password.
func (s *Server) resetSite(ctx context.Context, confirm, password string) error {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return apperr.BadRequest("the reset needs the administrator who stays (CLI: --as USER)")
	}
	switch p.Via {
	case "session", "ssh", "cli-as":
	default:
		return apperr.New(http.StatusForbidden, "not_allowed", "a site reset needs a signed-in administrator (no API key)")
	}
	cfg := s.app.Config.Get()
	if strings.TrimSpace(confirm) != cfg.UI.SiteName {
		return apperr.BadRequest("type the name of the site to confirm")
	}
	if p.Via != "cli-as" && p.Provider == "local" {
		var hash *string
		if err := s.app.DB.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, p.UserID).Scan(&hash); err != nil {
			return err
		}
		if hash == nil || !auth.VerifyPassword(*hash, password) {
			return apperr.New(http.StatusForbidden, "wrong_password", "wrong password")
		}
	}
	// the demo first: its accounts may live in the shared schema
	if err := s.removeDemo(ctx); err != nil {
		return err
	}
	shared := !s.app.SeparateAccounts
	var others int
	if err := s.app.Instance.Shared.QueryRow(ctx, `SELECT count(*) FROM sites WHERE name <> $1`, s.app.Site).Scan(&others); err != nil {
		return err
	}
	firstStart := !shared || others == 0 // the accounts belong to this site alone
	err := pgx.BeginFunc(ctx, s.app.DB, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = current_schema()
			AND tablename NOT IN ('schema_migrations', 'users') ORDER BY tablename`)
		if err != nil {
			return err
		}
		tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		if len(tables) > 0 {
			names := make([]string, len(tables))
			for i, t := range tables {
				names[i] = pgx.Identifier{t}.Sanitize()
			}
			if _, err := tx.Exec(ctx, `TRUNCATE `+strings.Join(names, ", ")+` RESTART IDENTITY CASCADE`); err != nil {
				return fmt.Errorf("reset: %w", err)
			}
		}
		if firstStart {
			// accounts: only the administrator who resets stays
			q := `DELETE FROM users WHERE id <> $1`
			if shared {
				q = `DELETE FROM public.users WHERE id <> $1`
				for _, t := range []string{"public.user_groups", "public.groups", "public.roles"} {
					if _, err := tx.Exec(ctx, `DELETE FROM `+t); err != nil {
						return fmt.Errorf("reset %s: %w", t, err)
					}
				}
			}
			if _, err := tx.Exec(ctx, q, p.UserID); err != nil {
				return fmt.Errorf("reset accounts: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := s.app.Files.Wipe(); err != nil {
		return err
	}
	// settings of the site as at the first start (instance settings, address,
	// languages, e-mail and the name stay)
	if err := s.app.Config.Update(func(c *config.Config) error {
		d := config.Defaults()
		d.Mode, d.Database, d.License, d.Sites, d.Server, d.Storage = c.Mode, c.Database, c.License, c.Sites, c.Server, c.Storage
		d.I18n, d.Mail = c.I18n, c.Mail
		d.UI.SiteName = c.UI.SiteName
		*c = *d
		return nil
	}); err != nil {
		return err
	}
	// roles, the administrator and the welcome space, as "ewiki setup" does
	adminRole, err := s.app.Auth.EnsureRole(ctx, "Administrators", "Full access to everything", true)
	if err != nil {
		return err
	}
	editors, err := s.app.Auth.EnsureRole(ctx, "Editors", "Create and edit content where granted", false)
	if err != nil {
		return err
	}
	if _, err := s.app.Auth.EnsureRole(ctx, "Readers", "Read content where granted", false); err != nil {
		return err
	}
	if _, err := s.app.DB.Exec(ctx, `INSERT INTO user_roles(user_id, role_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, p.UserID, adminRole); err != nil {
		return err
	}
	s.app.Bus.Publish(ctx, "*", "")
	s.app.Cache.Invalidate(ctx, "reset")
	sctx := identity.With(ctx, identity.CLI())
	if err := s.app.SeedWelcome(sctx, editors); err != nil {
		return err
	}
	s.app.Bus.Publish(ctx, "*", "")
	return audit.Log(ctx, s.app.DB, "site.reset", "site", 0, nil, map[string]any{"by": p.Handle()})
}
