// Package core_comments provides comments and replies below pages.
//
// Reading requires the View permission on the page's category; writing
// requires a signed-in user and (configurable) View or Edit. Authors can edit
// and delete their own comments; users with the Manage permission and
// administrators can delete every comment. Nothing is lost: edits and
// deletions are written to the audit log, deleted comments stay as
// placeholders.
package core_comments

import (
	"context"
	"embed"
	"errors"
	"html"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"

	_ "embed"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// MaxLen is the maximum comment length in characters.
const MaxLen = 10000

// Plugin is the comments plugin.
type Plugin struct{ host *sdk.Host }

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Comment is a comment as returned by the API.
type Comment struct {
	ID        int64      `json:"id"`
	ElementID int64      `json:"element_id"`
	ParentID  *int64     `json:"parent_id"`
	AuthorID  *int64     `json:"-"`
	Author    string     `json:"author"`
	Body      string     `json:"body"`
	HTML      string     `json:"html"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Edited    bool       `json:"edited"`
	DeletedAt *time.Time `json:"deleted_at"`
	CanEdit   bool       `json:"can_edit"`
	CanDelete bool       `json:"can_delete"`
}

var linkRe = regexp.MustCompile(`https?://[^\s<>"']+[^\s<>"'.,;:!?)\]]`)

// Format renders plain text: escaped, paragraphs, line breaks and links.
func Format(s string) string {
	var b strings.Builder
	for _, para := range regexp.MustCompile(`\n\s*\n`).Split(strings.TrimSpace(s), -1) {
		if para = strings.TrimSpace(para); para == "" {
			continue
		}
		esc := html.EscapeString(para)
		esc = linkRe.ReplaceAllStringFunc(esc, func(u string) string {
			return `<a href="` + u + `" rel="nofollow noopener external">` + u + `</a>`
		})
		b.WriteString("<p>" + strings.ReplaceAll(esc, "\n", "<br>") + "</p>")
	}
	return b.String()
}

func (p *Plugin) minPerm() acl.Perm {
	if v, _ := p.host.Settings()["write_permission"].(string); v == "edit" {
		return acl.Edit
	}
	return acl.View
}

func (p *Plugin) get(ctx context.Context, id int64) (*Comment, int64, error) {
	var c Comment
	err := p.host.DB.QueryRow(ctx, `SELECT id, element_id, parent_id, author_id, author, body, created_at, updated_at, edited, deleted_at
		FROM plugin_comments WHERE id=$1`, id).Scan(&c.ID, &c.ElementID, &c.ParentID, &c.AuthorID, &c.Author, &c.Body, &c.CreatedAt, &c.UpdatedAt, &c.Edited, &c.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, apperr.NotFound("comment")
	}
	if err != nil {
		return nil, 0, err
	}
	cat, err := p.host.Content.ElementCategory(ctx, c.ElementID, acl.View)
	if err != nil {
		return nil, 0, apperr.NotFound("comment")
	}
	return &c, cat, nil
}

func (p *Plugin) decorate(ctx context.Context, c *Comment, perms acl.Perm) {
	me := identity.From(ctx)
	own := me != nil && c.AuthorID != nil && *c.AuthorID == me.ID()
	if c.DeletedAt != nil {
		c.Body, c.HTML = "", ""
		return
	}
	c.HTML = Format(c.Body)
	c.CanEdit = own && perms&p.minPerm() != 0
	c.CanDelete = own || perms&acl.Manage != 0 || me.Admin()
}

func validBody(s string) (string, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if s == "" {
		return "", apperr.BadRequest("the comment is empty")
	}
	if utf8.RuneCountInString(s) > MaxLen {
		return "", apperr.BadRequest("a comment can have at most %d characters", MaxLen)
	}
	return s, nil
}

// Setup registers the API.
func (p *Plugin) Setup(host *sdk.Host) error {
	p.host = host

	// GET /elements/{id}/comments lists the comments of a page (oldest first).
	host.Handle("GET", "/elements/{id}/comments", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		cat, err := host.Content.ElementCategory(ctx, id, acl.View)
		if err != nil {
			return nil, err
		}
		rows, err := host.DB.Query(ctx, `SELECT id, element_id, parent_id, author_id, author, body, created_at, updated_at, edited, deleted_at
			FROM plugin_comments WHERE element_id=$1 ORDER BY id LIMIT 2000`, id)
		if err != nil {
			return nil, err
		}
		list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Comment, error) {
			var c Comment
			return &c, r.Scan(&c.ID, &c.ElementID, &c.ParentID, &c.AuthorID, &c.Author, &c.Body, &c.CreatedAt, &c.UpdatedAt, &c.Edited, &c.DeletedAt)
		})
		if err != nil {
			return nil, err
		}
		perms, err := host.ACL.Perms(ctx, identity.From(ctx), cat)
		if err != nil {
			return nil, err
		}
		for _, c := range list {
			p.decorate(ctx, c, perms)
		}
		me := identity.From(ctx)
		return map[string]any{"comments": list, "can_comment": me != nil && me.ID() != 0 && perms&p.minPerm() != 0}, nil
	})

	// POST /elements/{id}/comments adds a comment: {"body": "...", "parent_id": null}.
	host.Handle("POST", "/elements/{id}/comments", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		me := identity.From(ctx)
		if me == nil || me.ID() == 0 {
			return nil, apperr.ErrUnauthorized
		}
		cat, err := host.Content.ElementCategory(ctx, id, p.minPerm())
		if err != nil {
			return nil, err
		}
		var in struct {
			Body     string `json:"body"`
			ParentID *int64 `json:"parent_id"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		body, err := validBody(in.Body)
		if err != nil {
			return nil, err
		}
		if in.ParentID != nil {
			// replies attach to the top level comment of the thread
			var el int64
			var pp *int64
			if err := host.DB.QueryRow(ctx, `SELECT element_id, parent_id FROM plugin_comments WHERE id=$1`, *in.ParentID).Scan(&el, &pp); err != nil || el != id {
				return nil, apperr.BadRequest("invalid parent comment")
			}
			if pp != nil {
				in.ParentID = pp
			}
		}
		var c Comment
		err = pgx.BeginFunc(ctx, host.DB, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `INSERT INTO plugin_comments(element_id, parent_id, author_id, author, body) VALUES ($1,$2,$3,$4,$5)
				RETURNING id, element_id, parent_id, author_id, author, body, created_at, updated_at, edited, deleted_at`,
				id, in.ParentID, me.ID(), displayName(me), body).Scan(&c.ID, &c.ElementID, &c.ParentID, &c.AuthorID, &c.Author, &c.Body,
				&c.CreatedAt, &c.UpdatedAt, &c.Edited, &c.DeletedAt); err != nil {
				return err
			}
			return audit.Log(ctx, tx, "comment.create", "comment", c.ID, nil, map[string]any{"element_id": id, "body": body})
		})
		if err != nil {
			return nil, err
		}
		perms, _ := host.ACL.Perms(ctx, me, cat)
		p.decorate(ctx, &c, perms)
		return h.Created{V: &c}, nil
	})

	// PUT /comments/{id} edits an own comment: {"body": "..."}.
	host.Handle("PUT", "/comments/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		c, cat, err := p.get(ctx, id)
		if err != nil {
			return nil, err
		}
		perms, err := host.ACL.Perms(ctx, identity.From(ctx), cat)
		if err != nil {
			return nil, err
		}
		p.decorate(ctx, c, perms)
		if !c.CanEdit {
			return nil, apperr.ErrForbidden
		}
		var in struct {
			Body string `json:"body"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		body, err := validBody(in.Body)
		if err != nil {
			return nil, err
		}
		before := c.Body
		err = pgx.BeginFunc(ctx, host.DB, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `UPDATE plugin_comments SET body=$2, edited=true, updated_at=now() WHERE id=$1 RETURNING updated_at`, id, body).Scan(&c.UpdatedAt); err != nil {
				return err
			}
			return audit.Log(ctx, tx, "comment.update", "comment", id, map[string]string{"body": before}, map[string]string{"body": body})
		})
		if err != nil {
			return nil, err
		}
		c.Body, c.Edited = body, true
		p.decorate(ctx, c, perms)
		return c, nil
	})

	// DELETE /comments/{id} deletes a comment (author, Manage permission or admin).
	host.Handle("DELETE", "/comments/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		c, cat, err := p.get(ctx, id)
		if err != nil {
			return nil, err
		}
		if c.DeletedAt != nil {
			return h.NoContent{}, nil
		}
		perms, err := host.ACL.Perms(ctx, identity.From(ctx), cat)
		if err != nil {
			return nil, err
		}
		body := c.Body
		p.decorate(ctx, c, perms)
		if !c.CanDelete {
			return nil, apperr.ErrForbidden
		}
		err = pgx.BeginFunc(ctx, host.DB, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE plugin_comments SET deleted_at=now(), deleted_by=$2 WHERE id=$1`, id, identity.From(ctx).Handle()); err != nil {
				return err
			}
			return audit.Log(ctx, tx, "comment.delete", "comment", id, map[string]any{"element_id": c.ElementID, "author": c.Author, "body": body}, nil)
		})
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, nil
	})
	return nil
}

func displayName(p *identity.Principal) string {
	if p.DisplayName != "" {
		return p.DisplayName + " (" + p.Handle() + ")"
	}
	return p.Handle()
}
