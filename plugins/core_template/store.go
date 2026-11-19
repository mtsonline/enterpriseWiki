package core_template

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

//go:embed examples/*.json
var examplesFS embed.FS

// Migrations implements coreplugin.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements coreplugin.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// ID is the plugin id; templates are "core.template/<key>".
const ID = "core.template"

// Design permission: create, copy, import and change templates.
const permDesign = "core.template.design"

// Template is a stored template (or a shipped example, read only).
type Template struct {
	ID string `json:"id"`
	// Key: the part after "core.template/" (in addresses: /templates/{key})
	Key       string            `json:"key"`
	Names     map[string]string `json:"names"`
	Design    json.RawMessage   `json:"design,omitempty"`
	BasedOn   string            `json:"based_on"`
	Version   int               `json:"version"`
	ReadOnly  bool              `json:"read_only"`
	UpdatedAt *time.Time        `json:"updated_at,omitempty"`
	UpdatedBy *int64            `json:"updated_by,omitempty"`
	DeletedAt *time.Time        `json:"deleted_at,omitempty"`
}

// examples are the shipped templates of data (read only, to copy).
var examples = func() []Template {
	names := map[string]map[string]string{
		"wiki": {"en": "Wiki (data)", "de": "Wiki (Daten)"},
		"cms":  {"en": "Website (data)", "de": "Website (Daten)"},
	}
	out := []Template{}
	for _, k := range []string{"wiki", "cms"} {
		raw, err := examplesFS.ReadFile("examples/" + k + ".json")
		if err != nil {
			panic(err)
		}
		d, err := ParseDesign(raw)
		if err != nil {
			panic("core_template: example " + k + ": " + err.Error())
		}
		norm, _ := json.Marshal(d)
		out = append(out, Template{ID: ID + "/example-" + k, Key: "example-" + k, Names: names[k], Design: norm, Version: 1, ReadOnly: true})
	}
	return out
}()

var nameLangRe = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$`)

func cleanNames(in map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for l, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if !nameLangRe.MatchString(l) || utf8.RuneCountInString(v) > limits.NameMax || strings.ContainsAny(v, "<>") {
			return nil, apperr.BadRequest("a name is plain text of 1 to %d characters per language", limits.NameMax)
		}
		out[l] = v
	}
	if len(out) == 0 {
		return nil, apperr.BadRequest("the template needs a name")
	}
	return out, nil
}

// keyOf reads the key of a template id or path value: a number (stored) or
// example-… (shipped).
func keyOf(s string) (int64, string, error) {
	s = strings.TrimPrefix(s, ID+"/")
	if strings.HasPrefix(s, "example-") {
		for _, e := range examples {
			if e.ID == ID+"/"+s {
				return 0, s, nil
			}
		}
		return 0, "", apperr.NotFound("template")
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != s {
		return 0, "", apperr.NotFound("template")
	}
	return n, "", nil
}

func (p *Plugin) get(ctx context.Context, key string, withDeleted bool) (*Template, error) {
	n, ex, err := keyOf(key)
	if err != nil {
		return nil, err
	}
	if ex != "" {
		for _, e := range examples {
			if e.ID == ID+"/"+ex {
				c := e
				return &c, nil
			}
		}
	}
	var t Template
	var id int64
	err = p.host.DB.QueryRow(ctx, `SELECT id, names, design, based_on, version, updated_at, updated_by, deleted_at FROM plugin_templates WHERE id=$1`, n).
		Scan(&id, &t.Names, &t.Design, &t.BasedOn, &t.Version, &t.UpdatedAt, &t.UpdatedBy, &t.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && t.DeletedAt != nil && !withDeleted) {
		return nil, apperr.NotFound("template")
	}
	if err != nil {
		return nil, err
	}
	t.ID, t.Key = fmt.Sprintf("%s/%d", ID, id), strconv.FormatInt(id, 10)
	return &t, nil
}

// list returns the shipped examples and the stored templates (without
// their design; the trash only with withDeleted).
func (p *Plugin) list(ctx context.Context, deleted bool) ([]Template, error) {
	out := []Template{}
	if !deleted {
		for _, e := range examples {
			c := e
			c.Design = nil
			out = append(out, c)
		}
	}
	rows, err := p.host.DB.Query(ctx, `SELECT id, names, based_on, version, updated_at, updated_by, deleted_at FROM plugin_templates
		WHERE (deleted_at IS NOT NULL) = $1 ORDER BY id`, deleted)
	if err != nil {
		return nil, err
	}
	stored, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Template, error) {
		var t Template
		var id int64
		err := r.Scan(&id, &t.Names, &t.BasedOn, &t.Version, &t.UpdatedAt, &t.UpdatedBy, &t.DeletedAt)
		t.ID, t.Key = fmt.Sprintf("%s/%d", ID, id), strconv.FormatInt(id, 10)
		return t, err
	})
	if err != nil {
		return nil, err
	}
	return append(out, stored...), nil
}

// designs are the templates that can be used, with their designs (for
// rendering pages: no names of people, nothing from the trash).
func (p *Plugin) designs(ctx context.Context) ([]Template, error) {
	out := slices.Clone(examples)
	rows, err := p.host.DB.Query(ctx, `SELECT id, names, design, version FROM plugin_templates WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	stored, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Template, error) {
		var t Template
		var id int64
		err := r.Scan(&id, &t.Names, &t.Design, &t.Version)
		t.ID, t.Key = fmt.Sprintf("%s/%d", ID, id), strconv.FormatInt(id, 10)
		return t, err
	})
	if err != nil {
		return nil, err
	}
	return append(out, stored...), nil
}

