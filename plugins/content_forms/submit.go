package content_forms

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/mail"
	"github.com/mtsonline/enterprisewiki/internal/webhook"
)

// StoredFile is an uploaded file of a submission.
type StoredFile struct {
	Field   string `json:"field"`
	Name    string `json:"name"`
	Hash    string `json:"hash"`
	Size    int64  `json:"size"`
	Mime    string `json:"mime"`
	Comment string `json:"comment,omitempty"`
	Medium  int64  `json:"medium,omitempty"` // also in the media library
}

// MinAge is the minimum time between showing the form and submitting it
// (bots submit at once).
var MinAge = 2 * time.Second

// ---------------------------------------------------------------- tokens

// secret returns the secret of the form tokens (created once per site).
func (p *Plugin) secret(ctx context.Context) ([]byte, error) {
	var s []byte
	err := p.host.DB.QueryRow(ctx, `SELECT secret FROM plugin_form_secret WHERE id=1`).Scan(&s)
	if errors.Is(err, pgx.ErrNoRows) {
		s = make([]byte, 32)
		if _, err := rand.Read(s); err != nil {
			return nil, err
		}
		if _, err := p.host.DB.Exec(ctx, `INSERT INTO plugin_form_secret(id, secret) VALUES (1, $1) ON CONFLICT (id) DO NOTHING`, s); err != nil {
			return nil, err
		}
		err = p.host.DB.QueryRow(ctx, `SELECT secret FROM plugin_form_secret WHERE id=1`).Scan(&s)
	}
	return s, err
}

func (p *Plugin) sign(ctx context.Context, el, uid, at int64) (string, error) {
	key, err := p.secret(ctx)
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	fmt.Fprintf(m, "%d|%d|%d", el, uid, at)
	return strconv.FormatInt(at, 10) + "." + hex.EncodeToString(m.Sum(nil)), nil
}

// checkToken verifies a token of the form for the caller: signed, at least
// MinAge and at most a day old.
func (p *Plugin) checkToken(ctx context.Context, el, uid int64, tok string) error {
	ts, sig, ok := strings.Cut(tok, ".")
	at, err := strconv.ParseInt(ts, 10, 64)
	if !ok || err != nil {
		return apperr.BadRequest("the form is outdated: please reload the page")
	}
	want, err := p.sign(ctx, el, uid, at)
	if err != nil {
		return err
	}
	_, wsig, _ := strings.Cut(want, ".")
	if !hmac.Equal([]byte(sig), []byte(wsig)) {
		return apperr.BadRequest("the form is outdated: please reload the page")
	}
	age := time.Since(time.UnixMilli(at))
	if age < MinAge {
		return apperr.TooMany("please take a moment before submitting")
	}
	if age > 24*time.Hour {
		return apperr.BadRequest("the form is outdated: please reload the page")
	}
	return nil
}

// ---------------------------------------------------------------- limits

// limiter counts submissions per key in a sliding hour.
type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newLimiter() *limiter { return &limiter{hits: map[string][]time.Time{}} }

func (l *limiter) allow(key string, max int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.hits) > 50000 {
		l.hits = map[string][]time.Time{}
	}
	h := l.hits[key]
	i := 0
	for ; i < len(h) && now.Sub(h[i]) > time.Hour; i++ {
	}
	h = h[i:]
	if len(h) >= max {
		l.hits[key] = h
		return false
	}
	l.hits[key] = append(h, now)
	return true
}

