package content_forms

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/media"
	"github.com/mtsonline/enterprisewiki/internal/netguard"
	"github.com/mtsonline/enterprisewiki/internal/pdfdoc"
	"github.com/mtsonline/enterprisewiki/internal/secrets"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

// The further targets (webhook, property database, workflow, media library)
// are delivered by the worker "deliver" with the rights of the creator of
// the form, checked again at that moment: if the creator lost a right, the
// target fails (shown at the submission) instead of acting with more rights.

// pendingTargets lists the targets the worker delivers for a submission.
func (f *Form) pendingTargets(files []StoredFile) []string {
	out := []string{}
	if f.Targets.Webhook != nil {
		out = append(out, "webhook")
	}
	if f.Targets.PropDB != nil {
		out = append(out, "propdb")
	}
	if f.Targets.Workflow != nil {
		out = append(out, "workflow")
	}
	if f.Targets.Files != nil {
		out = append(out, "files")
	}
	for _, fl := range files {
		if x := f.Field(fl.Field); x != nil && x.MediaCategory > 0 && !slices.Contains(out, "media") {
			out = append(out, "media")
		}
	}
	if !f.Targets.Store {
		out = append(out, "forget") // remove after the other targets
	}
	return out
}

// retry delays of failed targets (then they stay failed)
var backoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}

type due struct {
	id, element int64
	lang        string
	data        map[string]any
	files       []StoredFile
	pending     []string
	attempts    int
	author      string
}

// deliver is the worker: due submissions with pending targets.
func (p *Plugin) deliver(ctx context.Context) error {
	for range 20 {
		done, err := p.deliverOne(ctx)
		if err != nil || done {
			return err
		}
	}
	return nil
}

