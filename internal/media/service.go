package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/storage"
)

// Perm are the rights on a medium.
type Perm int

// Rights on a medium.
const (
	View     Perm = 1 << iota // see / display it
	Download                  // download the file
	Edit                      // metadata, new versions, image editor
	Manage                    // rights, download switch, watermark, delete
	All      = View | Download | Edit | Manage
)

// Strings returns the names of the set bits.
func (p Perm) Strings() []string {
	out := []string{}
	for _, x := range []struct {
		b Perm
		n string
	}{{View, "view"}, {Download, "download"}, {Edit, "edit"}, {Manage, "manage"}} {
		if p&x.b != 0 {
			out = append(out, x.n)
		}
	}
	return out
}

// Watermark modes (see docs/developer/media.md).
const (
	WMNone         = "none"
	WMUploaderOnly = "uploader_only" // the original is not stored
	WMUploaderView = "uploader_view" // shown with the uploader's mark, original for downloads
	WMViewerView   = "viewer_view"   // the viewer's name is burnt in when shown
	WMViewerAll    = "viewer_all"    // ... when shown and downloaded
)

// WatermarkSpec configures the watermark of a medium.
type WatermarkSpec struct {
	Mode    string `json:"mode"`
	User    bool   `json:"user"`    // name of the uploader / viewer
	Date    bool   `json:"date"`    // date (and time)
	Comment string `json:"comment"` // optional text
}

func (w *WatermarkSpec) normalize() error {
	switch w.Mode {
	case "":
		w.Mode = WMNone
	case WMNone, WMUploaderOnly, WMUploaderView, WMViewerView, WMViewerAll:
	default:
		return apperr.BadRequest("unknown watermark mode %q", w.Mode)
	}
	w.Comment = strings.TrimSpace(w.Comment)
	if len([]rune(w.Comment)) > 120 {
		return apperr.BadRequest("watermark comment: at most 120 characters")
	}
	if w.Mode != WMNone && !w.User && !w.Date && w.Comment == "" {
		return apperr.BadRequest("a watermark needs the name, the date or a comment")
	}
	return nil
}

// Rule is an entry of the own rights of a medium.
type Rule struct {
	SubjectType string   `json:"subject_type"`
	SubjectID   *int64   `json:"subject_id,omitempty"`
	Perms       []string `json:"perms"`
}

