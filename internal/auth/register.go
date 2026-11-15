package auth

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/limits"
)

// Limits of a registration.
const (
	registerNameMax    = 100
	registerPerAddress = 3 // links to one address per hour
)

// Registration is what a person enters to get a local account.
type Registration struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	Language    string `json:"language"`
}

// RegistrationOpen reports whether people may create local accounts here:
// switched on, e-mail can be sent, and the accounts of this site are its
// own (on a further site with shared accounts only the main site registers).
func (s *Service) RegistrationOpen(ctx context.Context) bool {
	c := s.cfg.Get().Login.Registration
	if !c.Enabled || s.SendRegistration == nil || (s.MailReady != nil && !s.MailReady()) {
		return false
	}
	return s.SeparateAccounts || identity.Site(ctx) == "main"
}

// RegistrationAllowedHere reports whether registration may be switched on
// for the accounts of this site at all.
func (s *Service) RegistrationAllowedHere(ctx context.Context) bool {
	return s.SeparateAccounts || identity.Site(ctx) == "main"
}

// DomainAllowed reports whether addr matches the allowed domains (none: all).
func DomainAllowed(c config.Registration, addr string) bool {
	if len(c.Domains) == 0 {
		return true
	}
	at := strings.LastIndexByte(addr, '@')
	if at < 0 {
		return false
	}
	dom := addr[at+1:]
	for _, d := range c.Domains {
		if sub, ok := strings.CutPrefix(d, "*."); ok {
			if strings.HasSuffix(dom, "."+sub) {
				return true
			}
		} else if dom == d {
			return true
		}
	}
	return false
}

