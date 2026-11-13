package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Multi-factor sign-in as a chain of factors. Every factor is an MFA
// plugin with a factor number (plugin setting "factor", default from the
// plugin): factors with the same number are alternatives, different
// numbers are asked one after the other in ascending order. Factor 1 is
// the sign-in itself: the password, single sign-on, or a password-less
// method (a passkey) as an alternative to them.
//
// Each sign-in provider instance (local accounts too) chooses the factors
// that apply to its people ("mfa_methods", default all) and how many
// factors a sign-in needs at least ("mfa_required", default 1). A person
// is asked for every further factor they have set up; if they have fewer
// than required, they set one up during the sign-in.
//
// Until the chain is complete there is no session, only a pending sign-in
// (token in the answer or, after single sign-on, in a short-lived HttpOnly
// cookie): 10 minutes, 5 wrong answers, used once.

// MFAMethod is a factor.
type MFAMethod interface {
	// ID is the short name ("totp", "email", "webauthn", "network").
	ID() string
	// DefaultFactor is the factor number if the administrators set none.
	DefaultFactor() int
	// Enrolled reports whether user uid has set this factor up.
	Enrolled(ctx context.Context, uid int64) bool
	// Challenge prepares an answer: it may send a code or return options
	// for the browser (public) and keep state for Verify.
	Challenge(ctx context.Context, uid int64) (public any, state string, err error)
	// Verify checks the answer (JSON from the browser) with the state of
	// the last Challenge ("" without one).
	Verify(ctx context.Context, uid int64, state string, answer json.RawMessage) error
}

// MFAEnroller is implemented by factors a person sets up in the browser:
// in the profile, and during a sign-in that needs more factors.
type MFAEnroller interface {
	BeginEnroll(ctx context.Context, uid int64, options json.RawMessage) (public any, state string, err error)
	FinishEnroll(ctx context.Context, uid int64, state string, answer json.RawMessage) error
}

// MFASilent is implemented by factors decided without the person (e.g.
// the network of the request): Passes is asked instead of Challenge/Verify.
type MFASilent interface {
	Passes(ctx context.Context, uid int64, ip string) bool
}

// MFAResetter is implemented by factors that administrators can remove
// (reset of all factors of a user).
type MFAResetter interface {
	ResetMFA(ctx context.Context, uid int64) error
}

// MFABulk is implemented by factors that can tell for many users at once
// who has set them up (user lists).
type MFABulk interface {
	EnrolledAmong(ctx context.Context, ids []int64) map[int64]bool
}

// PasswordlessMethod is implemented by factors that also sign in without a
// password (passkeys) when they are factor 1: Begin returns options for
// the browser and state; Finish checks the answer and returns the user.
// The method must verify the user itself (user verification).
type PasswordlessMethod interface {
	MFAMethod
	Begin(ctx context.Context) (public any, state string, err error)
	Finish(ctx context.Context, state string, answer json.RawMessage) (uid int64, err error)
}

// KeyProof is what a security key or passkey returned when it signed a
// given challenge: enough to verify the signature later without the wiki.
type KeyProof struct {
	CredentialID      []byte   `json:"credential_id"`
	PublicKey         []byte   `json:"public_key"` // COSE key
	AuthenticatorData []byte   `json:"authenticator_data"`
	ClientDataJSON    []byte   `json:"client_data_json"`
	Signature         []byte   `json:"signature"`
	RPID              string   `json:"rp_id"`
	Origins           []string `json:"origins"`
	UserVerified      bool     `json:"user_verified"`
}

// KeySigner is implemented by factors with keys that can sign a challenge
// given by the wiki (WebAuthn): signature plugins use it to sign records.
// User verification (PIN, biometrics) is always required.
type KeySigner interface {
	HasKey(ctx context.Context, uid int64) bool
	SignBegin(ctx context.Context, uid int64, challenge []byte) (public any, state string, err error)
	SignFinish(ctx context.Context, uid int64, state string, answer json.RawMessage) (*KeyProof, error)
}

// KeySigner returns an enabled factor that can sign with keys, if any.
func (s *Service) KeySigner() KeySigner {
	for _, m := range s.MFAMethods() {
		if k, ok := m.(KeySigner); ok {
			return k
		}
	}
	return nil
}