// HasTemplate implements content.TemplateSource.
func (p *Plugin) HasTemplate(ctx context.Context, id string) bool {
	if !strings.HasPrefix(id, ID+"/") {
		return false
	}
	_, err := p.get(ctx, id, false)
	return err == nil
}

// Templates implements content.TemplateSource.
func (p *Plugin) Templates(ctx context.Context) []content.TemplateName {
	ts, err := p.designs(ctx)
	if err != nil {
		return nil
	}
	lang := p.host.Config.Get().I18n.DefaultLanguage
	out := []content.TemplateName{}
	for _, t := range ts {
		name := t.Names[lang]
		if name == "" {
			for _, v := range t.Names {
				name = v
				break
			}
		}
		out = append(out, content.TemplateName{ID: t.ID, Name: name, Names: t.Names})
	}
	return out
}

// usedBy names where a template is used (site or categories): a template
// in use cannot go to the trash.
func (p *Plugin) usedBy(ctx context.Context, id string) ([]string, error) {
	var out []string
	if p.host.Config.Get().UI.Template == id {
		out = append(out, "site")
	}
	// acl: only counts (the names of the categories are not shown)
	var n int
	if err := p.host.DB.QueryRow(ctx, `SELECT count(*) FROM categories WHERE template = $1 AND deleted_at IS NULL`, id).Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 {
		out = append(out, fmt.Sprintf("%d categories", n))
	}
	return out, nil
}

type input struct {
	Names   map[string]string `json:"names"`
	Design  json.RawMessage   `json:"design"`
	CopyOf  string            `json:"copy_of"`
	Version int               `json:"version"`
}

// checked parses and checks a design and returns it in its normal form.
func checked(raw json.RawMessage) (json.RawMessage, error) {
	d, err := ParseDesign(raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(d)
}

func (p *Plugin) create(ctx context.Context, in input) (*Template, error) {
	names, err := cleanNames(in.Names)
	if err != nil {
		return nil, err
	}
	design, based := in.Design, ""
	if in.CopyOf != "" {
		src, err := p.get(ctx, in.CopyOf, false)
		if err != nil {
			return nil, err
		}
		design, based = src.Design, src.ID
	}
	if len(design) == 0 {
		return nil, apperr.BadRequest("a template needs a design or a template to copy (copy_of)")
	}
	norm, err := checked(design)
	if err != nil {
		return nil, err
	}
	me := identity.From(ctx).ID()
	var id int64
	err = pgx.BeginFunc(ctx, p.host.DB, func(tx pgx.Tx) error {
		var n int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM plugin_templates`).Scan(&n); err != nil {
			return err
		}
		if max := p.host.Limit("core.template.count"); n >= max {
			return apperr.BadRequest("at most %d templates (expert setting core.template.count); empty the trash or raise it", max)
		}
		if err := tx.QueryRow(ctx, `INSERT INTO plugin_templates(names, design, based_on, created_by, updated_by) VALUES ($1, $2, $3, $4, $4) RETURNING id`,
			names, norm, based, me).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO plugin_template_versions(template_id, version, names, design, author) VALUES ($1, 1, $2, $3, $4)`, id, names, norm, me); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "core.template.create", "template", id, nil, map[string]any{"names": names, "based_on": based})
	})
	if err != nil {
		return nil, err
	}
	return p.get(ctx, strconv.FormatInt(id, 10), false)
}