// Register starts a registration: it checks the input and sends the
// confirmation link. The answer never tells whether the address already has
// an account (the owner of an existing address gets a mail saying so instead
// of a link), so the form cannot be used to find out who has an account.
func (s *Service) Register(ctx context.Context, in Registration, ip string) error {
	if !s.RegistrationOpen(ctx) {
		return apperr.NotFound("registration")
	}
	c := s.cfg.Get()
	addr, err := NormalizeEmail(in.Email)
	if err != nil {
		return err
	}
	if addr == "" {
		return apperr.BadRequest("e-mail address is required")
	}
	if !DomainAllowed(c.Login.Registration, addr) {
		return apperr.BadRequest("addresses of this domain cannot register here")
	}
	name := strings.TrimSpace(in.DisplayName)
	if name == "" || len([]rune(name)) > registerNameMax || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return apperr.BadRequest("name: 1 to %d characters, no control characters", registerNameMax)
	}
	lang := strings.TrimSpace(in.Language)
	if len(lang) > 20 || strings.IndexFunc(lang, func(r rune) bool { return !(r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)) }) >= 0 {
		lang = ""
	}
	if err := CheckPasswordPolicy(in.Password, c.Auth.MinPasswordLen); err != nil {
		return apperr.BadRequest("%s", err.Error())
	}
	if !s.limiter.allow("register:"+ip, int(limits.Value(c, limits.RegisterPerHour)), time.Hour) ||
		!s.limiter.allow("register-addr:"+addr, registerPerAddress, time.Hour) {
		return apperr.TooMany("too many registrations, try again later")
	}
	// hashed in every case, so the answer takes as long for known addresses
	hash, err := HashPassword(in.Password)
	if err != nil {
		return err
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_emails WHERE email=$1 AND verified)`, addr).Scan(&exists); err != nil {
		return err
	}
	link := ""
	if !exists {
		var pending int64
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM registrations_pending WHERE expires_at > now()`).Scan(&pending); err != nil {
			return err
		}
		if pending >= limits.Value(c, limits.RegisterPending) {
			return apperr.TooMany("too many registrations, try again later")
		}
		tok := NewToken("")
		// only the newest link of an address is valid
		if err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `DELETE FROM registrations_pending WHERE email=$1`, addr); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO registrations_pending(token_hash, email, display_name, language, password_hash, expires_at)
				VALUES ($1,$2,$3,$4,$5,$6)`, HashToken(tok), addr, name, lang, hash, time.Now().Add(emailTokenTTL))
			return err
		}); err != nil {
			return err
		}
		link = strings.TrimRight(c.Server.BaseURL, "/") + "/register/confirm?token=" + tok
	}
	if lang == "" {
		lang = c.I18n.DefaultLanguage
	}
	return s.SendRegistration(ctx, addr, name, lang, link)
}

// ConfirmRegistration creates the account of a registration with the token
// of its confirmation link. It does not sign in: the account's sign-in rules
// (further factors) apply at the next sign-in.
func (s *Service) ConfirmRegistration(ctx context.Context, token string) (*User, error) {
	if token == "" || len(token) > 200 {
		return nil, apperr.BadRequest("invalid confirmation link")
	}
	if !s.RegistrationOpen(ctx) {
		return nil, apperr.NotFound("registration")
	}
	groups := s.cfg.Get().Login.Registration.Groups
	var id int64
	err := db.Tx(ctx, s.pool, func(tx pgx.Tx) error {
		var addr, name, lang, hash string
		var valid bool
		err := tx.QueryRow(ctx, `DELETE FROM registrations_pending WHERE token_hash=$1
			RETURNING email, display_name, language, password_hash, expires_at > now()`, HashToken(token)).Scan(&addr, &name, &lang, &hash, &valid)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.BadRequest("the confirmation link is invalid or was already used")
		}
		if err != nil {
			return err
		}
		if !valid {
			return apperr.BadRequest("the confirmation link expired: please register again")
		}
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_emails WHERE email=$1 AND verified)`, addr).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return apperr.Conflict("the address already belongs to an account: please sign in")
		}
		user, err := freeUsername(ctx, tx, addr)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO users(username, provider, subject, display_name, language, password_hash)
			VALUES ($1,'local',$1,$2,$3,$4) RETURNING id`, user, name, lang, hash).Scan(&id); err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("the user name was just taken: please use the link again")
			}
			return err
		}
		if _, err := adoptEmail(ctx, tx, id, addr, true); err != nil {
			return err
		}
		// groups of the settings that still exist and carry no administrator role
		if _, err := tx.Exec(ctx, `INSERT INTO user_groups(user_id, group_id)
			SELECT $1, g.id FROM groups g WHERE g.id = ANY($2) AND NOT EXISTS (
				SELECT 1 FROM group_roles gr JOIN roles r ON r.id=gr.role_id WHERE gr.group_id=g.id AND r.is_admin)
			ON CONFLICT DO NOTHING`, id, groups); err != nil {
			return err
		}
		return audit.Log(identity.With(ctx, &identity.Principal{UserID: id, Via: "registration"}), tx, "user.register", "user", id, nil,
			map[string]any{"username": user, "email": addr, "groups": groups})
	})
	if err != nil {
		return nil, err
	}
	s.bump(ctx)
	return s.GetUser(identity.With(ctx, identity.CLI()), id)
}

// freeUsername derives a local user name from the address (its part before
// the @), with a number if it is taken by an account or a wiki user name.
func freeUsername(ctx context.Context, tx pgx.Tx, addr string) (string, error) {
	local := addr[:strings.LastIndexByte(addr, '@')]
	var b strings.Builder
	for _, r := range local {
		if b.Len() >= 40 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	base := strings.Trim(b.String(), ".-_")
	if base == "" {
		base = "user"
	}
	for i := 1; i <= 1000; i++ {
		name := base
		if i > 1 {
			name += strconv.Itoa(i)
		}
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE (provider='local' AND lower(username)=lower($1)) OR lower(alias)=lower($1))`, name).Scan(&taken); err != nil {
			return "", err
		}
		if !taken {
			return name, nil
		}
	}
	return "", apperr.Conflict("no free user name for %s", addr)
}
