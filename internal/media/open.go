package media

// Opening files in office programs (Collabora, OnlyOffice, Microsoft 365).
//
// Office plugins register an Opener. The core decides which program and
// mode a user gets (never more than the user's rights), issues a
// short-lived access token for exactly one file, user and mode, and serves
// the file to the office program (WOPI host in package server, or the
// plugin's own endpoints). Every request with a token re-checks the rights.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Modes of an opened file.
const (
	ModeEdit = "edit"
	ModeView = "view"
)

// Internal is the viewer id of the browser (no office program).
const Internal = "internal"

// OpenerCaps describes what an office program can enforce.
type OpenerCaps struct {
	// Restrict: the program can hide download, print, export and copy, so
	// users without the right "download" may view (or edit) there.
	Restrict bool
	// Watermark: the program shows a text over the document (files with a
	// watermark for viewers).
	Watermark bool
	// Script: the browser loads a script from Origin (frontend API).
	Script bool
}

// Launch is what an Opener needs to start the program for one file.
type Launch struct {
	Medium     *Medium
	Mode       string // edit | view
	Restricted bool   // no download, print, export or copy
	Watermark  string // text to show over the document ("" = none)
	Token      string // access token (secret; for the office program only)
	Expires    time.Time
	Lang       string
	UserID     int64
	UserName   string
	BaseURL    string // public URL of the site (no trailing slash)
}

// Action tells the browser how to start the program.
type Action struct {
	// Kind: "wopi" (POST Form to URL into an iframe) or a kind handled by
	// the plugin's frontend (e.g. "onlyoffice").
	Kind   string            `json:"kind"`
	URL    string            `json:"url"`
	Form   map[string]string `json:"form,omitempty"`
	Script string            `json:"script,omitempty"`
	Config any               `json:"config,omitempty"`
	// Origin of the program (frame-src, form-action, script-src).
	Origin string `json:"origin"`
}

// Opener is an office program provided by a plugin.
type Opener interface {
	ID() string // plugin id
	Label() string
	Caps() OpenerCaps
	// Supports reports whether the program can view or edit a file.
	Supports(ctx context.Context, name, mime string) (view, edit bool)
	// Launch returns how the browser starts the program.
	Launch(ctx context.Context, l Launch) (*Action, error)
	// Origins are the scheme://host of the program (none = not
	// configured); the pages of the wiki allow them as frame (and script)
	// sources.
	Origins() []string
}

// WOPIPath is the path of the WOPI files endpoint: WOPISrc is the base URL
// of the site + WOPIPath + medium id.
const WOPIPath = "/api/v1/wopi/files/"

// ProofVerifier is implemented by WOPI openers that check the signature
// of the office server on every WOPI request (proof keys).
type ProofVerifier interface {
	VerifyProof(ctx context.Context, r *http.Request, token, fullURL string) error
}

// FileInfoExtender adds program specific fields to WOPI CheckFileInfo.
type FileInfoExtender interface {
	ExtendFileInfo(info map[string]any, t *Token, m *Medium)
}

// Options are per-plugin settings the core respects.
type OpenerOptions struct {
	// ReadOnlyWithoutDownload lets users without the right "download"
	// view in the program (only with OpenerCaps.Restrict).
	ReadOnlyWithoutDownload bool
}

// OptionsProvider is implemented by openers with settings.
type OptionsProvider interface{ Options() OpenerOptions }

type openers struct {
	mu    sync.RWMutex
	m     map[string]Opener
	paths []string // token paths of plugins
}

// RegisterOpener adds an office program (called by plugins in Setup).
func (s *Service) RegisterOpener(o Opener) {
	s.op.mu.Lock()
	defer s.op.mu.Unlock()
	if s.op.m == nil {
		s.op.m = map[string]Opener{}
	}
	s.op.m[o.ID()] = o
}