// clientIP is the address of the caller (behind trusted proxies the core
// passes the client's address in the audit context).
func clientIP(r *http.Request) string {
	if ip := audit.ClientIP(r.Context()); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---------------------------------------------------------------- loading

// load returns the form of element id (with view right) in lang.
func (p *Plugin) load(ctx context.Context, id int64, lang string) (*Form, int64, string, string, error) {
	cat, err := p.host.Content.ElementCategory(ctx, id, acl.View)
	if err != nil {
		return nil, 0, "", "", err
	}
	var typ, shown, title string
	var body []byte
	err = p.host.DB.QueryRow(ctx, `SELECT e.type, r.lang, r.title, r.body FROM elements e JOIN element_revisions r ON r.element_id = e.id
		WHERE e.id=$1 AND r.lang = ANY(ARRAY[$2, e.original_lang]) ORDER BY (r.lang=$2) DESC, r.rev DESC LIMIT 1`, id, lang).Scan(&typ, &shown, &title, &body)
	if err != nil {
		return nil, 0, "", "", apperr.NotFound("form")
	}
	if typ != TypeID {
		return nil, 0, "", "", apperr.NotFound("form")
	}
	f, err := Decode(body)
	if err != nil {
		return nil, 0, "", "", err
	}
	return f, cat, shown, title, nil
}

// openState explains why a form cannot be submitted ("" = open).
func (p *Plugin) openState(ctx context.Context, f *Form, id int64) (string, error) {
	now := time.Now()
	me := identity.From(ctx)
	switch {
	case f.Opens != nil && now.Before(*f.Opens):
		return "not_open", nil
	case f.Closes != nil && now.After(*f.Closes):
		return "closed", nil
	case (f.SignedIn || f.OncePerUser) && me.ID() <= 0:
		return "sign_in", nil
	}
	if f.MaxEntries > 0 {
		var n int
		if err := p.host.DB.QueryRow(ctx, `SELECT count(*) FROM plugin_form_submissions WHERE element_id=$1`, id).Scan(&n); err != nil {
			return "", err
		}
		if n >= f.MaxEntries {
			return "full", nil
		}
	}
	if f.OncePerUser && me.ID() > 0 {
		var done bool
		if err := p.host.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM plugin_form_submissions WHERE element_id=$1 AND user_id=$2)`, id, me.ID()).Scan(&done); err != nil {
			return "", err
		}
		if done {
			return "already", nil
		}
	}
	return "", nil
}

// ---------------------------------------------------------------- start

// start answers GET /elements/{id}/start: the token, whether the form is
// open and the prefilled values of the caller.
func (p *Plugin) start(w http.ResponseWriter, r *http.Request) (any, error) {
	id, err := h.PathID(r, "id")
	if err != nil {
		return nil, err
	}
	ctx := r.Context()
	f, _, _, _, err := p.load(ctx, id, r.URL.Query().Get("lang"))
	if err != nil {
		return nil, err
	}
	state, err := p.openState(ctx, f, id)
	if err != nil {
		return nil, err
	}
	me := identity.From(ctx)
	tok, err := p.sign(ctx, id, me.ID(), time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	prefill := map[string]string{}
	if me.ID() > 0 {
		for _, x := range f.Fields {
			switch x.Prefill {
			case "user.name":
				prefill[x.ID] = me.DisplayName
			case "user.email":
				prefill[x.ID] = me.Email
			case "user.username":
				prefill[x.ID] = me.Username
			}
		}
	}
	return map[string]any{"token": tok, "state": state, "prefill": prefill, "signed_in": me.ID() > 0}, nil
}

// ---------------------------------------------------------------- submit

type submitInput struct {
	Values  map[string]json.RawMessage `json:"values"`
	Token   string                     `json:"token"`
	Website string                     `json:"website"` // hidden field: filled only by bots
	Lang    string                     `json:"lang"`
}

type upload struct {
	field, name, path, comment string
}

// submit answers POST /elements/{id}/submit (multipart: "data" with the
// JSON of submitInput, files "file.FIELD", comments "comment.FIELD.N").
func (p *Plugin) submit(w http.ResponseWriter, r *http.Request) (any, error) {
	id, err := h.PathID(r, "id")
	if err != nil {
		return nil, err
	}
	ctx := r.Context()
	me := identity.From(ctx)
	limitKey, max := "ip:"+clientIP(r), 20
	if me.ID() > 0 {
		limitKey, max = "u:"+strconv.FormatInt(me.ID(), 10), 60
	}
	if !p.limiter.allow(fmt.Sprintf("%d|%s", id, limitKey), max) {
		return nil, apperr.TooMany("too many submissions, please try again later")
	}
	// the form is loaded first: its file limits bound the request
	lang := r.URL.Query().Get("lang")
	f, cat, shown, title, err := p.load(ctx, id, lang)
	if err != nil {
		return nil, err
	}
	var limit int64 = 1 << 20
	for _, x := range f.Fields {
		if x.Type == "file" {
			limit += int64(x.MaxFiles) * int64(x.MaxSizeMB) << 20
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, min(limit, p.host.Files.MaxBytes()*MaxFilesField))
	in := submitInput{}
	var ups []upload
	tmp, err := os.MkdirTemp("", "ewiki-form-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if ups, err = p.readMultipart(r, f, tmp, &in); err != nil {
			return nil, err
		}
	} else if err := h.Decode(r, &in); err != nil {
		return nil, err
	}
	if err := p.checkToken(ctx, id, me.ID(), in.Token); err != nil {
		return nil, err
	}
	if in.Website != "" {
		// a bot filled the hidden field: answered like a success, nothing is stored
		return map[string]any{"id": 0, "message": f.Success}, nil
	}
	state, err := p.openState(ctx, f, id)
	if err != nil {
		return nil, err
	}
	if state != "" {
		return nil, apperr.Conflict("%s", stateMessage(state))
	}
	counts := map[string]int{}
	for _, u := range ups {
		counts[u.field]++
	}
	values, errs := f.CheckValues(in.Values, counts)
	// files of fields hidden by their condition are dropped
	kept := ups[:0]
	for _, u := range ups {
		if x := f.Field(u.field); x != nil && x.Visible(values) {
			kept = append(kept, u)
		}
	}
	ups = kept
	var files []StoredFile
	if len(errs) == 0 {
		for _, u := range ups {
			x := f.Field(u.field)
			if err := p.checkFile(ctx, x, u.name, u.path); err != nil {
				var rej *ErrRejected
				if errors.As(err, &rej) {
					errs = append(errs, FieldError{Field: u.field, Code: rej.Code, Param: rej.Name})
					continue
				}
				return nil, err
			}
		}
	}
	if len(errs) > 0 {
		h.Write(w, http.StatusUnprocessableEntity, map[string]any{"error": map[string]any{"code": "invalid", "message": "please check the marked fields", "fields": errs}})
		return h.Handled{}, nil
	}
	for _, u := range ups {
		fh, err := os.Open(u.path) // #nosec G304 -- temporary file of this upload
		if err != nil {
			return nil, err
		}
		stored, err := p.host.Files.Put(ctx, fh, u.name)
		_ = fh.Close()
		if err != nil {
			return nil, err
		}
		files = append(files, StoredFile{Field: u.field, Name: stored.Name, Hash: stored.Hash, Size: stored.Size, Mime: stored.Mime, Comment: u.comment})
	}
	if files == nil {
		files = []StoredFile{}
	}
	pending := f.pendingTargets(files)
	author := ""
	var uid *int64
	if me.ID() > 0 {
		author = me.DisplayName
		if author == "" {
			author = me.Handle()
		}
		v := me.ID()
		uid = &v
	}
	var sid int64
	err = pgx.BeginFunc(ctx, p.host.DB, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO plugin_form_submissions(element_id, lang, data, files, user_id, author, pending, next_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7, CASE WHEN jsonb_array_length($7::jsonb) > 0 THEN now() END) RETURNING id`,
			id, shown, values, files, uid, author, pending).Scan(&sid); err != nil {
			return err
		}
		// e-mails go into the queue with the submission (same transaction)
		return p.queueMails(ctx, tx, f, cat, id, sid, title, shown, values, files)
	})
	if err != nil {
		return nil, err
	}
	// webhooks of the administration: that and where, never the values
	p.host.Webhooks.Emit(ctx, webhook.Event{Name: "form.submitted", CategoryID: cat, Actor: author,
		Object: webhook.Object{Type: "form", ID: id, Title: title, URL: fmt.Sprintf("/e/%d", id)}, Data: map[string]any{"submission": sid}})
	msg := f.Success
	if msg == "" {
		msg = p.host.T(ctx, shown, "forms.thanks", nil)
	}
	return h.Created{V: map[string]any{"id": sid, "message": msg}}, nil
}

