// Package signature lets people sign records (e.g. decisions in workflows)
// with signature methods provided by plugins (passkey, S/MIME, qualified
// remote signatures …) and keeps verifiable evidence.
//
// What is signed is a canonical JSON record built by the feature (never by
// the browser); its SHA-256 digest goes to the method. The evidence –
// record, signer, method, method proof – is additionally sealed by the
// wiki with a hybrid signature (Ed25519 and ML-DSA-65), so that any later
// change of evidence or record shows when it is verified. Everybody with
// the export and the public keys can verify it offline.
package signature

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/secrets"
)

// Levels of signatures (eIDAS): advanced or qualified.
const (
	LevelAdvanced  = "advanced"
	LevelQualified = "qualified"
)

// PendingTTL is how long a started signature waits for its completion.
const PendingTTL = 10 * time.Minute

// sealContext separates the seal from any other use of the keys.
const sealContext = "ewiki-signature-seal-v1"

// Request is what a method signs: the digest of the record for a person.
type Request struct {
	UserID int64
	Lang   string
	Digest []byte // SHA-256 of the canonical record
	Record json.RawMessage
}

// Challenge is what the browser does to sign: "webauthn" (options for
// navigator.credentials.get) or "redirect" (open URL, come back).
type Challenge struct {
	Kind   string `json:"kind"`
	Public any    `json:"public,omitempty"`
	URL    string `json:"url,omitempty"`
}

// Check is one step of a verification.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Method is a way to sign, provided by a plugin of the kind "signature".
type Method interface {
	// ID is the plugin id (e.g. "signature.passkey").
	ID() string
	// Level is LevelAdvanced or LevelQualified.
	Level() string
	Label(lang string) string
	// Available reports whether the person can sign with it (e.g. has a
	// passkey).
	Available(ctx context.Context, uid int64) bool
	// Begin starts signing; state is kept by the core until Finish.
	Begin(ctx context.Context, req Request) (ch Challenge, state string, err error)
	// Finish checks the answer of the browser and returns the proof (all
	// that is needed to verify it later, JSON).
	Finish(ctx context.Context, req Request, state string, answer json.RawMessage) (json.RawMessage, error)
}

// Verifier checks a proof of a method offline (also in the CLI).
type Verifier func(digest []byte, proof json.RawMessage) []Check

var (
	verMu     sync.RWMutex
	verifiers = map[string]Verifier{}
)

// RegisterVerifier registers the offline check of a method (from init).
func RegisterVerifier(method string, v Verifier) {
	verMu.Lock()
	defer verMu.Unlock()
	verifiers[method] = v
}

func verifier(method string) Verifier {
	verMu.RLock()
	defer verMu.RUnlock()
	return verifiers[method]
}

// Viewer decides whether the caller may see the signatures of a reference
// (e.g. "workflow_case:12"); registered by the feature that signs.
type Viewer func(ctx context.Context, ref string) bool

// Service keeps methods, pending signatures and evidence of one site.
type Service struct {
	pool    *pgxpool.Pool
	Box     *secrets.Box
	Enabled func(pluginID string) bool

	mu      sync.RWMutex
	methods map[string]Method
	viewers map[string]Viewer
	keys    *keyPair
	cms     CMSSealer
}

// CMSSealer seals evidence additionally with an X.509 certificate of the
// organisation (detached CMS SignedData over the same bytes as the seal of
// the wiki), so it can be checked with standard tools (OpenSSL). It returns
// nil when no certificate is set up or valid; an error aborts signing.
type CMSSealer func(ctx context.Context, q db.Querier, msg []byte) ([]byte, error)

// SetCMSSealer sets the additional seal (plugin signature.smime; nil: none).
func (s *Service) SetCMSSealer(f CMSSealer) {
	s.mu.Lock()
	s.cms = f
	s.mu.Unlock()
}

// CMSVerifier checks an additional CMS seal over msg.
type CMSVerifier func(msg, der []byte) Check