// Openers returns the registered programs of active plugins, sorted by label.
func (s *Service) Openers() []Opener {
	s.op.mu.RLock()
	defer s.op.mu.RUnlock()
	out := make([]Opener, 0, len(s.op.m))
	for id, o := range s.op.m {
		if s.Enabled == nil || s.Enabled(id) {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label() < out[j].Label() })
	return out
}

// Opener returns an active program by id.
func (s *Service) Opener(id string) Opener {
	for _, o := range s.Openers() {
		if o.ID() == id {
			return o
		}
	}
	return nil
}

// OpenOption is one way a user may open a file.
type OpenOption struct {
	Viewer     string   `json:"viewer"`
	Label      string   `json:"label"`
	Modes      []string `json:"modes"` // allowed modes, best first
	Restricted bool     `json:"restricted"`
	Default    bool     `json:"default"`
	External   bool     `json:"external"` // open there right away
}

func watermarked(m *Medium) bool {
	switch m.Watermark.Mode {
	case WMViewerView, WMViewerAll, WMUploaderView:
		return true
	}
	return false
}

// modesFor decides what the caller may do with m in o: never more than the
// rights (perms) allow.
func (s *Service) modesFor(ctx context.Context, o Opener, m *Medium, perms Perm) (modes []string, restricted bool) {
	view, edit := o.Supports(ctx, m.Name, m.Mime)
	if !view && !edit {
		return nil, false
	}
	caps := o.Caps()
	readOnly := false
	if op, ok := o.(OptionsProvider); ok {
		readOnly = op.Options().ReadOnlyWithoutDownload
	}
	if s.watermarked(ctx, m) && perms&Manage == 0 {
		// the program gets the original: only for viewing with its own
		// watermark and without any way to take the file out
		if view && caps.Watermark && caps.Restrict {
			return []string{ModeView}, true
		}
		return nil, false
	}
	download := perms&Download != 0
	if !download && !(caps.Restrict && readOnly) {
		return nil, false
	}
	restricted = !download
	if edit && perms&Edit != 0 {
		modes = append(modes, ModeEdit)
	}
	if view {
		modes = append(modes, ModeView)
	}
	return modes, restricted
}

// OpenOptions lists the programs the caller may open medium id with.
// The internal viewer is always first.
func (s *Service) OpenOptions(ctx context.Context, id int64, defViewer string, external bool) ([]OpenOption, error) {
	m, perms, err := s.require(ctx, id, View)
	if err != nil {
		return nil, err
	}
	out := []OpenOption{{Viewer: Internal, Label: "Browser", Modes: []string{ModeView}, Restricted: perms&Download == 0,
		Default: defViewer == "" || defViewer == Internal}}
	for _, o := range s.Openers() {
		modes, restricted := s.modesFor(ctx, o, m, perms)
		if len(modes) == 0 {
			continue
		}
		def := o.ID() == defViewer
		out = append(out, OpenOption{Viewer: o.ID(), Label: o.Label(), Modes: modes, Restricted: restricted, Default: def, External: def && external})
	}
	if !slices.ContainsFunc(out, func(o OpenOption) bool { return o.Default }) {
		out[0].Default = true // default program inactive or not suitable
	}
	return out, nil
}

// OpenInput selects program and mode ("" = default / best allowed).
type OpenInput struct {
	Viewer  string `json:"viewer"`
	Mode    string `json:"mode"`
	Lang    string `json:"-"`
	BaseURL string `json:"-"`
	TTL     time.Duration
}

// OpenResult is the answer of Open.
type OpenResult struct {
	Viewer     string  `json:"viewer"`
	Mode       string  `json:"mode"`
	Restricted bool    `json:"restricted"`
	Action     *Action `json:"action,omitempty"`
}