func stateMessage(s string) string {
	switch s {
	case "not_open":
		return "the form is not open yet"
	case "closed":
		return "the form is closed"
	case "sign_in":
		return "please sign in to submit this form"
	case "full":
		return "the form takes no more submissions"
	case "already":
		return "you have already submitted this form"
	}
	return s
}

// readMultipart reads the parts of a submission: the JSON of the values and
// the files (to temporary files, each within the limit of its field).
func (p *Plugin) readMultipart(r *http.Request, f *Form, dir string, in *submitInput) ([]upload, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, apperr.BadRequest("invalid upload")
	}
	var ups []upload
	comments := map[string]string{}
	n := 0
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				return nil, apperr.New(http.StatusRequestEntityTooLarge, "too_large", "the files are too large")
			}
			return nil, apperr.BadRequest("invalid upload")
		}
		name := part.FormName()
		switch {
		case name == "data":
			if err := json.NewDecoder(io.LimitReader(part, 1<<20)).Decode(in); err != nil {
				return nil, apperr.BadRequest("invalid form data")
			}
		case strings.HasPrefix(name, "comment."):
			b, _ := io.ReadAll(io.LimitReader(part, 2000))
			comments[strings.TrimPrefix(name, "comment.")] = clean(string(b), 1000)
		case strings.HasPrefix(name, "file."):
			x := f.Field(strings.TrimPrefix(name, "file."))
			if x == nil || x.Type != "file" {
				return nil, apperr.BadRequest("unknown file field")
			}
			if n++; n > MaxFields*MaxFilesField {
				return nil, apperr.BadRequest("too many files")
			}
			u, err := saveTemp(part, dir, x)
			if err != nil {
				return nil, err
			}
			u.comment = comments[fmt.Sprintf("%s.%d", x.ID, countField(ups, x.ID))]
			ups = append(ups, u)
		}
		_ = part.Close()
	}
	// comments may come after their files
	idx := map[string]int{}
	for i := range ups {
		k := fmt.Sprintf("%s.%d", ups[i].field, idx[ups[i].field])
		idx[ups[i].field]++
		if c, ok := comments[k]; ok {
			ups[i].comment = c
		}
	}
	return ups, nil
}