// AccessGuard is implemented by plugins that restrict sign-ins and
// requests (e.g. to networks): CheckSignIn before a session is created,
// CheckRequest for every authenticated request.
type AccessGuard interface {
	CheckSignIn(ctx context.Context, uid int64, ip string) error
	CheckRequest(ctx context.Context, p *identity.Principal) error
}

// ErrNetworkDenied is returned when an account is used from an address
// outside its allowed networks.
var ErrNetworkDenied = &apperr.Error{Status: 403, Code: "network_denied", Msg: "this account cannot be used from your network"}

// ErrMFAInvalid is returned for a wrong answer of a factor.
var ErrMFAInvalid = &apperr.Error{Status: 401, Code: "mfa_invalid", Msg: "the answer of the factor is not valid"}

// ErrMFAExpired is returned for an unknown, used or expired pending sign-in.
var ErrMFAExpired = &apperr.Error{Status: 401, Code: "mfa_expired", Msg: "the sign-in has expired: please sign in again"}

// errMFARequired answers a sign-in whose chain is not complete yet; Detail
// is the token of the pending sign-in (next step: MFAPendingInfo).
func errMFARequired(token string) error {
	return &apperr.Error{Status: 401, Code: "mfa_required", Msg: "a further factor is required", Detail: token}
}

type registeredMFA struct {
	pluginID string
	m        MFAMethod
}

type registeredGuard struct {
	pluginID string
	g        AccessGuard
}

// RegisterMFAMethod adds a factor owned by pluginID.
func (s *Service) RegisterMFAMethod(pluginID string, m MFAMethod) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mfa = append(s.mfa, registeredMFA{pluginID, m})
}

// RegisterAccessGuard adds a guard owned by pluginID.
func (s *Service) RegisterAccessGuard(pluginID string, g AccessGuard) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.guards = append(s.guards, registeredGuard{pluginID, g})
}

func (s *Service) activeGuards() []AccessGuard {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []AccessGuard
	for _, r := range s.guards {
		if s.enabled(r.pluginID) {
			out = append(out, r.g)
		}
	}
	return out
}

// checkSignIn asks the guards before a session is created for uid.
func (s *Service) checkSignIn(ctx context.Context, uid int64, ip string) error {
	for _, g := range s.activeGuards() {
		if err := g.CheckSignIn(ctx, uid, ip); err != nil {
			if errors.Is(err, ErrNetworkDenied) {
				s.log.Warn("sign-in refused by a guard", "user", uid, "ip", ip)
				_ = audit.Log(identity.With(ctx, &identity.Principal{UserID: uid}), s.pool, "auth.network_denied", "user", uid, nil, map[string]string{"ip": ip})
			}
			return err
		}
	}
	return nil
}