func (p *Plugin) update(ctx context.Context, key string, in input) (*Template, error) {
	cur, err := p.get(ctx, key, false)
	if err != nil {
		return nil, err
	}
	if cur.ReadOnly {
		return nil, apperr.Forbidden("the shipped templates cannot be changed; copy one")
	}
	if in.Version != cur.Version {
		return nil, apperr.Conflict("the template was changed meanwhile (version %d, you edited %d); load it again", cur.Version, in.Version)
	}
	names := cur.Names
	if in.Names != nil {
		if names, err = cleanNames(in.Names); err != nil {
			return nil, err
		}
	}
	design := cur.Design
	if len(in.Design) > 0 {
		if design, err = checked(in.Design); err != nil {
			return nil, err
		}
	}
	n, _, _ := keyOf(key)
	me := identity.From(ctx).ID()
	keep := p.host.Limit("core.template.versions")
	err = pgx.BeginFunc(ctx, p.host.DB, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE plugin_templates SET names=$2, design=$3, version=version+1, updated_by=$4, updated_at=now() WHERE id=$1 AND version=$5 AND deleted_at IS NULL`,
			n, names, design, me, cur.Version)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.Conflict("the template was changed meanwhile; load it again")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO plugin_template_versions(template_id, version, names, design, author) VALUES ($1, $2, $3, $4, $5)`,
			n, cur.Version+1, names, design, me); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM plugin_template_versions WHERE template_id=$1 AND version <= $2`, n, int64(cur.Version+1)-keep); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "core.template.update", "template", n, map[string]any{"version": cur.Version}, map[string]any{"version": cur.Version + 1, "names": names})
	})
	if err != nil {
		return nil, err
	}
	return p.get(ctx, key, false)
}

// Setup registers the template source and the API.
func (p *Plugin) Setup(host *coreplugin.Host) error {
	p.host = host
	host.Content.AddTemplateSource(ID, p)
	design := func(r *http.Request) error { return perm.Require(r.Context(), permDesign) }

	// GET /designs → the templates that can be used, with their designs (the
	// wiki builds its pages from them; public like the CSS of a template)
	// action: core.template.designs
	host.Handle("GET", "/designs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return p.designs(r.Context())
	})
	// GET /limits → the limits of templates (the same file the server checks with)
	// action: core.template.limits
	host.Handle("GET", "/limits", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return json.RawMessage(limitsJSON), nil
	})
	// GET /templates?deleted=1 → the templates (shipped and stored; the trash)
	// action: core.template.list
	host.Handle("GET", "/templates", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := design(r); err != nil {
			return nil, err
		}
		return p.list(r.Context(), r.URL.Query().Get("deleted") == "1")
	})
	// GET /templates/{key} → one template with its design and where it is used
	// action: core.template.get
	host.Handle("GET", "/templates/{key}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := design(r); err != nil {
			return nil, err
		}
		t, err := p.get(r.Context(), r.PathValue("key"), true)
		if err != nil {
			return nil, err
		}
		used, err := p.usedBy(r.Context(), t.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"template": t, "used_by": nonNil(used)}, nil
	})
	// POST /check {design} → the problems of a design (empty: it can be saved)
	// action: core.template.check
	host.Handle("POST", "/check", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := design(r); err != nil {
			return nil, err
		}
		var in struct {
			Design json.RawMessage `json:"design"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if len(in.Design) > limits.MaxBytes {
			return nil, apperr.BadRequest("a template has at most %d bytes", limits.MaxBytes)
		}
		dec := json.NewDecoder(strings.NewReader(string(in.Design)))
		dec.DisallowUnknownFields()
		var d Design
		if err := dec.Decode(&d); err != nil {
			return map[string]any{"problems": []Problem{{Where: "design", Text: err.Error()}}}, nil
		}
		ps := d.Problems()
		if ps == nil {
			ps = []Problem{}
		}
		return map[string]any{"problems": ps}, nil
	})
	// POST /templates {names, design | copy_of} → a new template
	// action: core.template.create
	host.Handle("POST", "/templates", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := design(r); err != nil {
			return nil, err
		}
		var in input
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		t, err := p.create(r.Context(), in)
		if err != nil {
			return nil, err
		}
		w.WriteHeader(http.StatusCreated)
		return t, nil
	})
	// PUT /templates/{key} {names, design, version} → a new version
	// action: core.template.update
	host.Handle("PUT", "/templates/{key}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := design(r); err != nil {
			return nil, err
		}
		var in input
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return p.update(r.Context(), r.PathValue("key"), in)
	})
	// DELETE /templates/{key} → to the trash (not while in use);
	// ?purge=1 removes a template in the trash for good
	// action: core.template.delete
	host.Handle("DELETE", "/templates/{key}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := design(r); err != nil {
			return nil, err
		}
		ctx := r.Context()
		t, err := p.get(ctx, r.PathValue("key"), true)
		if err != nil {
			return nil, err
		}
		if t.ReadOnly {
			return nil, apperr.Forbidden("the shipped templates cannot be deleted")
		}
		n, _, _ := keyOf(t.ID)
		if r.URL.Query().Get("purge") == "1" {
			if t.DeletedAt == nil {
				return nil, apperr.BadRequest("only a template in the trash can be removed for good")
			}
			err := pgx.BeginFunc(ctx, host.DB, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, `DELETE FROM plugin_templates WHERE id=$1`, n); err != nil {
					return err
				}
				// its default placements go with it
				if _, err := tx.Exec(ctx, `DELETE FROM placements WHERE level='template' AND target=$1`, t.ID); err != nil {
					return err
				}
				return audit.Log(ctx, tx, "core.template.purge", "template", n, map[string]any{"names": t.Names}, nil)
			})
			if err != nil {
				return nil, err
			}
			w.WriteHeader(http.StatusNoContent)
			return nil, nil
		}
		if t.DeletedAt != nil {
			return nil, apperr.NotFound("template")
		}
		used, err := p.usedBy(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		if len(used) > 0 {
			return nil, apperr.Conflict("the template is in use (%s); choose another one there first", strings.Join(used, ", "))
		}
		err = pgx.BeginFunc(ctx, host.DB, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE plugin_templates SET deleted_at=now() WHERE id=$1`, n); err != nil {
				return err
			}
			return audit.Log(ctx, tx, "core.template.delete", "template", n, map[string]any{"names": t.Names}, nil)
		})
		if err != nil {
			return nil, err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil, nil
	})
	// POST /templates/{key}/restore → back from the trash
	// action: core.template.restore
	host.Handle("POST", "/templates/{key}/restore", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := design(r); err != nil {
			return nil, err
		}
		ctx := r.Context()
		t, err := p.get(ctx, r.PathValue("key"), true)
		if err != nil {
			return nil, err
		}
		if t.DeletedAt == nil {
			return nil, apperr.BadRequest("the template is not in the trash")
		}
		n, _, _ := keyOf(t.ID)
		err = pgx.BeginFunc(ctx, host.DB, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE plugin_templates SET deleted_at=NULL WHERE id=$1`, n); err != nil {
				return err
			}
			return audit.Log(ctx, tx, "core.template.restore", "template", n, nil, map[string]any{"names": t.Names})
		})
		if err != nil {
			return nil, err
		}
		return p.get(ctx, t.ID, false)
	})
	// GET /templates/{key}/versions → the saved versions (newest first)
	// action: core.template.versions
	host.Handle("GET", "/templates/{key}/versions", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := design(r); err != nil {
			return nil, err
		}
		ctx := r.Context()
		t, err := p.get(ctx, r.PathValue("key"), true)
		if err != nil {
			return nil, err
		}
		type ver struct {
			Version   int               `json:"version"`
			Names     map[string]string `json:"names"`
			Design    json.RawMessage   `json:"design"`
			Author    *int64            `json:"author"`
			CreatedAt time.Time         `json:"created_at"`
		}
		if t.ReadOnly {
			return []ver{{Version: 1, Names: t.Names, Design: t.Design}}, nil
		}
		n, _, _ := keyOf(t.ID)
		rows, err := host.DB.Query(ctx, `SELECT version, names, design, author, created_at FROM plugin_template_versions WHERE template_id=$1 ORDER BY version DESC LIMIT 500`, n)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowToStructByPos[ver])
	})
	// GET /templates/{key}/export → the template as a file (names and design)
	// action: core.template.export
	host.Handle("GET", "/templates/{key}/export", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := design(r); err != nil {
			return nil, err
		}
		t, err := p.get(r.Context(), r.PathValue("key"), false)
		if err != nil {
			return nil, err
		}
		w.Header().Set("Content-Disposition", `attachment; filename="template.json"`)
		return map[string]any{"format": "ewiki-template", "version": 1, "names": t.Names, "design": t.Design}, nil
	})
	// POST /templates/import {format, version, names, design} → a new template
	// action: core.template.import
	host.Handle("POST", "/templates/import", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := design(r); err != nil {
			return nil, err
		}
		var in struct {
			Format  string            `json:"format"`
			Version int               `json:"version"`
			Names   map[string]string `json:"names"`
			Design  json.RawMessage   `json:"design"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		if in.Format != "ewiki-template" || in.Version != 1 {
			return nil, apperr.BadRequest("not a template file of Enterprise Wiki (format ewiki-template, version 1)")
		}
		t, err := p.create(r.Context(), input{Names: in.Names, Design: in.Design})
		if err != nil {
			return nil, err
		}
		w.WriteHeader(http.StatusCreated)
		return t, nil
	})
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