func countField(ups []upload, field string) int {
	n := 0
	for _, u := range ups {
		if u.field == field {
			n++
		}
	}
	return n
}

func saveTemp(part *multipart.Part, dir string, x *Field) (upload, error) {
	name := filepath.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
	name = clean(strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name), 200)
	if name == "" || name == "." {
		name = "file"
	}
	out, err := os.CreateTemp(dir, "up-*")
	if err != nil {
		return upload{}, err
	}
	defer func() { _ = out.Close() }()
	max := int64(x.MaxSizeMB) << 20
	n, err := io.Copy(out, io.LimitReader(part, max+1))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return upload{}, apperr.New(http.StatusRequestEntityTooLarge, "too_large", "the files are too large")
		}
		return upload{}, err
	}
	if n > max {
		return upload{}, apperr.New(http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("%s is larger than %d MB", name, x.MaxSizeMB))
	}
	if n == 0 {
		return upload{}, apperr.BadRequest("%s is empty", name)
	}
	return upload{field: x.ID, name: name, path: out.Name()}, nil
}

// ---------------------------------------------------------------- e-mails

// placeholders of e-mails and titles
func (f *Form) vars(title, formURL, subURL string, values map[string]any, files []StoredFile, author string) map[string]string {
	v := map[string]string{"form.title": title, "form.url": formURL, "submission.url": subURL, "submitter.name": author}
	var all []string
	for _, x := range f.Fields {
		if !x.Input() {
			continue
		}
		val, ok := values[x.ID]
		if x.Type == "file" {
			var names []string
			for _, fl := range files {
				if fl.Field == x.ID {
					names = append(names, fl.Name)
				}
			}
			val, ok = strings.Join(names, ", "), len(names) > 0
		}
		text := f.Text(x.ID, val)
		v["field."+x.ID] = text
		if ok && text != "" {
			all = append(all, x.Label+": "+text)
		}
		if x.Type == "email" && v["submitter.email"] == "" {
			v["submitter.email"] = text
		}
	}
	v["all"] = strings.Join(all, "\n")
	return v
}