var (
	cmsMu       sync.RWMutex
	cmsVerifier CMSVerifier
)

// RegisterCMSVerifier sets the check of CMS seals (in init of the plugin that
// creates them, so offline checks of the CLI work too).
func RegisterCMSVerifier(v CMSVerifier) {
	cmsMu.Lock()
	cmsVerifier = v
	cmsMu.Unlock()
}

// New returns the service.
func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, methods: map[string]Method{}, viewers: map[string]Viewer{}}
}

// Register adds a method (inactive while its plugin is disabled).
func (s *Service) Register(m Method) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.methods[m.ID()] = m
}

// RegisterViewer sets who may see the signatures of a purpose.
func (s *Service) RegisterViewer(purpose string, v Viewer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.viewers[purpose] = v
}

func (s *Service) method(id string) Method {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.methods[id]
	if m == nil || (s.Enabled != nil && !s.Enabled(id)) {
		return nil
	}
	return m
}

// MethodInfo describes a method for a person.
type MethodInfo struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Level     string `json:"level"`
	Available bool   `json:"available"`
}

// Methods lists the active methods (available to uid first).
func (s *Service) Methods(ctx context.Context, uid int64, lang string) []MethodInfo {
	s.mu.RLock()
	list := make([]Method, 0, len(s.methods))
	for id, m := range s.methods {
		if s.Enabled == nil || s.Enabled(id) {
			list = append(list, m)
		}
	}
	s.mu.RUnlock()
	out := make([]MethodInfo, 0, len(list))
	for _, m := range list {
		out = append(out, MethodInfo{ID: m.ID(), Label: m.Label(lang), Level: m.Level(), Available: uid != 0 && m.Available(ctx, uid)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Available != out[j].Available {
			return out[i].Available
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Satisfies reports whether a method meets a requirement: "advanced" (any
// method), "qualified" (only qualified ones) or the id of a method.
func Satisfies(req string, m MethodInfo) bool {
	switch req {
	case "", LevelAdvanced:
		return true
	case LevelQualified:
		return m.Level == LevelQualified
	}
	return m.ID == req
}

// Canonical encodes a record as canonical JSON (sorted keys, no spaces).
func Canonical(v any) (json.RawMessage, []byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	var x any // re-encode: maps of structs get sorted keys as well; numbers stay exact
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&x); err != nil {
		return nil, nil, err
	}
	if b, err = json.Marshal(x); err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(b)
	return b, sum[:], nil
}

// Start describes a signature to begin.
type Start struct {
	Purpose string // e.g. "workflow.decision"
	Ref     string // e.g. "workflow_case:12"
	Method  string
	Require string // "advanced", "qualified" or a method id
	Lang    string
	Record  any
	// Payload is kept with the pending signature and returned by Complete
	// (e.g. the note and the form of the decision).
	Payload any
}

// Begin starts a signature for the caller: builds the canonical record,
// asks the method and keeps everything for Complete. token goes to the
// browser together with the challenge.
func (s *Service) Begin(ctx context.Context, st Start) (token string, ch Challenge, err error) {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return "", ch, apperr.ErrUnauthorized
	}
	m := s.method(st.Method)
	if m == nil {
		return "", ch, apperr.BadRequest("unknown signature method %q", st.Method)
	}
	info := MethodInfo{ID: m.ID(), Level: m.Level()}
	if !Satisfies(st.Require, info) {
		return "", ch, apperr.BadRequest("this signature needs %s", st.Require)
	}
	if !m.Available(ctx, p.UserID) {
		return "", ch, apperr.BadRequest("you cannot sign with %s yet (set it up in your profile)", m.Label(st.Lang))
	}
	rec, digest, err := Canonical(st.Record)
	if err != nil {
		return "", ch, err
	}
	payload, err := json.Marshal(st.Payload)
	if err != nil {
		return "", ch, err
	}
	ch, state, err := m.Begin(ctx, Request{UserID: p.UserID, Lang: st.Lang, Digest: digest, Record: rec})
	if err != nil {
		return "", ch, err
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", ch, err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	th := sha256.Sum256([]byte(token))
	if _, err := s.pool.Exec(ctx, `DELETE FROM signature_pending WHERE expires_at < now()`); err != nil {
		return "", ch, err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO signature_pending(token_hash, user_id, method, purpose, ref, require, digest, record, payload, state, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, th[:], p.UserID, m.ID(), st.Purpose, st.Ref, st.Require, digest, rec, payload, state, time.Now().Add(PendingTTL))
	return token, ch, err
}

// Pending is a started signature as Complete returns it.
type Pending struct {
	Purpose, Ref, Method string
	Digest               []byte
	Record               json.RawMessage
	Payload              json.RawMessage
}

// Complete finishes a signature of the caller: the method checks the
// answer; check gets the pending signature and must confirm that it still
// fits (e.g. rebuild the record from the current state and compare the
// digest). The evidence is stored and sealed in tx; returns its id.
func (s *Service) Complete(ctx context.Context, tx pgx.Tx, token, lang string, answer json.RawMessage, check func(Pending) error) (int64, Pending, error) {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return 0, Pending{}, apperr.ErrUnauthorized
	}
	th := sha256.Sum256([]byte(token))
	var pd Pending
	var uid int64
	var state string
	var expires time.Time
	// one attempt per token: it is gone whatever the outcome (outside the
	// transaction, which may roll back)
	err := s.pool.QueryRow(ctx, `DELETE FROM signature_pending WHERE token_hash=$1
		RETURNING user_id, method, purpose, ref, digest, record, payload, state, expires_at`, th[:]).
		Scan(&uid, &pd.Method, &pd.Purpose, &pd.Ref, &pd.Digest, &pd.Record, &pd.Payload, &state, &expires)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (uid != p.UserID || time.Now().After(expires))) {
		return 0, pd, apperr.BadRequest("the signature has expired or is not yours – please sign again")
	}
	if err != nil {
		return 0, pd, err
	}
	if err := check(pd); err != nil {
		return 0, pd, err
	}
	m := s.method(pd.Method)
	if m == nil {
		return 0, pd, apperr.BadRequest("the signature method %q is no longer available", pd.Method)
	}
	proof, err := m.Finish(ctx, Request{UserID: uid, Lang: lang, Digest: pd.Digest, Record: pd.Record}, state, answer)
	if err != nil {
		return 0, pd, apperr.BadRequest("signature not valid: %v", err)
	}
	var signer Signer
	if err := tx.QueryRow(ctx, `SELECT username, display_name, email FROM users WHERE id=$1`, uid).Scan(&signer.Username, &signer.Name, &signer.Email); err != nil {
		return 0, pd, err
	}
	signer.ID = uid
	ev := Evidence{Version: 1, Purpose: pd.Purpose, Ref: pd.Ref, Signer: signer, Method: m.ID(), Level: m.Level(),
		Digest: hex.EncodeToString(pd.Digest), Record: pd.Record, Proof: proof, SignedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if err := s.seal(ctx, tx, &ev); err != nil {
		return 0, pd, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO signatures(purpose, ref, user_id, signer, method, level, digest, record, proof, key_id, seal_ed, seal_ml, seal_cms, signed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`,
		ev.Purpose, ev.Ref, uid, signer, ev.Method, ev.Level, ev.Digest, string(ev.Record), string(ev.Proof), ev.Seal.Key, ev.Seal.Ed25519, ev.Seal.MLDSA, ev.Seal.CMS, ev.SignedAt).Scan(&ev.ID)
	if err != nil {
		return 0, pd, err
	}
	return ev.ID, pd, audit.Log(ctx, tx, "signature.create", "signature", ev.ID, nil, map[string]string{"ref": ev.Ref, "method": ev.Method, "level": ev.Level})
}

// Signer is the person as it was when signing.
type Signer struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

// Seal is the hybrid signature of the wiki over the evidence.
type Seal struct {
	Key     int64  `json:"key"`
	Ed25519 []byte `json:"ed25519"`
	MLDSA   []byte `json:"ml_dsa_65"`
	// CMS is the optional additional seal with the certificate of the
	// organisation (DER, detached; content: the sealed bytes).
	CMS []byte `json:"cms,omitempty"`
}

// Evidence is a stored signature.
type Evidence struct {
	ID       int64           `json:"id"`
	Version  int             `json:"v"`
	Purpose  string          `json:"purpose"`
	Ref      string          `json:"ref"`
	Signer   Signer          `json:"signer"`
	Method   string          `json:"method"`
	Level    string          `json:"level"`
	Digest   string          `json:"digest"` // hex SHA-256 of the record
	Record   json.RawMessage `json:"record"`
	Proof    json.RawMessage `json:"proof"`
	SignedAt time.Time       `json:"signed_at"`
	Seal     Seal            `json:"seal"`
}

// sealed is what the seal covers: everything but id and the seal.
func (e *Evidence) sealed() ([]byte, error) {
	b, _, err := Canonical(map[string]any{"v": e.Version, "purpose": e.Purpose, "ref": e.Ref, "signer": e.Signer, "method": e.Method,
		"level": e.Level, "digest": e.Digest, "record": e.Record, "proof": e.Proof, "signed_at": e.SignedAt.UTC().Format(time.RFC3339Nano), "key": e.Seal.Key})
	return b, err
}

// ---------------------------------------------------------------- keys

type keyPair struct {
	id  int64
	ed  ed25519.PrivateKey
	ml  *mldsa.PrivateKey
	pub PublicKey
}

// PublicKey is a seal key of the wiki.
type PublicKey struct {
	ID          int64     `json:"id"`
	Ed25519     []byte    `json:"ed25519"`
	MLDSA       []byte    `json:"ml_dsa_65"`
	CreatedAt   time.Time `json:"created_at"`
	Fingerprint string    `json:"fingerprint"` // SHA-256 of both public keys
}

func fingerprint(ed, ml []byte) string {
	d := sha256.Sum256(append(append([]byte{}, ed...), ml...))
	return hex.EncodeToString(d[:16])
}

func init() {
	secrets.Register(secrets.Column{Table: "signature_keys", Key: "id::text", Value: "private"})
}

// current returns the active seal key, creating one the first time.
func (s *Service) current(ctx context.Context, tx pgx.Tx) (*keyPair, error) {
	s.mu.RLock()
	k := s.keys
	s.mu.RUnlock()
	if k != nil {
		return k, nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('signature_keys'))`); err != nil {
		return nil, err
	}
	var id int64
	var priv []byte
	var created time.Time
	err := tx.QueryRow(ctx, `SELECT id, private, created_at FROM signature_keys WHERE retired_at IS NULL ORDER BY id DESC LIMIT 1`).Scan(&id, &priv, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		seed := make([]byte, ed25519.SeedSize+mldsa.PrivateKeySize)
		if _, err := rand.Read(seed); err != nil {
			return nil, err
		}
		kp, err := pair(seed)
		if err != nil {
			return nil, err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO signature_keys(public_ed, public_ml, private) VALUES ($1,$2,'') RETURNING id, created_at`,
			kp.pub.Ed25519, kp.pub.MLDSA).Scan(&id, &created); err != nil {
			return nil, err
		}
		box := s.Box
		if box == nil {
			box = secrets.Plain()
		}
		sealed, err := box.Seal(seed, secrets.AAD("signature_keys", fmt.Sprint(id)))
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE signature_keys SET private=$2 WHERE id=$1`, id, sealed); err != nil {
			return nil, err
		}
		priv = sealed
	} else if err != nil {
		return nil, err
	}
	box := s.Box
	if box == nil {
		box = secrets.Plain()
	}
	seed, err := box.Open(priv, secrets.AAD("signature_keys", fmt.Sprint(id)))
	if err != nil {
		return nil, fmt.Errorf("signature key: %w", err)
	}
	k, err = pair(seed)
	if err != nil {
		return nil, err
	}
	k.id, k.pub.ID, k.pub.CreatedAt = id, id, created
	s.mu.Lock()
	s.keys = k
	s.mu.Unlock()
	return k, nil
}

func pair(seed []byte) (*keyPair, error) {
	if len(seed) != ed25519.SeedSize+mldsa.PrivateKeySize {
		return nil, errors.New("signature key: wrong size")
	}
	ed := ed25519.NewKeyFromSeed(seed[:ed25519.SeedSize])
	ml, err := mldsa.NewPrivateKey(mldsa.MLDSA65(), seed[ed25519.SeedSize:])
	if err != nil {
		return nil, err
	}
	edPub := ed.Public().(ed25519.PublicKey)
	mlPub := ml.PublicKey().Bytes()
	return &keyPair{ed: ed, ml: ml, pub: PublicKey{Ed25519: edPub, MLDSA: mlPub, Fingerprint: fingerprint(edPub, mlPub)}}, nil
}

func (s *Service) seal(ctx context.Context, tx pgx.Tx, e *Evidence) error {
	k, err := s.current(ctx, tx)
	if err != nil {
		return err
	}
	e.Seal.Key = k.id
	msg, err := e.sealed()
	if err != nil {
		return err
	}
	e.Seal.Ed25519 = ed25519.Sign(k.ed, msg)
	if e.Seal.MLDSA, err = k.ml.Sign(rand.Reader, msg, &mldsa.Options{Context: sealContext}); err != nil {
		return err
	}
	s.mu.RLock()
	cms := s.cms
	s.mu.RUnlock()
	if cms != nil {
		if e.Seal.CMS, err = cms(ctx, tx, msg); err != nil {
			return fmt.Errorf("seal with the certificate of the organisation: %w", err)
		}
	}
	return nil
}

// Keys lists the public seal keys of the wiki (also retired ones).
func (s *Service) Keys(ctx context.Context) ([]PublicKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, public_ed, public_ml, created_at FROM signature_keys ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PublicKey, error) {
		var k PublicKey
		err := r.Scan(&k.ID, &k.Ed25519, &k.MLDSA, &k.CreatedAt)
		k.Fingerprint = fingerprint(k.Ed25519, k.MLDSA)
		return k, err
	})
}

// ---------------------------------------------------------------- reading and verifying

// Get returns a signature if the caller may see it: the signer, the
// administrators and whoever the viewer of its purpose allows.
func (s *Service) Get(ctx context.Context, id int64) (*Evidence, error) {
	var e Evidence
	var uid *int64
	err := s.pool.QueryRow(ctx, `SELECT id, purpose, ref, user_id, signer, method, level, digest, record, proof, key_id, seal_ed, seal_ml, seal_cms, signed_at
		FROM signatures WHERE id=$1`, id).Scan(&e.ID, &e.Purpose, &e.Ref, &uid, &e.Signer, &e.Method, &e.Level, &e.Digest, &e.Record, &e.Proof,
		&e.Seal.Key, &e.Seal.Ed25519, &e.Seal.MLDSA, &e.Seal.CMS, &e.SignedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("signature")
	}
	if err != nil {
		return nil, err
	}
	e.Version = 1
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return nil, apperr.NotFound("signature")
	}
	if !(p.Admin() || (uid != nil && *uid == p.UserID)) {
		s.mu.RLock()
		v := s.viewers[e.Purpose]
		s.mu.RUnlock()
		if v == nil || !v(ctx, e.Ref) {
			return nil, apperr.NotFound("signature")
		}
	}
	return &e, nil
}

// Export is a signature with the public key of its seal: all that is
// needed to verify it elsewhere.
type Export struct {
	Evidence
	Key PublicKey `json:"public_key"`
	// Sealed are the exact bytes both seals cover (also the content of the
	// CMS seal, e.g. for "openssl cms -verify -content"). Only informative:
	// the checks compute them from the evidence.
	Sealed []byte `json:"sealed,omitempty"`
}

// ExportOf completes an evidence with its public key.
func (s *Service) ExportOf(ctx context.Context, e *Evidence) (*Export, error) {
	keys, err := s.Keys(ctx)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		if k.ID == e.Seal.Key {
			msg, err := e.sealed()
			if err != nil {
				return nil, err
			}
			return &Export{Evidence: *e, Key: k, Sealed: msg}, nil
		}
	}
	return nil, errors.New("seal key missing")
}

// Verify checks an exported signature: the record against its digest, the
// proof of the method and the seal of the wiki. known are the keys of this
// wiki (nil: only check against the key in the export).
func Verify(x *Export, known []PublicKey) []Check {
	var out []Check
	_, digest, err := Canonical(json.RawMessage(x.Record))
	ok := err == nil && hex.EncodeToString(digest) == strings.ToLower(x.Digest)
	out = append(out, Check{Name: "record", OK: ok, Detail: "SHA-256 " + x.Digest})
	raw, _ := hex.DecodeString(x.Digest)
	if v := verifier(x.Method); v != nil {
		out = append(out, v(raw, x.Proof)...)
	} else {
		out = append(out, Check{Name: "method", OK: false, Detail: "no check for " + x.Method})
	}
	msg, err := x.Evidence.sealed()
	edOK := err == nil && len(x.Key.Ed25519) == ed25519.PublicKeySize && ed25519.Verify(x.Key.Ed25519, msg, x.Seal.Ed25519)
	mlOK := false
	if pk, perr := mldsa.NewPublicKey(mldsa.MLDSA65(), x.Key.MLDSA); perr == nil && err == nil {
		mlOK = mldsa.Verify(pk, msg, x.Seal.MLDSA, &mldsa.Options{Context: sealContext}) == nil
	}
	out = append(out, Check{Name: "seal.ed25519", OK: edOK}, Check{Name: "seal.ml_dsa_65", OK: mlOK})
	if len(x.Seal.CMS) > 0 {
		cmsMu.RLock()
		v := cmsVerifier
		cmsMu.RUnlock()
		switch {
		case err != nil:
			out = append(out, Check{Name: "seal.cms", OK: false, Detail: err.Error()})
		case v == nil:
			out = append(out, Check{Name: "seal.cms", OK: false, Detail: "no check for CMS seals"})
		default:
			out = append(out, v(msg, x.Seal.CMS))
		}
	}
	if known != nil {
		match := false
		for _, k := range known {
			if k.ID == x.Seal.Key && fingerprint(k.Ed25519, k.MLDSA) == fingerprint(x.Key.Ed25519, x.Key.MLDSA) {
				match = true
			}
		}
		out = append(out, Check{Name: "seal.key_of_this_wiki", OK: match, Detail: x.Key.Fingerprint})
	}
	return out
}

// Valid reports whether all checks passed.
func Valid(cs []Check) bool {
	for _, c := range cs {
		if !c.OK {
			return false
		}
	}
	return len(cs) > 0
}

// ForRef lists the ids of the signatures of a reference.
func ForRef(ctx context.Context, q db.Querier, ref string) ([]int64, error) {
	rows, err := q.Query(ctx, `SELECT id FROM signatures WHERE ref=$1 ORDER BY id`, ref)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// DeleteRef removes the signatures of a reference (retention of cases).
func DeleteRef(ctx context.Context, q db.Querier, ref string) error {
	_, err := q.Exec(ctx, `DELETE FROM signatures WHERE ref=$1`, ref)
	return err
}

// ErrStatus is the HTTP status of a missing signature.
var ErrStatus = http.StatusPreconditionRequired