// Medium is a file of the media library.
type Medium struct {
	ID          int64         `json:"id"`
	CategoryID  int64         `json:"category_id"`
	Name        string        `json:"name"`
	Title       string        `json:"title"`
	Alt         string        `json:"alt"`
	Description string        `json:"description"`
	Version     int           `json:"version"`
	Mime        string        `json:"mime"`
	Size        int64         `json:"size"`
	Width       int           `json:"width"`
	Height      int           `json:"height"`
	Download    bool          `json:"download"`
	OwnACL      bool          `json:"own_acl"`
	Watermark   WatermarkSpec `json:"watermark"`
	CreatedBy   *int64        `json:"created_by,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	// Permissions of the caller; ViewOnly: the file cannot be downloaded.
	Permissions []string `json:"permissions"`
	ViewOnly    bool     `json:"view_only"`
	URL         string   `json:"url"`
	hash        string
}

// Version is one version of a medium.
type Version struct {
	Version   int       `json:"version"`
	Hash      string    `json:"-"`
	Mime      string    `json:"mime"`
	Size      int64     `json:"size"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Ops       []Op      `json:"ops"`
	Note      string    `json:"note"`
	CreatedBy *int64    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Service manages the media library of a site.
type Service struct {
	pool  *pgxpool.Pool
	acl   *acl.Service
	files *storage.Store
	log   *slog.Logger
	// UserName returns the display name of a user (watermarks).
	UserName func(ctx context.Context, id int64) string
	// LoadPrincipal loads a user with groups and roles (access tokens).
	LoadPrincipal func(ctx context.Context, id int64) (*identity.Principal, error)
	// Enabled reports whether a plugin is active on this site (openers).
	Enabled func(pluginID string) bool
	op      openers
}

// New creates the media service.
func New(pool *pgxpool.Pool, a *acl.Service, files *storage.Store, log *slog.Logger) *Service {
	files.AddReference("media_versions", "hash")
	return &Service{pool: pool, acl: a, files: files, log: log}
}

const cols = `m.id, m.category_id, m.name, m.title, m.alt, m.description, m.current, v.mime, v.size, v.width, v.height,
	m.download, m.own_acl, m.watermark, m.created_by, m.created_at, m.updated_at, v.hash`

const from = ` FROM media m JOIN media_versions v ON v.media_id = m.id AND v.version = m.current`

func scan(row pgx.Row) (*Medium, error) {
	m := &Medium{}
	var wm []byte
	err := row.Scan(&m.ID, &m.CategoryID, &m.Name, &m.Title, &m.Alt, &m.Description, &m.Version, &m.Mime, &m.Size, &m.Width, &m.Height,
		&m.Download, &m.OwnACL, &wm, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt, &m.hash)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(wm, &m.Watermark)
	m.URL = fmt.Sprintf("/api/v1/media/%d/view", m.ID)
	return m, nil
}

func (s *Service) load(ctx context.Context, id int64) (*Medium, error) {
	m, err := scan(s.pool.QueryRow(ctx, `SELECT `+cols+from+` WHERE m.id=$1 AND m.deleted_at IS NULL`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("medium")
	}
	return m, err
}

// catPerms maps category rights to media rights.
func catPerms(p acl.Perm) Perm {
	var out Perm
	if p&acl.View != 0 {
		out |= View | Download
	}
	if p&(acl.Edit|acl.Create) != 0 {
		out |= Edit
	}
	if p&acl.Manage != 0 {
		out |= Manage
	}
	return out
}

func (s *Service) ownPerms(ctx context.Context, p *identity.Principal, id int64) (Perm, error) {
	rows, err := s.pool.Query(ctx, `SELECT subject_type, subject_id, perms FROM media_acl WHERE media_id=$1`, id)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var out Perm
	for rows.Next() {
		var st string
		var sid *int64
		var bits int
		if err := rows.Scan(&st, &sid, &bits); err != nil {
			return 0, err
		}
		if matches(p, st, sid) {
			out |= Perm(bits)
		}
	}
	return out, rows.Err()
}

func matches(p *identity.Principal, st string, sid *int64) bool {
	signedIn := p != nil && p.UserID > 0
	switch st {
	case "guest":
		return true
	case "authenticated":
		return signedIn
	}
	if !signedIn || sid == nil {
		return false
	}
	switch st {
	case "user":
		return p.UserID == *sid
	case "group":
		return contains(p.Groups, *sid)
	case "role":
		return contains(p.Roles, *sid)
	}
	return false
}

func contains(l []int64, v int64) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

// Perms returns the rights of the caller on m (see docs/developer/media.md).
func (s *Service) Perms(ctx context.Context, m *Medium) (Perm, error) {
	p := identity.From(ctx)
	cat, err := s.acl.Perms(ctx, p, m.CategoryID)
	if err != nil {
		return 0, err
	}
	admin := p != nil && (p.IsAdmin || p.InstanceAdmin) && p.Via != "apikey"
	var out Perm
	switch {
	case admin:
		out = All
	case m.OwnACL:
		if out, err = s.ownPerms(ctx, p, m.ID); err != nil {
			return 0, err
		}
		if p != nil && p.Via == "apikey" { // keys never exceed their category limits
			out &= catPerms(cat)
		}
	default:
		out = catPerms(cat)
		if out&View == 0 {
			// embedded in an element the caller may see
			rows, err := s.pool.Query(ctx, `SELECT DISTINCT e.category_id FROM media_usage u JOIN elements e ON e.id = u.element_id
				WHERE u.media_id=$1 AND e.deleted_at IS NULL`, m.ID)
			if err != nil {
				return 0, err
			}
			var cats []int64
			for rows.Next() {
				var c int64
				if err := rows.Scan(&c); err != nil {
					rows.Close()
					return 0, err
				}
				cats = append(cats, c)
			}
			rows.Close()
			for _, c := range cats {
				if s.acl.Can(ctx, p, c, acl.View) {
					out |= View | Download
					break
				}
			}
		}
	}
	if out&View == 0 {
		out = 0 // nothing without seeing it
	}
	if !m.Download && out&Manage == 0 {
		out &^= Download
	}
	return out, nil
}

// Get returns a medium the caller may see (404 otherwise).
func (s *Service) Get(ctx context.Context, id int64) (*Medium, error) {
	m, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.withPerms(ctx, m)
}

func (s *Service) withPerms(ctx context.Context, m *Medium) (*Medium, error) {
	perms, err := s.Perms(ctx, m)
	if err != nil {
		return nil, err
	}
	if perms&View == 0 {
		return nil, apperr.NotFound("medium")
	}
	m.Permissions = perms.Strings()
	m.ViewOnly = perms&Download == 0
	return m, nil
}

func (s *Service) require(ctx context.Context, id int64, want Perm) (*Medium, Perm, error) {
	m, err := s.load(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	perms, err := s.Perms(ctx, m)
	if err != nil {
		return nil, 0, err
	}
	if perms&View == 0 {
		return nil, 0, apperr.NotFound("medium")
	}
	if perms&want != want {
		return nil, 0, apperr.ErrForbidden
	}
	m.Permissions = perms.Strings()
	m.ViewOnly = perms&Download == 0
	return m, perms, nil
}

// ListInput filters the media list.
type ListInput struct {
	CategoryID int64
	Recursive  bool
	Query      string
	Type       string // image, video, audio, pdf, document, other
	Limit      int
	Offset     int
}

// List returns the media the caller may see.
func (s *Service) List(ctx context.Context, in ListInput) ([]*Medium, error) {
	if in.Limit <= 0 || in.Limit > 200 {
		in.Limit = 50
	}
	where := []string{"m.deleted_at IS NULL"}
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "$?", "$"+strconv.Itoa(len(args))))
	}
	if in.CategoryID > 0 {
		if in.Recursive {
			add(`m.category_id IN (SELECT id FROM categories WHERE path <@ (SELECT path FROM categories WHERE id=$?))`, in.CategoryID)
		} else {
			add(`m.category_id = $?`, in.CategoryID)
		}
	}
	if q := strings.TrimSpace(in.Query); q != "" {
		add(`(m.name ILIKE $? OR m.title ILIKE $? OR m.description ILIKE $?)`, "%"+escapeLike(q)+"%")
	}
	switch in.Type {
	case "":
	case "image", "video", "audio":
		add(`v.mime LIKE $?`, in.Type+"/%")
	case "pdf":
		add(`v.mime = $?`, "application/pdf")
	case "document":
		add(`(v.mime LIKE 'application/vnd.%' OR v.mime LIKE 'application/msword%' OR v.mime = $? OR v.mime LIKE 'text/%')`, "application/rtf")
	default:
		return nil, apperr.BadRequest("unknown type %q", in.Type)
	}
	// filter by rights in Go; read in pages until the page is full
	var out []*Medium
	skip := in.Offset
	for page := 0; len(out) < in.Limit && page < 50; page++ {
		rows, err := s.pool.Query(ctx, `SELECT `+cols+from+` WHERE `+strings.Join(where, " AND ")+
			fmt.Sprintf(` ORDER BY m.updated_at DESC, m.id DESC LIMIT 200 OFFSET %d`, page*200), args...)
		if err != nil {
			return nil, err
		}
		list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Medium, error) { return scan(r) })
		if err != nil {
			return nil, err
		}
		for _, m := range list {
			if m, err := s.withPerms(ctx, m); err == nil {
				if skip > 0 {
					skip--
					continue
				}
				out = append(out, m)
				if len(out) == in.Limit {
					break
				}
			}
		}
		if len(list) < 200 {
			break
		}
	}
	if out == nil {
		out = []*Medium{}
	}
	return out, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// UploadInput creates a medium.