// Open starts the office program for medium id (issues an access token).
func (s *Service) Open(ctx context.Context, id int64, in OpenInput) (*OpenResult, error) {
	m, perms, err := s.require(ctx, id, View)
	if err != nil {
		return nil, err
	}
	if in.Viewer == "" || in.Viewer == Internal {
		return &OpenResult{Viewer: Internal, Mode: ModeView, Restricted: perms&Download == 0}, nil
	}
	p := identity.From(ctx)
	switch {
	case p.IsCLI():
		return nil, apperr.BadRequest("office programs need a user: use --as USER")
	case p != nil && (p.Via == "apikey" || p.Via == "feed" || p.Via == "office"):
		// the token would carry the user's full rights, not the key's limits
		return nil, apperr.New(403, "not_allowed", "office programs cannot be opened with an API key")
	}
	if in.Mode != "" && in.Mode != ModeEdit && in.Mode != ModeView {
		return nil, apperr.BadRequest("mode must be edit or view")
	}
	o := s.Opener(in.Viewer)
	if o == nil {
		return nil, apperr.BadRequest("unknown or inactive program")
	}
	modes, restricted := s.modesFor(ctx, o, m, perms)
	if len(modes) == 0 {
		return nil, apperr.New(403, "not_allowed", "this file cannot be opened in this program with your rights")
	}
	mode := modes[0]
	if in.Mode != "" {
		if !slices.Contains(modes, in.Mode) {
			return nil, apperr.New(403, "not_allowed", "this mode is not allowed with your rights")
		}
		mode = in.Mode
	}
	if in.TTL <= 0 {
		in.TTL = 10 * time.Hour
	}
	tok, exp, err := s.issueToken(ctx, m.ID, p.ID(), o.ID(), mode, restricted, in.TTL)
	if err != nil {
		return nil, err
	}
	l := Launch{Medium: m, Mode: mode, Restricted: restricted, Token: tok, Expires: exp, Lang: in.Lang,
		UserID: p.ID(), UserName: s.principalName(ctx), BaseURL: strings.TrimSuffix(in.BaseURL, "/")}
	if l.UserName == "" {
		l.UserName = "Guest"
	}
	if s.watermarked(ctx, m) && perms&Manage == 0 {
		l.Watermark = s.watermarkText(ctx, m)
	}
	a, err := o.Launch(ctx, l)
	if err != nil {
		s.log.Warn("office launch failed", "viewer", o.ID(), "media", m.ID, "err", err)
		return nil, apperr.New(502, "office_unavailable", "the office program is not available: "+err.Error())
	}
	return &OpenResult{Viewer: o.ID(), Mode: mode, Restricted: restricted, Action: a}, nil
}

// watermarkText is the text an office program shows over a watermarked file.
func (s *Service) watermarkText(ctx context.Context, m *Medium) string {
	name, t := viewerName(ctx), time.Now()
	spec := m.Watermark
	if !watermarked(m) { // watermark of the category (confidential): name and time
		spec = WatermarkSpec{Mode: WMViewerAll, User: true, Date: true, Comment: spec.Comment}
	} else if m.Watermark.Mode == WMUploaderView {
		name, t = s.userName(ctx, m.CreatedBy), m.CreatedAt
	}
	return strings.Join(lines(spec, name, t), " · ")
}

// watermarked: the medium's own watermark, or the one of its (confidential) category.
func (s *Service) watermarked(ctx context.Context, m *Medium) bool {
	return watermarked(m) || s.marked(ctx, m.CategoryID)
}

// ---------------------------------------------------------------- tokens

func hashToken(tok string) []byte {
	h := sha256.Sum256([]byte(tok))
	return h[:]
}