func render(tpl string, vars map[string]string, oneLine bool) string {
	out := tpl
	for k, val := range vars {
		if oneLine {
			val = strings.Join(strings.Fields(val), " ")
		}
		out = strings.ReplaceAll(out, "{{"+k+"}}", val)
	}
	return out
}

// queueMails queues the e-mails of a submission: to the targets and a copy
// to the submitter. Values of confidential forms never go into e-mails,
// only the link.
func (p *Plugin) queueMails(ctx context.Context, tx pgx.Tx, f *Form, cat, id, sid int64, title, lang string, values map[string]any, files []StoredFile) error {
	if len(f.Targets.Email) == 0 && !f.Copy {
		return nil
	}
	base := strings.TrimSuffix(p.host.Config.Get().Server.BaseURL, "/")
	formURL := fmt.Sprintf("%s/e/%d", base, id)
	subURL := fmt.Sprintf("%s/e/%d?submission=%d", base, id, sid)
	me := identity.From(ctx)
	author := ""
	if me.ID() > 0 {
		author = me.DisplayName
	}
	vars := f.vars(title, formURL, subURL, values, files, author)
	confidential := p.host.ACL.Classification(ctx, cat) >= acl.Confidential
	tr := func(key string) string { return p.host.T(ctx, lang, key, map[string]string{"form": title}) }
	for _, e := range f.Targets.Email {
		to, err := p.resolveRecipients(ctx, tx, e.To, values)
		if err != nil || len(to) == 0 {
			continue
		}
		subject := render(e.Subject, vars, true)
		if subject == "" {
			subject = tr("forms.mail_subject")
		}
		body := render(e.Body, vars, false)
		if strings.TrimSpace(e.Body) == "" {
			body = tr("forms.mail_intro") + "\n\n" + vars["all"] + "\n\n" + subURL
		}
		if confidential {
			body = tr("forms.mail_confidential") + "\n\n" + subURL
		}
		if _, err := p.host.Mail.Enqueue(ctx, tx, mail.Message{To: to, Subject: subject, Text: body, Source: "content.forms",
			Ref: fmt.Sprintf("form:%d:%d", id, sid), Owner: -1}); err != nil {
			return err
		}
	}
	if f.Copy && me.ID() > 0 && me.Email != "" {
		body := tr("forms.copy_intro") + "\n\n" + vars["all"]
		if confidential {
			body = tr("forms.copy_intro") + "\n\n" + formURL
		}
		if _, err := p.host.Mail.Enqueue(ctx, tx, mail.Message{To: []string{me.Email}, Subject: tr("forms.copy_subject"), Text: body,
			Source: "content.forms", Ref: fmt.Sprintf("form:%d:%d", id, sid), Owner: -1}); err != nil {
			return err
		}
	}
	return nil
}

// resolveRecipients turns an address list into addresses: plain addresses,
// user:NAME, group:NAME and field:ID (an e-mail field of the submission).
func (p *Plugin) resolveRecipients(ctx context.Context, q pgx.Tx, spec string, values map[string]any) ([]string, error) {
	var out []string
	for _, r := range Recipients(spec) {
		switch {
		case strings.HasPrefix(r, "user:"):
			var e string
			if err := q.QueryRow(ctx, `SELECT email FROM users WHERE lower(username)=lower($1) AND NOT disabled AND email <> ''`, strings.TrimPrefix(r, "user:")).Scan(&e); err == nil {
				out = append(out, e)
			}
		case strings.HasPrefix(r, "group:"):
			rows, err := q.Query(ctx, `SELECT u.email FROM users u JOIN user_groups ug ON ug.user_id=u.id JOIN groups g ON g.id=ug.group_id
				WHERE (lower(g.name)=lower($1) OR '#' || g.id::text = $1) AND NOT u.disabled AND u.email <> '' LIMIT 500`, strings.TrimPrefix(r, "group:"))
			if err != nil {
				return nil, err
			}
			list, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return nil, err
			}
			out = append(out, list...)
		case strings.HasPrefix(r, "field:"):
			if s, ok := values[strings.TrimPrefix(r, "field:")].(string); ok && s != "" {
				out = append(out, s)
			}
		default:
			out = append(out, r)
		}
	}
	return out, nil
}