type UploadInput struct {
	CategoryID  int64
	Name        string
	Title       string
	Alt         string
	Description string
	Download    *bool
	Watermark   WatermarkSpec
	// ACL: own rights (nil: rights of the category and embedding elements)
	ACL []Rule
}

// Upload stores a new medium (right "create" or "edit" in the category).
func (s *Service) Upload(ctx context.Context, in UploadInput, r io.Reader) (*Medium, error) {
	p := identity.From(ctx)
	cat, err := s.acl.Perms(ctx, p, in.CategoryID)
	if err != nil {
		return nil, err
	}
	if catPerms(cat)&Edit == 0 && !(p != nil && p.IsAdmin) {
		if cat&acl.View == 0 {
			return nil, apperr.NotFound("category")
		}
		return nil, apperr.ErrForbidden
	}
	if err := in.Watermark.normalize(); err != nil {
		return nil, err
	}
	rules, err := s.checkRules(ctx, in.CategoryID, in.ACL)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(r, s.files.MaxBytes()+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > s.files.MaxBytes() {
		return nil, apperr.New(413, "too_large", "file too large")
	}
	name := cleanName(in.Name)
	// "original not stored": only the version with the uploader's mark
	if in.Watermark.Mode == WMUploaderOnly {
		marked, err := s.mark(ctx, data, in.Watermark, s.principalName(ctx), time.Now())
		if err != nil {
			return nil, err
		}
		data = marked
	}
	f, err := s.files.Put(ctx, bytes.NewReader(data), name)
	if err != nil {
		return nil, err
	}
	w, h := dims(data, f.Mime)
	download := true
	if in.Download != nil {
		download = *in.Download
	}
	wm, _ := json.Marshal(in.Watermark)
	var uid *int64
	if p != nil && p.UserID > 0 {
		uid = &p.UserID
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO media(category_id, name, title, alt, description, download, own_acl, watermark, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, in.CategoryID, name, strings.TrimSpace(in.Title), strings.TrimSpace(in.Alt),
		strings.TrimSpace(in.Description), download, rules != nil, wm, uid).Scan(&id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO media_versions(media_id, version, hash, mime, size, width, height, created_by) VALUES ($1,1,$2,$3,$4,$5,$6,$7)`,
		id, f.Hash, f.Mime, f.Size, w, h, uid); err != nil {
		return nil, err
	}
	if err := writeRules(ctx, tx, id, rules); err != nil {
		return nil, err
	}
	if err := audit.Log(ctx, tx, "media.upload", "media", id, nil, map[string]any{"name": name, "category": in.CategoryID, "own_acl": rules != nil, "watermark": in.Watermark.Mode, "download": download}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

type rule struct {
	st  string
	sid *int64
	p   Perm
}

func permOf(names []string) (Perm, error) {
	var p Perm
	for _, n := range names {
		switch n {
		case "view":
			p |= View
		case "download":
			p |= View | Download
		case "edit":
			p |= View | Edit
		case "manage":
			p |= All
		default:
			return 0, apperr.BadRequest("unknown right %q", n)
		}
	}
	return p, nil
}

// checkRules validates own rights: only rights the caller has in the
// category can be given (administrators: all). nil: no own rights.
func (s *Service) checkRules(ctx context.Context, categoryID int64, in []Rule) ([]rule, error) {
	if in == nil {
		return nil, nil
	}
	p := identity.From(ctx)
	cat, err := s.acl.Perms(ctx, p, categoryID)
	if err != nil {
		return nil, err
	}
	allowed := catPerms(cat)
	if p != nil && (p.IsAdmin || p.InstanceAdmin) && p.Via != "apikey" {
		allowed = All
	}
	if len(in) > 100 {
		return nil, apperr.BadRequest("at most 100 rules")
	}
	out := []rule{}
	for _, r := range in {
		perm, err := permOf(r.Perms)
		if err != nil {
			return nil, err
		}
		if perm == 0 {
			continue
		}
		if perm&^allowed != 0 {
			return nil, apperr.Forbidden("you can only give rights you have yourself (%s)", strings.Join(allowed.Strings(), ", "))
		}
		switch r.SubjectType {
		case "guest", "authenticated":
			if r.SubjectID != nil {
				return nil, apperr.BadRequest("%s has no subject_id", r.SubjectType)
			}
		case "user", "group", "role":
			if r.SubjectID == nil {
				return nil, apperr.BadRequest("%s needs subject_id", r.SubjectType)
			}
		default:
			return nil, apperr.BadRequest("unknown subject type %q", r.SubjectType)
		}
		out = append(out, rule{r.SubjectType, r.SubjectID, perm})
	}
	return out, nil
}

func writeRules(ctx context.Context, tx pgx.Tx, id int64, rules []rule) error {
	if _, err := tx.Exec(ctx, `DELETE FROM media_acl WHERE media_id=$1`, id); err != nil {
		return err
	}
	for _, r := range rules {
		if _, err := tx.Exec(ctx, `INSERT INTO media_acl(media_id, subject_type, subject_id, perms) VALUES ($1,$2,$3,$4)`, id, r.st, r.sid, int(r.p)); err != nil {
			if strings.Contains(err.Error(), "violates foreign key") {
				return apperr.BadRequest("unknown subject")
			}
			return err
		}
	}
	return nil
}

// Rules returns the own rights of a medium (right "manage").
func (s *Service) Rules(ctx context.Context, id int64) ([]Rule, error) {
	if _, _, err := s.require(ctx, id, Manage); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT subject_type, subject_id, perms FROM media_acl WHERE media_id=$1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Rule, error) {
		var x Rule
		var bits int
		err := r.Scan(&x.SubjectType, &x.SubjectID, &bits)
		x.Perms = Perm(bits).Strings()
		return x, err
	})
}

// UpdateInput changes a medium; nil fields stay.
type UpdateInput struct {
	Name        *string        `json:"name"`
	Title       *string        `json:"title"`
	Alt         *string        `json:"alt"`
	Description *string        `json:"description"`
	CategoryID  *int64         `json:"category_id"`
	Download    *bool          `json:"download"`
	Watermark   *WatermarkSpec `json:"watermark"`
	// ACL replaces the own rights; an empty list with OwnACL false removes them.
	ACL    *[]Rule `json:"acl"`
	OwnACL *bool   `json:"own_acl"`
}

// Update changes texts (edit), category, download, watermark and rights (manage).
func (s *Service) Update(ctx context.Context, id int64, in UpdateInput) (*Medium, error) {
	want := Edit
	if in.CategoryID != nil || in.Download != nil || in.Watermark != nil || in.ACL != nil || in.OwnACL != nil {
		want |= Manage
	}
	m, _, err := s.require(ctx, id, want)
	if err != nil {
		return nil, err
	}
	if in.CategoryID != nil && *in.CategoryID != m.CategoryID {
		if !s.acl.Can(ctx, identity.From(ctx), *in.CategoryID, acl.Create) && !s.acl.Can(ctx, identity.From(ctx), *in.CategoryID, acl.Edit) {
			return nil, apperr.Forbidden("no right to add media to the target category")
		}
		m.CategoryID = *in.CategoryID
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	set(&m.Title, in.Title)
	set(&m.Alt, in.Alt)
	set(&m.Description, in.Description)
	if in.Name != nil {
		m.Name = cleanName(*in.Name)
	}
	if in.Download != nil {
		m.Download = *in.Download
	}
	if in.Watermark != nil {
		if err := in.Watermark.normalize(); err != nil {
			return nil, err
		}
		if in.Watermark.Mode == WMUploaderOnly && m.Watermark.Mode != WMUploaderOnly {
			return nil, apperr.BadRequest("\"original not stored\" can only be chosen when uploading")
		}
		if m.Watermark.Mode == WMUploaderOnly && in.Watermark.Mode != WMUploaderOnly {
			return nil, apperr.BadRequest("the original of this medium was not stored")
		}
		m.Watermark = *in.Watermark
	}
	var rules []rule
	replaceRules := false
	if in.OwnACL != nil {
		m.OwnACL = *in.OwnACL
		replaceRules = true
	}
	if in.ACL != nil {
		if rules, err = s.checkRules(ctx, m.CategoryID, *in.ACL); err != nil {
			return nil, err
		}
		replaceRules = true
		if in.OwnACL == nil {
			m.OwnACL = true
		}
	}
	wm, _ := json.Marshal(m.Watermark)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE media SET name=$2, title=$3, alt=$4, description=$5, category_id=$6, download=$7, watermark=$8, own_acl=$9, updated_at=now() WHERE id=$1`,
		id, m.Name, m.Title, m.Alt, m.Description, m.CategoryID, m.Download, wm, m.OwnACL); err != nil {
		return nil, err
	}
	if replaceRules {
		if !m.OwnACL {
			rules = nil
		}
		if err := writeRules(ctx, tx, id, rules); err != nil {
			return nil, err
		}
	}
	if err := audit.Log(ctx, tx, "media.update", "media", id, nil, in); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Delete moves a medium to the trash (right "manage").
func (s *Service) Delete(ctx context.Context, id int64) error {
	if _, _, err := s.require(ctx, id, Manage); err != nil {
		return err
	}
	p := identity.From(ctx)
	var uid *int64
	if p != nil && p.UserID > 0 {
		uid = &p.UserID
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE media SET deleted_at=now(), deleted_by=$2 WHERE id=$1`, id, uid); err != nil {
		return err
	}
	if err := audit.Log(ctx, tx, "media.delete", "media", id, nil, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Versions lists the versions (right "edit").
func (s *Service) Versions(ctx context.Context, id int64) ([]Version, error) {
	if _, _, err := s.require(ctx, id, Edit); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT version, hash, mime, size, width, height, ops, note, created_by, created_at
		FROM media_versions WHERE media_id=$1 ORDER BY version DESC`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Version, error) {
		var v Version
		var ops []byte
		err := r.Scan(&v.Version, &v.Hash, &v.Mime, &v.Size, &v.Width, &v.Height, &ops, &v.Note, &v.CreatedBy, &v.CreatedAt)
		_ = json.Unmarshal(ops, &v.Ops)
		if v.Ops == nil {
			v.Ops = []Op{}
		}
		return v, err
	})
}

func (s *Service) addVersion(ctx context.Context, m *Medium, data []byte, ops []Op, note, action string) (*Medium, error) {
	f, err := s.files.Put(ctx, bytes.NewReader(data), m.Name)
	if err != nil {
		return nil, err
	}
	w, h := dims(data, f.Mime)
	p := identity.From(ctx)
	var uid *int64
	if p != nil && p.UserID > 0 {
		uid = &p.UserID
	}
	if ops == nil {
		ops = []Op{}
	}
	opsJSON, _ := json.Marshal(ops)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var v int
	if err := tx.QueryRow(ctx, `SELECT max(version)+1 FROM media_versions WHERE media_id=$1`, m.ID).Scan(&v); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO media_versions(media_id, version, hash, mime, size, width, height, ops, note, created_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		m.ID, v, f.Hash, f.Mime, f.Size, w, h, opsJSON, strings.TrimSpace(note), uid); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE media SET current=$2, updated_at=now() WHERE id=$1`, m.ID, v); err != nil {
		return nil, err
	}
	if err := audit.Log(ctx, tx, action, "media", m.ID, nil, map[string]any{"version": v, "ops": ops}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.Get(ctx, m.ID)
}

// Replace uploads a new version of the file (right "edit").
func (s *Service) Replace(ctx context.Context, id int64, r io.Reader, note string) (*Medium, error) {
	m, _, err := s.require(ctx, id, Edit)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(r, s.files.MaxBytes()+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > s.files.MaxBytes() {
		return nil, apperr.New(413, "too_large", "file too large")
	}
	if m.Watermark.Mode == WMUploaderOnly {
		if data, err = s.mark(ctx, data, m.Watermark, s.principalName(ctx), time.Now()); err != nil {
			return nil, err
		}
	}
	return s.addVersion(ctx, m, data, nil, note, "media.replace")
}

// EditImage applies image editor steps to the current version and stores
// the result as a new version (right "edit").
func (s *Service) EditImage(ctx context.Context, id int64, ops []Op, note string) (*Medium, error) {
	m, _, err := s.require(ctx, id, Edit)
	if err != nil {
		return nil, err
	}
	if !editable(m.Mime) {
		return nil, apperr.BadRequest("only PNG, JPEG, GIF and WebP images can be edited")
	}
	if len(ops) == 0 {
		return nil, apperr.BadRequest("no editing steps")
	}
	data, err := s.read(m.hash)
	if err != nil {
		return nil, err
	}
	img, format, err := Decode(data)
	if err != nil {
		return nil, apperr.BadRequest("cannot read the image: %v", err)
	}
	if img, err = Apply(img, ops); err != nil {
		return nil, apperr.BadRequest("%v", err)
	}
	if format == "webp" {
		format = "png" // no WebP encoder
	}
	out, _, err := Encode(img, format)
	if err != nil {
		return nil, err
	}
	return s.addVersion(ctx, m, out, ops, note, "media.edit")
}

// Restore makes an older version the current one (as a new version).
func (s *Service) Restore(ctx context.Context, id int64, version int) (*Medium, error) {
	m, _, err := s.require(ctx, id, Edit)
	if err != nil {
		return nil, err
	}
	var hash string
	if err := s.pool.QueryRow(ctx, `SELECT hash FROM media_versions WHERE media_id=$1 AND version=$2`, id, version).Scan(&hash); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.NotFound("version")
		}
		return nil, err
	}
	data, err := s.read(hash)
	if err != nil {
		return nil, err
	}
	return s.addVersion(ctx, m, data, nil, fmt.Sprintf("restored version %d", version), "media.restore")
}

func (s *Service) read(hash string) ([]byte, error) {
	f, err := s.files.Open(hash)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, s.files.MaxBytes()+1))
}

// Usage returns the elements the caller may see that embed the medium.
func (s *Service) Usage(ctx context.Context, id int64) ([]int64, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id, e.category_id FROM media_usage u JOIN elements e ON e.id = u.element_id
		WHERE u.media_id=$1 AND e.deleted_at IS NULL ORDER BY e.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	p := identity.From(ctx)
	for rows.Next() {
		var eid, cid int64
		if err := rows.Scan(&eid, &cid); err != nil {
			return nil, err
		}
		if s.acl.Can(ctx, p, cid, acl.View) {
			out = append(out, eid)
		}
	}
	return out, rows.Err()
}

var (
	refRe  = regexp.MustCompile(`/api/v1/media/(\d+)`)
	listRe = regexp.MustCompile(`\\?"media\\?"\s*:\s*\\?"([\d,\s]+)\\?"`) // macro parameter media="1,2,3" (also JSON-escaped)
	numRe  = regexp.MustCompile(`\d+`)
)

// RefreshUsage recomputes which media an element embeds (all languages,
// latest revisions). Called on every change of an element.
func (s *Service) RefreshUsage(ctx context.Context, elementID int64) error {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (lang) body::text FROM element_revisions WHERE element_id=$1 ORDER BY lang, rev DESC`, elementID)
	if err != nil {
		return err
	}
	ids := map[int64]bool{}
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			rows.Close()
			return err
		}
		for _, m := range refRe.FindAllStringSubmatch(body, -1) {
			if id, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				ids[id] = true
			}
		}
		for _, m := range listRe.FindAllStringSubmatch(body, -1) {
			for _, v := range numRe.FindAllString(m[1], 100) {
				if id, err := strconv.ParseInt(v, 10, 64); err == nil {
					ids[id] = true
				}
			}
		}
	}
	rows.Close()
	list := make([]int64, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM media_usage WHERE element_id=$1`, elementID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO media_usage(media_id, element_id) SELECT id, $1 FROM media WHERE id = ANY($2) ON CONFLICT DO NOTHING`, elementID, list); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// HashVisible reports whether hash belongs to a medium whose current