func (s *Service) issueToken(ctx context.Context, mediaID, userID int64, viewer, mode string, restricted bool, ttl time.Duration) (string, time.Time, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	tok := base64.RawURLEncoding.EncodeToString(b)
	exp := time.Now().Add(ttl).Truncate(time.Second)
	var uid *int64
	if userID > 0 {
		uid = &userID
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO media_access_tokens(token_hash, media_id, user_id, viewer, mode, restricted, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, hashToken(tok), mediaID, uid, viewer, mode, restricted, exp)
	if err != nil {
		return "", time.Time{}, err
	}
	// housekeeping: expired tokens of everybody
	_, _ = s.pool.Exec(ctx, `DELETE FROM media_access_tokens WHERE expires_at < now() - interval '1 hour'`)
	return tok, exp, nil
}

// Token is a validated access token.
type Token struct {
	MediaID    int64
	UserID     int64
	Viewer     string
	Mode       string
	Restricted bool
	Expires    time.Time
}

// ErrBadToken is returned for unknown, expired or mismatched tokens.
var ErrBadToken = apperr.New(401, "invalid_token", "invalid or expired access token")

// TokenContext validates tok for medium id and returns a context with the
// token's user as principal (Via "office"), after re-checking that the
// user still has the rights the token was issued for.
func (s *Service) TokenContext(ctx context.Context, id int64, tok string) (context.Context, *Token, *Medium, error) {
	if tok == "" || len(tok) > 200 {
		return ctx, nil, nil, ErrBadToken
	}
	t := &Token{}
	var uid *int64
	err := s.pool.QueryRow(ctx, `SELECT media_id, user_id, viewer, mode, restricted, expires_at FROM media_access_tokens
		WHERE token_hash=$1 AND expires_at > now()`, hashToken(tok)).Scan(&t.MediaID, &uid, &t.Viewer, &t.Mode, &t.Restricted, &t.Expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return ctx, nil, nil, ErrBadToken
	}
	if err != nil {
		return ctx, nil, nil, err
	}
	if t.MediaID != id {
		return ctx, nil, nil, ErrBadToken
	}
	if s.Opener(t.Viewer) == nil {
		return ctx, nil, nil, ErrBadToken // plugin switched off
	}
	var p *identity.Principal
	if uid != nil {
		t.UserID = *uid
		if s.LoadPrincipal == nil {
			return ctx, nil, nil, ErrBadToken
		}
		if p, err = s.LoadPrincipal(ctx, *uid); err != nil {
			return ctx, nil, nil, ErrBadToken // user deleted or disabled
		}
		p.Via = "office"
	}
	ctx = identity.With(ctx, p)
	want := View
	if t.Mode == ModeEdit {
		want |= Edit
	}
	m, perms, err := s.require(ctx, id, want)
	if err != nil {
		return ctx, nil, nil, ErrBadToken
	}
	// rights or file settings changed since the token was issued
	modes, restricted := s.modesFor(ctx, s.Opener(t.Viewer), m, perms)
	if !slices.Contains(modes, t.Mode) || (restricted && !t.Restricted) {
		return ctx, nil, nil, ErrBadToken
	}
	return ctx, t, m, nil
}

// RevokeTokens removes all tokens of a medium (e.g. after rights changed).
func (s *Service) RevokeTokens(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM media_access_tokens WHERE media_id=$1`, id)
	return err
}

// ---------------------------------------------------------------- file access for office programs

// Content returns the current file of a medium opened with a token
// (ctx from TokenContext).
func (s *Service) Content(ctx context.Context, m *Medium) ([]byte, error) {
	return s.read(m.hash)
}

// SaveFromOffice stores data as a new version (token mode "edit"; ctx from
// TokenContext). The version note names the program.
func (s *Service) SaveFromOffice(ctx context.Context, m *Medium, t *Token, r io.Reader) (*Medium, error) {
	if t.Mode != ModeEdit {
		return nil, apperr.ErrForbidden
	}
	data, err := io.ReadAll(io.LimitReader(r, s.files.MaxBytes()+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > s.files.MaxBytes() {
		return nil, apperr.New(413, "too_large", "file too large")
	}
	label := t.Viewer
	if o := s.Opener(t.Viewer); o != nil {
		label = o.Label()
	}
	if m.Watermark.Mode == WMUploaderOnly {
		// keep the rule: the original never exists without the mark (the
		// office program saves the marked file it got; images only)
		if editable(m.Mime) {
			if data, err = s.mark(ctx, data, m.Watermark, s.userName(ctx, m.CreatedBy), m.CreatedAt); err != nil {
				return nil, err
			}
		}
	}
	return s.addVersion(ctx, m, data, nil, label, "media.office_save")
}

// Ext returns the lower-case extension of a file name without the dot.
func Ext(name string) string {
	return strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
}

// ---------------------------------------------------------------- locks (WOPI)

// ErrLockMismatch is returned when a different lock is held; Current is the
// lock in place ("" = none).
type ErrLockMismatch struct{ Current string }

func (e *ErrLockMismatch) Error() string { return "lock mismatch" }

const lockTTL = 30 * time.Minute

// CurrentLock returns the lock of a medium ("" = none or expired).
func (s *Service) CurrentLock(ctx context.Context, id int64) (string, error) {
	var l string
	err := s.pool.QueryRow(ctx, `SELECT lock_id FROM media_locks WHERE media_id=$1 AND expires_at > now()`, id).Scan(&l)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return l, err
}

// Lock sets, refreshes or replaces (oldLock != "") the lock of a medium.
func (s *Service) Lock(ctx context.Context, id int64, lock, oldLock string) error {
	if lock == "" || len(lock) > 1024 {
		return apperr.BadRequest("invalid lock")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var cur string
	err = tx.QueryRow(ctx, `SELECT lock_id FROM media_locks WHERE media_id=$1 AND expires_at > now() FOR UPDATE`, id).Scan(&cur)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	want := lock
	if oldLock != "" {
		want = oldLock
	}
	if cur != "" && cur != want {
		return &ErrLockMismatch{Current: cur}
	}
	if cur == "" && oldLock != "" {
		return &ErrLockMismatch{Current: ""}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO media_locks(media_id, lock_id, expires_at) VALUES ($1,$2,$3)
		ON CONFLICT (media_id) DO UPDATE SET lock_id=EXCLUDED.lock_id, expires_at=EXCLUDED.expires_at`, id, lock, time.Now().Add(lockTTL)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Unlock removes the lock if it matches.
func (s *Service) Unlock(ctx context.Context, id int64, lock string) error {
	cur, err := s.CurrentLock(ctx, id)
	if err != nil {
		return err
	}
	if cur != lock {
		return &ErrLockMismatch{Current: cur}
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM media_locks WHERE media_id=$1 AND lock_id=$2`, id, lock)
	return err
}

// OfficeWatermark returns the text an office program must show over m for
// the caller ("" = none; ctx from TokenContext).
func (s *Service) OfficeWatermark(ctx context.Context, m *Medium) string {
	if !s.watermarked(ctx, m) {
		return ""
	}
	perms, err := s.Perms(ctx, m)
	if err != nil || perms&Manage != 0 {
		return ""
	}
	return s.watermarkText(ctx, m)
}

// ItemVersion is the version string office programs see for m.
func ItemVersion(m *Medium) string { return "v" + strconv.Itoa(m.Version) }

// RegisterTokenPath marks a path prefix of a plugin endpoint that office
// servers call with an access token (no session or API key is evaluated
// there; the handler must call TokenContext).
func (s *Service) RegisterTokenPath(prefix string) {
	s.op.mu.Lock()
	defer s.op.mu.Unlock()
	if strings.HasPrefix(prefix, "/api/v1/x/") && strings.HasSuffix(prefix, "/") && !slices.Contains(s.op.paths, prefix) {
		s.op.paths = append(s.op.paths, prefix)
	}
}

// TokenPath reports whether path is called by office servers with an
// access token (WOPI or a registered plugin path).
func (s *Service) TokenPath(path string) bool {
	if strings.HasPrefix(path, WOPIPath) {
		return true
	}
	s.op.mu.RLock()
	defer s.op.mu.RUnlock()
	for _, p := range s.op.paths {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