// checkRequest asks the guards for an authenticated request.
func (s *Service) checkRequest(ctx context.Context, p *identity.Principal) error {
	if p == nil {
		return nil
	}
	for _, g := range s.activeGuards() {
		if err := g.CheckRequest(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

// MFAFactor is a factor with its configuration.
type MFAFactor struct {
	ID            string `json:"id"`
	Plugin        string `json:"plugin"`
	Factor        int    `json:"factor"`
	DefaultFactor int    `json:"default_factor"`
	Enabled       bool   `json:"enabled"`
	Passwordless  bool   `json:"passwordless"` // may be factor 1
	Enroll        bool   `json:"enroll"`       // set up in the browser
	Silent        bool   `json:"silent"`       // decided without the person
	// CountsTwo: passing it also covers the next factor of the chain
	// (only password-less methods, which check the person themselves).
	CountsTwo bool `json:"counts_two"`
	m         MFAMethod
}

// factorOf returns the configured factor number of a method.
func (s *Service) factorOf(r registeredMFA) int {
	f := r.m.DefaultFactor()
	if v, ok := s.cfg.Get().Settings[r.pluginID]["factor"]; ok {
		if n, err := strconv.Atoi(fmt.Sprint(v)); err == nil && n >= 1 && n <= 9 {
			f = n
		}
	}
	if _, ok := r.m.(PasswordlessMethod); !ok && f < 2 {
		f = 2 // only password-less methods replace the sign-in
	}
	return f
}

// countsTwo reports whether a method counts as two factors (plugin
// setting "counts_two"; only password-less methods with user verification).
func (s *Service) countsTwo(r registeredMFA) bool {
	if _, ok := r.m.(PasswordlessMethod); !ok {
		return false
	}
	v, ok := s.cfg.Get().Settings[r.pluginID]["counts_two"].(bool)
	return ok && v
}

// MFAFactors lists all factors (also of disabled plugins), by factor.
func (s *Service) MFAFactors() []MFAFactor {
	s.mu.RLock()
	list := slices.Clone(s.mfa)
	s.mu.RUnlock()
	out := make([]MFAFactor, 0, len(list))
	for _, r := range list {
		_, pl := r.m.(PasswordlessMethod)
		_, en := r.m.(MFAEnroller)
		_, si := r.m.(MFASilent)
		out = append(out, MFAFactor{ID: r.m.ID(), Plugin: r.pluginID, Factor: s.factorOf(r), DefaultFactor: r.m.DefaultFactor(),
			Enabled: s.enabled(r.pluginID), Passwordless: pl, Enroll: en, Silent: si, CountsTwo: s.countsTwo(r), m: r.m})
	}
	slices.SortStableFunc(out, func(a, b MFAFactor) int { return a.Factor - b.Factor })
	return out
}

// MFAMethods returns the active factors.
func (s *Service) MFAMethods() []MFAMethod {
	var out []MFAMethod
	for _, f := range s.MFAFactors() {
		if f.Enabled {
			out = append(out, f.m)
		}
	}
	return out
}

func (s *Service) mfaFactor(id string) (MFAFactor, bool) {
	for _, f := range s.MFAFactors() {
		if f.Enabled && f.ID == id {
			return f, true
		}
	}
	return MFAFactor{}, false
}

// providerSettings returns the settings of a sign-in provider instance.
func (s *Service) providerSettings(provider string) map[string]any {
	for _, p := range s.Providers() {
		if sp, ok := p.(SettingsProvider); ok && p.ID() == provider {
			return sp.Settings()
		}
	}
	for _, p := range s.RedirectProviders() {
		if sp, ok := p.(SettingsProvider); ok && p.ID() == provider {
			return sp.Settings()
		}
	}
	return nil
}

// chainFor returns the active factors of a provider instance and the
// number of factors its sign-ins need.
func (s *Service) chainFor(provider string) ([]MFAFactor, int) {
	set := s.providerSettings(provider)
	var chosen []string
	if v, ok := set["mfa_methods"]; ok && v != nil {
		chosen = StringList(v)
		if chosen == nil {
			chosen = []string{}
		}
	}
	var out []MFAFactor
	for _, f := range s.MFAFactors() {
		if f.Enabled && (chosen == nil || slices.Contains(chosen, f.ID)) {
			out = append(out, f)
		}
	}
	required := 1
	if n, err := strconv.Atoi(fmt.Sprint(set["mfa_required"])); err == nil && n >= 1 && n <= 9 {
		required = n
	}
	return out, required
}

// EnrolledMFA lists the factors user uid has set up.
func (s *Service) EnrolledMFA(ctx context.Context, uid int64) []string {
	out := []string{}
	for _, m := range s.MFAMethods() {
		if m.Enrolled(ctx, uid) {
			out = append(out, m.ID())
		}
	}
	return out
}

// mfaAmong reports for each of ids whether the user has any factor.
func (s *Service) mfaAmong(ctx context.Context, ids []int64) map[int64]bool {
	out := map[int64]bool{}
	for _, m := range s.MFAMethods() {
		if b, ok := m.(MFABulk); ok {
			for id, on := range b.EnrolledAmong(ctx, ids) {
				out[id] = out[id] || on
			}
			continue
		}
		for _, id := range ids {
			if !out[id] && m.Enrolled(ctx, id) {
				out[id] = true
			}
		}
	}
	return out
}

// ---------------------------------------------------------------- pending sign-ins

const (
	mfaPendingTTL      = 10 * time.Minute
	mfaPendingAttempts = 5
)

type pendingSSO struct {
	Provider  string `json:"provider"`
	Subject   string `json:"subject"`
	SessionID string `json:"session_id"`
	Hint      string `json:"hint"`
}

type pending struct {
	token  string
	uid    int64
	chain  string // provider instance whose factors apply
	sso    pendingSSO
	next   string
	data   map[string]string
	passed int // factors passed (factor 1 included)
	last   int // number of the last factor passed
	cover  int // factor numbers up to this one are covered (a factor counting two)
}

func (s *Service) loadPending(ctx context.Context, token string) (*pending, error) {
	if token == "" || len(token) > 100 {
		return nil, ErrMFAExpired
	}
	p := &pending{token: token}
	err := s.pool.QueryRow(ctx, `SELECT user_id, chain, sso, next, data, passed, last_factor, covered FROM mfa_pending
		WHERE token_hash=$1 AND expires_at > now()`, HashToken(token)).Scan(&p.uid, &p.chain, &p.sso, &p.next, &p.data, &p.passed, &p.last, &p.cover)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMFAExpired
	}
	if p.data == nil {
		p.data = map[string]string{}
	}
	return p, err
}

func (s *Service) savePending(ctx context.Context, p *pending) error {
	_, err := s.pool.Exec(ctx, `UPDATE mfa_pending SET passed=$2, last_factor=$3, data=$4, covered=$5 WHERE token_hash=$1`, HashToken(p.token), p.passed, p.last, p.data, p.cover)
	return err
}

// MFAStep is the next step of a pending sign-in.
type MFAStep struct {
	Kind     string   `json:"kind"` // verify | enroll | done
	Factor   int      `json:"factor"`
	Methods  []string `json:"methods"`
	Passed   int      `json:"passed"`
	Required int      `json:"required"`
}

// nextStep decides what a pending sign-in needs next; factors the person
// passes without doing anything (network) are passed on the way.
func (s *Service) nextStep(ctx context.Context, p *pending, ip string) (*MFAStep, error) {
	chain, required := s.chainFor(p.chain)
	numbers := []int{}
	for _, f := range chain {
		if f.Factor > p.last && !slices.Contains(numbers, f.Factor) {
			numbers = append(numbers, f.Factor)
		}
	}
	slices.Sort(numbers)
	for _, n := range numbers {
		var interactive, enrollable []string
		silentPass, silentFail := false, false
		for _, f := range chain {
			if f.Factor != n {
				continue
			}
			switch sm, silent := f.m.(MFASilent); {
			case f.m.Enrolled(ctx, p.uid) && silent:
				if sm.Passes(ctx, p.uid, ip) {
					silentPass = true
				} else {
					silentFail = true
				}
			case f.m.Enrolled(ctx, p.uid):
				interactive = append(interactive, f.ID)
			case f.Enroll && !silent:
				enrollable = append(enrollable, f.ID)
			}
		}
		if n <= p.cover {
			// covered by a factor counting two: only a network that
			// cannot be replaced by anything still decides
			if silentFail && !silentPass && len(interactive) == 0 {
				_ = audit.Log(identity.With(ctx, &identity.Principal{UserID: p.uid}), s.pool, "auth.network_denied", "user", p.uid, nil, map[string]string{"ip": ip})
				return nil, ErrNetworkDenied
			}
			continue
		}
		switch {
		case silentPass:
			p.passed, p.last = p.passed+1, n
			if err := s.savePending(ctx, p); err != nil {
				return nil, err
			}
			continue
		case len(interactive) > 0:
			return &MFAStep{Kind: "verify", Factor: n, Methods: interactive, Passed: p.passed, Required: required}, nil
		case silentFail:
			_ = audit.Log(identity.With(ctx, &identity.Principal{UserID: p.uid}), s.pool, "auth.network_denied", "user", p.uid, nil, map[string]string{"ip": ip})
			return nil, ErrNetworkDenied
		case p.passed < required && len(enrollable) > 0:
			return &MFAStep{Kind: "enroll", Factor: n, Methods: enrollable, Passed: p.passed, Required: required}, nil
		}
	}
	if p.passed < required {
		s.log.Warn("sign-in needs more factors than can be set up", "provider", p.chain, "required", required, "user", p.uid)
		return nil, apperr.Forbidden("this sign-in needs %d factors, but not enough factors are available: please contact the administrators", required)
	}
	return &MFAStep{Kind: "done", Passed: p.passed, Required: required}, nil
}

// SignInResult is a sign-in after factor 1: a session, or a pending
// sign-in (MFAToken) that needs further factors.
type SignInResult struct {
	*LoginResult
	MFAToken string
}

// afterFirstFactor continues a sign-in whose factor 1 was passed: further
// factors of the chain of provider (chain), or the session.
func (s *Service) afterFirstFactor(ctx context.Context, uid int64, chain string, ext *ExternalIdentity, next, ip, ua string, two bool) (*SignInResult, error) {
	sso := pendingSSO{}
	if ext != nil {
		sso = pendingSSO{ext.Provider, ext.Subject, ext.SessionID, ext.LogoutHint}
		if len(sso.Hint) > 16<<10 {
			sso.Hint = ""
		}
	}
	p := &pending{token: NewToken(""), uid: uid, chain: chain, sso: sso, next: SafeNext(next), data: map[string]string{}, passed: 1, last: 1}
	if two {
		p.passed, p.cover = 2, s.nextFactorNumber(chain, 1)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO mfa_pending(token_hash, user_id, chain, sso, next, passed, last_factor, covered, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,1,$7,$8)`, HashToken(p.token), uid, chain, sso, p.next, p.passed, p.cover, time.Now().Add(mfaPendingTTL)); err != nil {
		return nil, err
	}
	step, err := s.nextStep(ctx, p, ip)
	if err != nil {
		_, _ = s.pool.Exec(ctx, `DELETE FROM mfa_pending WHERE token_hash=$1`, HashToken(p.token))
		return nil, err
	}
	if step.Kind != "done" {
		return &SignInResult{MFAToken: p.token}, nil
	}
	res, err := s.completePending(ctx, p, ip, ua)
	if err != nil {
		return nil, err
	}
	return &SignInResult{LoginResult: res}, nil
}

// completePending turns a complete pending sign-in into a session (once).
func (s *Service) completePending(ctx context.Context, p *pending, ip, ua string) (*LoginResult, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM mfa_pending WHERE token_hash=$1`, HashToken(p.token))
	if err != nil || tag.RowsAffected() != 1 {
		return nil, ErrMFAExpired
	}
	if err := s.checkSignIn(ctx, p.uid, ip); err != nil {
		return nil, err
	}
	ext := &ExternalIdentity{Provider: p.sso.Provider, Subject: p.sso.Subject, SessionID: p.sso.SessionID, LogoutHint: p.sso.Hint}
	return s.newSession(ctx, p.uid, ext, ip, ua)
}

// MFAPendingInfo returns the next step of a pending sign-in.
func (s *Service) MFAPendingInfo(ctx context.Context, token, ip string) (*MFAStep, error) {
	p, err := s.loadPending(ctx, token)
	if err != nil {
		return nil, err
	}
	return s.nextStep(ctx, p, ip)
}

// stepMethod checks that method belongs to the next step of kind.
func (s *Service) stepMethod(ctx context.Context, p *pending, method, kind, ip string) (MFAFactor, error) {
	step, err := s.nextStep(ctx, p, ip)
	if err != nil {
		return MFAFactor{}, err
	}
	f, ok := s.mfaFactor(method)
	if step.Kind != kind || !ok || !slices.Contains(step.Methods, method) {
		return MFAFactor{}, apperr.BadRequest("this factor is not asked now")
	}
	return f, nil
}

// MFAChallenge prepares a factor of the next step (sends the e-mail code,
// returns the WebAuthn options).
func (s *Service) MFAChallenge(ctx context.Context, token, method, ip string) (any, error) {
	p, err := s.loadPending(ctx, token)
	if err != nil {
		return nil, err
	}
	f, err := s.stepMethod(ctx, p, method, "verify", ip)
	if err != nil {
		return nil, err
	}
	pub, state, err := f.m.Challenge(ctx, p.uid)
	if err != nil {
		return nil, err
	}
	p.data[method] = state
	return pub, s.savePending(ctx, p)
}

// MFAResult is the answer to a step: the session (done) or the next step.
type MFAResult struct {
	*LoginResult
	Next string
	Step *MFAStep
}

// countAttempt counts an answer before it is checked (concurrent guesses
// count too); after 5 the pending sign-in ends.
func (s *Service) countAttempt(ctx context.Context, token string) error {
	var attempts int
	if err := s.pool.QueryRow(ctx, `UPDATE mfa_pending SET attempts = attempts + 1 WHERE token_hash=$1 RETURNING attempts`,
		HashToken(token)).Scan(&attempts); err != nil {
		return ErrMFAExpired
	}
	if attempts > mfaPendingAttempts {
		_, _ = s.pool.Exec(ctx, `DELETE FROM mfa_pending WHERE token_hash=$1`, HashToken(token))
		return ErrMFAExpired
	}
	return nil
}

// nextFactorNumber is the factor number following n in the chain (0: none).
func (s *Service) nextFactorNumber(provider string, n int) int {
	chain, _ := s.chainFor(provider)
	next := 0
	for _, f := range chain {
		if f.Factor > n && (next == 0 || f.Factor < next) {
			next = f.Factor
		}
	}
	return next
}

// advance records a passed factor and returns the next step or the session.
func (s *Service) advance(ctx context.Context, p *pending, f MFAFactor, ip, ua string) (*MFAResult, error) {
	factor := f.Factor
	p.passed, p.last = p.passed+1, factor
	if f.CountsTwo {
		p.passed++
		p.cover = max(p.cover, s.nextFactorNumber(p.chain, factor))
	}
	if err := s.savePending(ctx, p); err != nil {
		return nil, err
	}
	step, err := s.nextStep(ctx, p, ip)
	if err != nil {
		return nil, err
	}
	if step.Kind != "done" {
		return &MFAResult{Step: step}, nil
	}
	res, err := s.completePending(ctx, p, ip, ua)
	if err != nil {
		return nil, err
	}
	return &MFAResult{LoginResult: res, Next: p.next}, nil
}

// CompleteMFA checks the answer to a factor of the next step.
func (s *Service) CompleteMFA(ctx context.Context, token, method string, answer json.RawMessage, ip, ua string) (*MFAResult, error) {
	p, err := s.loadPending(ctx, token)
	if err != nil {
		return nil, err
	}
	f, err := s.stepMethod(ctx, p, method, "verify", ip)
	if err != nil {
		return nil, err
	}
	if err := s.countAttempt(ctx, token); err != nil {
		return nil, err
	}
	if err := f.m.Verify(ctx, p.uid, p.data[method], answer); err != nil {
		return nil, err
	}
	delete(p.data, method)
	return s.advance(ctx, p, f, ip, ua)
}

// MFAEnrollBegin starts setting up a factor during a sign-in that needs
// more factors than the person has.
func (s *Service) MFAEnrollBegin(ctx context.Context, token, method string, options json.RawMessage, ip string) (any, error) {
	p, err := s.loadPending(ctx, token)
	if err != nil {
		return nil, err
	}
	f, err := s.stepMethod(ctx, p, method, "enroll", ip)
	if err != nil {
		return nil, err
	}
	pub, state, err := f.m.(MFAEnroller).BeginEnroll(ctx, p.uid, options)
	if err != nil {
		return nil, err
	}
	p.data["enroll:"+method] = state
	return pub, s.savePending(ctx, p)
}

// MFAEnrollFinish completes setting up a factor during a sign-in; the
// factor counts as passed (the person just proved it).
func (s *Service) MFAEnrollFinish(ctx context.Context, token, method string, answer json.RawMessage, ip, ua string) (*MFAResult, error) {
	p, err := s.loadPending(ctx, token)
	if err != nil {
		return nil, err
	}
	f, err := s.stepMethod(ctx, p, method, "enroll", ip)
	if err != nil {
		return nil, err
	}
	if err := s.countAttempt(ctx, token); err != nil {
		return nil, err
	}
	if err := f.m.(MFAEnroller).FinishEnroll(ctx, p.uid, p.data["enroll:"+method], answer); err != nil {
		return nil, err
	}
	delete(p.data, "enroll:"+method)
	_ = audit.Log(identity.With(ctx, &identity.Principal{UserID: p.uid}), s.pool, "mfa.enroll", "user", p.uid, nil, map[string]string{"method": method, "during": "sign-in"})
	return s.advance(ctx, p, f, ip, ua)
}

// ---------------------------------------------------------------- setting up factors in the profile

// RecentSignIn is how long after the sign-in factors may be changed.
const RecentSignIn = 15 * time.Minute

// RequireRecentSignIn allows changes of factors only in a browser session
// signed in during the last 15 minutes (a stolen session cannot add its
// own factor; API keys never can).
func RequireRecentSignIn(ctx context.Context) (int64, error) {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return 0, apperr.ErrUnauthorized
	}
	if p.Via != "session" {
		return 0, apperr.Forbidden("factors can only be changed in the browser")
	}
	if time.Since(p.SignedIn) > RecentSignIn {
		return 0, &apperr.Error{Status: 403, Code: "reauth_required", Msg: "please sign in again to change your factors"}
	}
	return p.UserID, nil
}

func (s *Service) myEnroller(ctx context.Context, method string) (int64, MFAEnroller, error) {
	uid, err := RequireRecentSignIn(ctx)
	if err != nil {
		return 0, nil, err
	}
	f, ok := s.mfaFactor(method)
	en, isEn := f.m.(MFAEnroller)
	if !ok || !isEn {
		return 0, nil, apperr.NotFound("factor " + method)
	}
	// only factors of the account's own sign-in
	chain, _ := s.chainFor(identity.From(ctx).Provider)
	if !slices.ContainsFunc(chain, func(x MFAFactor) bool { return x.ID == method }) {
		return 0, nil, apperr.BadRequest("this factor is not used for your sign-in")
	}
	return uid, en, nil
}

// MyEnrollBegin starts setting up a factor in the profile.
func (s *Service) MyEnrollBegin(ctx context.Context, method string, options json.RawMessage) (any, error) {
	uid, en, err := s.myEnroller(ctx, method)
	if err != nil {
		return nil, err
	}
	pub, state, err := en.BeginEnroll(ctx, uid, options)
	if err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO mfa_enroll(user_id, method, state, expires_at) VALUES ($1,$2,$3, now() + interval '10 minutes')
		ON CONFLICT (user_id, method) DO UPDATE SET state=EXCLUDED.state, expires_at=EXCLUDED.expires_at`, uid, method, state); err != nil {
		return nil, err
	}
	return pub, nil
}

// MyEnrollFinish completes setting up a factor in the profile.
func (s *Service) MyEnrollFinish(ctx context.Context, method string, answer json.RawMessage) error {
	uid, en, err := s.myEnroller(ctx, method)
	if err != nil {
		return err
	}
	var state string
	if err := s.pool.QueryRow(ctx, `DELETE FROM mfa_enroll WHERE user_id=$1 AND method=$2 AND expires_at > now() RETURNING state`, uid, method).Scan(&state); err != nil {
		return apperr.BadRequest("the setup has expired: please start again")
	}
	if err := en.FinishEnroll(ctx, uid, state, answer); err != nil {
		return err
	}
	s.bump(ctx)
	return audit.Log(ctx, s.pool, "mfa.enroll", "user", uid, nil, map[string]string{"method": method})
}

// MFAStatus describes the factors of the signed-in user.
type MFAStatus struct {
	// Applies: the wiki asks for factors at this account's sign-in.
	Applies  bool        `json:"applies"`
	Methods  []string    `json:"methods"`  // factors set up
	Chain    []MFAFactor `json:"chain"`    // factors of the account's sign-in
	Required int         `json:"required"` // factors a sign-in needs
}

// MFA returns the status of the signed-in user.
func (s *Service) MFA(ctx context.Context) (*MFAStatus, error) {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return nil, apperr.ErrUnauthorized
	}
	chain, required := s.chainFor(p.Provider)
	if chain == nil {
		chain = []MFAFactor{}
	}
	return &MFAStatus{Applies: len(chain) > 0, Methods: s.EnrolledMFA(ctx, p.UserID), Chain: chain, Required: required}, nil
}

// ---------------------------------------------------------------- sign-in without password

// PasswordlessMethods lists the password-less methods that are factor 1.
func (s *Service) PasswordlessMethods() []string {
	out := []string{}
	for _, f := range s.MFAFactors() {
		if _, ok := f.m.(PasswordlessMethod); ok && f.Enabled && f.Factor == 1 {
			out = append(out, f.ID)
		}
	}
	return out
}

func (s *Service) passwordless(id string) PasswordlessMethod {
	if !slices.Contains(s.PasswordlessMethods(), id) {
		return nil
	}
	f, _ := s.mfaFactor(id)
	return f.m.(PasswordlessMethod)
}

// PasswordlessBegin starts a password-less sign-in; the token identifies
// it at PasswordlessFinish (5 minutes, once).
func (s *Service) PasswordlessBegin(ctx context.Context, method, ip string) (string, any, error) {
	m := s.passwordless(method)
	if m == nil {
		return "", nil, apperr.NotFound("sign-in method " + method)
	}
	if !s.limiter.allow("pwless:"+ip, 60, 5*time.Minute) {
		return "", nil, apperr.TooMany("too many login attempts, try again later")
	}
	pub, state, err := m.Begin(ctx)
	if err != nil {
		return "", nil, err
	}
	tok := NewToken("")
	if _, err := s.pool.Exec(ctx, `INSERT INTO passwordless_pending(token_hash, method, state, expires_at) VALUES ($1,$2,$3, now() + interval '5 minutes')`,
		HashToken(tok), method, state); err != nil {
		return "", nil, err
	}
	return tok, pub, nil
}

// PasswordlessFinish completes factor 1 without password; the account's
// own sign-in provider must allow the method, and its further factors
// follow (mfa_required).
func (s *Service) PasswordlessFinish(ctx context.Context, token string, answer json.RawMessage, ip, ua string) (*SignInResult, error) {
	var method, state string
	err := s.pool.QueryRow(ctx, `DELETE FROM passwordless_pending WHERE token_hash=$1 AND expires_at > now() RETURNING method, state`,
		HashToken(token)).Scan(&method, &state)
	if err != nil {
		return nil, ErrMFAExpired
	}
	m := s.passwordless(method)
	if m == nil {
		return nil, ErrMFAExpired
	}
	uid, err := m.Finish(ctx, state, answer)
	if err != nil {
		s.limiter.allow("pwless:"+ip, 60, 5*time.Minute)
		return nil, apperr.ErrInvalidLogin
	}
	var provider string
	var disabled bool
	if err := s.pool.QueryRow(ctx, `SELECT provider, disabled FROM users WHERE id=$1`, uid).Scan(&provider, &disabled); err != nil || disabled {
		return nil, apperr.ErrInvalidLogin
	}
	chain, _ := s.chainFor(provider)
	i := slices.IndexFunc(chain, func(f MFAFactor) bool { return f.ID == method && f.Factor == 1 })
	if i < 0 {
		return nil, apperr.Forbidden("this account cannot sign in with %s", method)
	}
	_, _ = s.pool.Exec(ctx, `UPDATE users SET last_login_at=now() WHERE id=$1`, uid)
	return s.afterFirstFactor(ctx, uid, provider, &ExternalIdentity{Provider: method}, "", ip, ua, chain[i].CountsTwo)
}

// newSession creates a browser session for uid after a complete sign-in.
func (s *Service) newSession(ctx context.Context, uid int64, ext *ExternalIdentity, ip, ua string) (*LoginResult, error) {
	tok, exp, err := s.createSession(ctx, uid, "web", config.Duration(s.cfg.Get().Auth.SessionTTL, 7*24*time.Hour), ip, ua, ext)
	if err != nil {
		return nil, err
	}
	pr, err := s.LoadPrincipal(ctx, uid)
	if err != nil {
		return nil, err
	}
	pr.Via, pr.SignedIn = "session", time.Now()
	return &LoginResult{tok, exp, pr}, nil
}

// MaskEmail shows enough of an address to recognise it (a***@example.org).
func MaskEmail(a string) string {
	at := -1
	for i := range a {
		if a[i] == '@' {
			at = i
		}
	}
	if at < 1 {
		return ""
	}
	return a[:1] + "***" + a[at:]
}

// RequireUserAdmin allows changes of user uid by administrators of users
// (only administrators change administrators).
func (s *Service) RequireUserAdmin(ctx context.Context, uid int64) error {
	if err := s.requireUserAdmin(ctx); err != nil {
		return err
	}
	return guardTarget(ctx, s.pool, uid)
}