// version (or, with the right "edit", any version) the caller may see.
func (s *Service) HashVisible(ctx context.Context, hash string) bool {
	rows, err := s.pool.Query(ctx, `SELECT m.id, v.version = m.current FROM media_versions v JOIN media m ON m.id = v.media_id
		WHERE v.hash=$1 AND m.deleted_at IS NULL LIMIT 20`, hash)
	if err != nil {
		return false
	}
	type hit struct {
		id      int64
		current bool
	}
	var hits []hit
	for rows.Next() {
		var h hit
		if rows.Scan(&h.id, &h.current) == nil {
			hits = append(hits, h)
		}
	}
	rows.Close()
	for _, h := range hits {
		m, err := s.load(ctx, h.id)
		if err != nil {
			continue
		}
		perms, err := s.Perms(ctx, m)
		if err != nil {
			continue
		}
		// a file with a watermark for viewers or no download is never served raw
		if m.Watermark.Mode == WMViewerView || m.Watermark.Mode == WMViewerAll || m.Watermark.Mode == WMUploaderView {
			if perms&Manage == 0 {
				continue
			}
		}
		if (h.current && perms&Download != 0) || perms&Edit != 0 {
			return true
		}
	}
	return false
}

func cleanName(n string) string {
	n = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(n, "/", "_"), `\`, "_"))
	n = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, n)
	if n == "" {
		n = "file"
	}
	if len(n) > 200 {
		n = n[:200]
	}
	return n
}

func editable(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

func dims(data []byte, mime string) (int, int) {
	if !editable(mime) {
		return 0, 0
	}
	cfg, _, err := imageConfig(data)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func (s *Service) principalName(ctx context.Context) string {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return ""
	}
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.Username
}