func (p *Plugin) deliverOne(ctx context.Context) (bool, error) {
	var d due
	err := pgx.BeginFunc(ctx, p.host.DB, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT id, element_id, lang, data, files, pending, attempts, author FROM plugin_form_submissions
			WHERE next_at IS NOT NULL AND next_at <= now() ORDER BY next_at LIMIT 1 FOR UPDATE SKIP LOCKED`).
			Scan(&d.id, &d.element, &d.lang, &d.data, &d.files, &d.pending, &d.attempts, &d.author)
		if err != nil {
			return err
		}
		// claim it for a while (another node skips it)
		_, err = tx.Exec(ctx, `UPDATE plugin_form_submissions SET next_at = now() + interval '10 minutes' WHERE id=$1`, d.id)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	left, failure := p.deliverTargets(ctx, &d)
	var next *time.Time
	attempts := d.attempts
	if len(left) > 0 && !(len(left) == 1 && left[0] == "forget") {
		attempts++
		if attempts <= len(backoff) {
			t := time.Now().Add(backoff[attempts-1])
			next = &t
		}
	} else if slices.Contains(left, "forget") {
		_, err := p.host.DB.Exec(ctx, `DELETE FROM plugin_form_submissions WHERE id=$1`, d.id)
		return false, err
	}
	_, err = p.host.DB.Exec(ctx, `UPDATE plugin_form_submissions SET pending=$2, attempts=$3, next_at=$4, last_error=$5 WHERE id=$1`,
		d.id, left, attempts, next, failure)
	return false, err
}

// deliverTargets runs the pending targets; it returns those still open and
// the last error.
func (p *Plugin) deliverTargets(ctx context.Context, d *due) ([]string, string) {
	var owner int64
	var cat int64
	var created *int64
	if err := p.host.DB.QueryRow(ctx, `SELECT category_id, created_by FROM elements WHERE id=$1 AND deleted_at IS NULL`, d.element).Scan(&cat, &created); err != nil {
		return d.pending, "the form was deleted"
	}
	if created != nil {
		owner = *created
	}
	op, err := p.host.Auth.LoadPrincipal(ctx, owner)
	if err != nil {
		return d.pending, "the creator of the form is no longer active"
	}
	op.Via = "form"
	octx := identity.With(ctx, op)
	f, _, _, title, err := p.load(octx, d.element, d.lang)
	if err != nil {
		return d.pending, "the creator of the form can no longer see it"
	}
	left := []string{}
	var failure string
	for _, t := range d.pending {
		var err error
		switch t {
		case "webhook":
			err = p.sendWebhook(octx, f, cat, d, title)
		case "propdb":
			err = p.addEntry(octx, f, d, title)
		case "workflow":
			err = p.startCase(octx, f, d, title)
		case "media":
			err = p.addMedia(octx, f, d)
		case "files":
			err = p.storeFiles(octx, f, d, title)
		case "forget":
			left = append(left, t)
			continue
		}
		if err != nil {
			left = append(left, t)
			failure = t + ": " + err.Error()
			p.host.Log.Info("form target failed", "submission", d.id, "target", t, "err", err)
		}
	}
	return left, failure
}

// call runs a request of the wiki's own API in-process with the identity of
// ctx (all checks of the API apply).
func (p *Plugin) call(ctx context.Context, method, path string, body any) (int, []byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	req := httptest.NewRequestWithContext(ctx, method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "ewiki")
	rec := httptest.NewRecorder()
	p.host.Mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes(), nil
}

func apiError(code int, b []byte) error {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(b, &e)
	if e.Error.Message == "" {
		e.Error.Message = http.StatusText(code)
	}
	return fmt.Errorf("%d %s", code, e.Error.Message)
}

// ---------------------------------------------------------------- webhook

func (p *Plugin) sendWebhook(ctx context.Context, f *Form, cat int64, d *due, title string) error {
	t := f.Targets.Webhook
	if t == nil {
		return nil
	}
	if !p.host.Can(ctx, cat, permExternal) {
		return errors.New("the creator of the form may no longer send submissions outside")
	}
	if p.host.ACL.Classification(ctx, cat) >= acl.Confidential {
		return errors.New("confidential forms are not sent to webhooks")
	}
	sec := p.host.Config.Get().Plugins.Macros
	u, err := url.Parse(t.URL)
	if err != nil {
		return err
	}
	if !netguard.Allowed(u, sec.HTTPAllow) {
		return fmt.Errorf("%s is not on the allow list (plugins.macros.http_allow in config.yaml)", u.Redacted())
	}
	labels := map[string]string{}
	for _, x := range f.Fields {
		if x.Input() {
			labels[x.ID] = x.Label
		}
	}
	files := make([]map[string]any, 0, len(d.files))
	for _, fl := range d.files {
		files = append(files, map[string]any{"field": fl.Field, "name": fl.Name, "size": fl.Size, "mime": fl.Mime, "sha256": fl.Hash})
	}
	body, _ := json.Marshal(map[string]any{"event": "form.submitted", "submission": d.id, "submitted_by": d.author,
		"form": map[string]any{"element_id": d.element, "title": title, "labels": labels}, "data": d.data, "files": files})
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "enterprise-wiki-forms")
	if t.SecretRef != "" {
		key, err := secrets.GetRef(ctx, p.host.DB, p.host.Secrets, d.element, t.SecretRef)
		if err != nil {
			return err
		}
		m := hmac.New(sha256.New, []byte(key))
		m.Write(body)
		req.Header.Set("X-Ewiki-Signature", "sha256="+hex.EncodeToString(m.Sum(nil)))
	}
	resp, err := netguard.Client(sec.HTTPAllow, sec.HTTPAllowPrivate, 20*time.Second).Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("the server answered %d", resp.StatusCode)
	}
	return nil
}

// ---------------------------------------------------------------- property database

// propDB reads where a property database adds entries.
func propDB(ctx context.Context, gh *sdk.Host, el int64) (cat int64, typ string, err error) {
	var dbType string
	var body []byte
	err = gh.DB.QueryRow(ctx, `SELECT e.category_id, e.type, r.body FROM elements e JOIN element_revisions r ON r.element_id=e.id AND r.lang=e.original_lang
		WHERE e.id=$1 AND e.deleted_at IS NULL ORDER BY r.rev DESC LIMIT 1`, el).Scan(&cat, &dbType, &body)
	if err != nil || dbType != "content.propdb" {
		return 0, "", apperr.BadRequest("element %d is no property database", el)
	}
	var c struct {
		NewType     string `json:"new_type"`
		NewCategory int64  `json:"new_category"`
	}
	_ = json.Unmarshal(body, &c)
	if c.NewCategory > 0 {
		cat = c.NewCategory
	}
	typ = c.NewType
	if typ == "" {
		typ = "core.html"
	}
	return cat, typ, nil
}

// propDBUsable: the caller sees the database and may create its entries.
func propDBUsable(ctx context.Context, gh *sdk.Host, el int64) error {
	if _, err := gh.Content.ElementCategory(ctx, el, acl.View); err != nil {
		return apperr.BadRequest("property database %d not found", el)
	}
	cat, _, err := propDB(ctx, gh, el)
	if err != nil {
		return err
	}
	if !gh.ACL.Can(ctx, identity.From(ctx), cat, acl.Create) {
		return apperr.Forbidden("no right to add entries to the property database %d", el)
	}
	return nil
}

func (p *Plugin) addEntry(ctx context.Context, f *Form, d *due, title string) error {
	t := f.Targets.PropDB
	if t == nil {
		return nil
	}
	if err := propDBUsable(ctx, p.host, t.Element); err != nil {
		return err
	}
	cat, typ, err := propDB(ctx, p.host, t.Element)
	if err != nil {
		return err
	}
	et := p.host.Content.Type(typ)
	if et == nil {
		return fmt.Errorf("element type %s is not available", typ)
	}
	values := map[string]any{}
	for field, key := range t.Map {
		if v, ok := d.data[field]; ok {
			values[key] = v
		}
	}
	vars := f.vars(title, "", "", d.data, d.files, d.author)
	name := strings.TrimSpace(render(t.Title, vars, true))
	if name == "" {
		name = fmt.Sprintf("%s #%d", title, d.id)
	}
	code, b, err := p.call(ctx, http.MethodPost, "/api/v1/elements", map[string]any{"category_id": cat, "type": typ, "title": clean(name, 300),
		"lang": d.lang, "body": et.EmptyBody(), "fields": map[string]any{"core.properties": map[string]any{"values": values}}})
	if err != nil {
		return err
	}
	if code != http.StatusCreated {
		return apiError(code, b)
	}
	return nil
}

// ---------------------------------------------------------------- workflow

// workflowUsable: the caller sees the workflow (and may start cases).
func workflowUsable(ctx context.Context, gh *sdk.Host, el int64) error {
	if _, err := gh.Content.ElementCategory(ctx, el, acl.View); err != nil {
		return apperr.BadRequest("workflow %d not found", el)
	}
	var typ string
	if err := gh.DB.QueryRow(ctx, `SELECT type FROM elements WHERE id=$1`, el).Scan(&typ); err != nil || typ != "workflow.process" {
		return apperr.BadRequest("element %d is no workflow", el)
	}
	if identity.From(ctx).ID() <= 0 {
		return apperr.ErrUnauthorized
	}
	return nil
}

func (p *Plugin) startCase(ctx context.Context, f *Form, d *due, title string) error {
	t := f.Targets.Workflow
	if t == nil {
		return nil
	}
	if err := workflowUsable(ctx, p.host, t.Element); err != nil {
		return err
	}
	data := map[string]any{}
	for field, key := range t.Map {
		v, ok := d.data[field]
		if !ok {
			continue
		}
		if x := f.Field(field); x != nil && x.Type == "file" {
			continue
		}
		switch val := v.(type) {
		case []any, []string:
			data[key] = f.Text(field, val)
		default:
			data[key] = val
		}
	}
	name := fmt.Sprintf("%s #%d", title, d.id)
	if d.author != "" {
		name += " – " + d.author
	}
	code, b, err := p.call(ctx, http.MethodPost, fmt.Sprintf("/api/v1/x/workflow.process/elements/%d/instances", t.Element),
		map[string]any{"title": clean(name, 200), "lang": d.lang, "data": data})
	if err != nil {
		return err
	}
	if code != http.StatusCreated && code != http.StatusOK {
		return apiError(code, b)
	}
	return nil
}

// ---------------------------------------------------------------- media library

// addMedia also stores the files of fields with a media category there.
func (p *Plugin) addMedia(ctx context.Context, f *Form, d *due) error {
	changed := false
	for i, fl := range d.files {
		x := f.Field(fl.Field)
		if x == nil || x.MediaCategory <= 0 || fl.Medium > 0 {
			continue
		}
		if !p.host.ACL.Can(ctx, identity.From(ctx), x.MediaCategory, acl.Edit) {
			return fmt.Errorf("the creator of the form may no longer upload into category %d", x.MediaCategory)
		}
		fh, err := p.host.Files.Open(fl.Hash)
		if err != nil {
			return err
		}
		m, err := p.host.Media.Upload(ctx, media.UploadInput{CategoryID: x.MediaCategory, Name: fl.Name, Title: fl.Name, Description: fl.Comment}, fh)
		_ = fh.Close()
		if err != nil {
			return err
		}
		d.files[i].Medium = m.ID
		changed = true
	}
	if changed {
		_, err := p.host.DB.Exec(ctx, `UPDATE plugin_form_submissions SET files=$2 WHERE id=$1`, d.id, d.files)
		return err
	}
	return nil
}

// ---------------------------------------------------------------- files

// record returns the submission as label/value pairs (reading order).
func (f *Form) record(d *due) [][2]string {
	var out [][2]string
	for _, x := range f.Fields {
		if !x.Input() || x.Type == "hidden" {
			continue
		}
		v, ok := d.data[x.ID]
		text := f.Text(x.ID, v)
		if x.Type == "file" || x.Type == "signature" {
			var names []string
			if text != "" {
				names = append(names, text)
			}
			for _, fl := range d.files {
				if fl.Field == x.ID {
					names = append(names, fl.Name)
				}
			}
			text, ok = strings.Join(names, ", "), len(names) > 0
		}
		if ok {
			out = append(out, [2]string{x.Label, text})
		}
	}
	return out
}

// storeFiles stores the submission as PDF, JSON, CSV or text in the media
// library (with the rights of the creator of the form).
func (p *Plugin) storeFiles(ctx context.Context, f *Form, d *due, title string) error {
	t := f.Targets.Files
	if t == nil {
		return nil
	}
	if !p.host.ACL.Can(ctx, identity.From(ctx), t.Category, acl.Edit) {
		return fmt.Errorf("the creator of the form may no longer upload into category %d", t.Category)
	}
	vars := f.vars(title, "", "", d.data, d.files, d.author)
	base := safeName(render(t.Name, vars, true))
	if strings.TrimSpace(t.Name) == "" {
		base = safeName(fmt.Sprintf("%s %d", title, d.id))
	}
	rec := f.record(d)
	var created time.Time
	_ = p.host.DB.QueryRow(ctx, `SELECT created_at FROM plugin_form_submissions WHERE id=$1`, d.id).Scan(&created)
	when := created.Format("2006-01-02 15:04")
	for _, format := range t.Formats {
		var data []byte
		switch format {
		case "pdf":
			lines := []pdfdoc.Line{{Text: title, Bold: true, Size: 16}, {Text: fmt.Sprintf("#%d · %s · %s", d.id, when, d.author), Size: 9, Space: 4}}
			for _, r := range rec {
				lines = append(lines, pdfdoc.Line{Text: r[0], Bold: true, Space: 8}, pdfdoc.Line{Text: r[1]})
			}
			data = pdfdoc.Write(fmt.Sprintf("%s #%d", title, d.id), d.lang, lines)
		case "json":
			data, _ = json.MarshalIndent(map[string]any{"form": title, "element_id": d.element, "submission": d.id, "submitted_at": created,
				"submitted_by": d.author, "data": d.data, "files": d.files}, "", "  ")
		case "csv":
			var b bytes.Buffer
			w := csv.NewWriter(&b)
			for _, r := range rec {
				_ = w.Write([]string{csvSafe(r[0]), csvSafe(r[1])})
			}
			w.Flush()
			data = append([]byte("\xef\xbb\xbf"), b.Bytes()...)
		case "txt":
			var b strings.Builder
			fmt.Fprintf(&b, "%s #%d\n%s %s\n\n", title, d.id, when, d.author)
			for _, r := range rec {
				fmt.Fprintf(&b, "%s: %s\n", r[0], r[1])
			}
			data = []byte(b.String())
		default:
			continue
		}
		if _, err := p.host.Media.Upload(ctx, media.UploadInput{CategoryID: t.Category, Name: base + "." + format, Title: base}, bytes.NewReader(data)); err != nil {
			return err
		}
	}
	return nil
}
