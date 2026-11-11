package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

// PreviewInput is a sample sign-in for the mapping preview: what a
// provider would deliver for a person.
type PreviewInput struct {
	Subject       string   `json:"subject"` // stable id at the provider; default: username
	Username      string   `json:"username"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	Groups        []string `json:"groups"`
}

// Preview shows what a sign-in would lead to, without changing anything.
type Preview struct {
	Provider string `json:"provider"`
	// Outcome: existing (known sign-in), link_auto (linked to the account
	// with the confirmed address), link_confirm (the person confirms with
	// the existing sign-in), create (new account, JIT), refused (no JIT),
	// disabled (the account is disabled)
	Outcome      string   `json:"outcome"`
	User         string   `json:"user,omitempty"` // handle of the existing account
	Groups       []string `json:"groups"`
	NewGroups    []string `json:"new_groups"`    // created at the sign-in
	Roles        []string `json:"roles"`         // existing roles assigned
	MissingRoles []string `json:"missing_roles"` // mapped but unknown: ignored
	JIT          bool     `json:"jit"`
	TrustEmail   bool     `json:"trust_email"`
	Verified     bool     `json:"email_verified"`
}

// PreviewSignIn applies the mapping and JIT rules of provider to a sample
// sign-in and reports the result (administrators; nothing is stored).
func (s *Service) PreviewSignIn(ctx context.Context, provider string, in PreviewInput) (*Preview, error) {
	if err := perm.Require(ctx, "core.settings.manage"); err != nil {
		return nil, err
	}
	if err := perm.Require(ctx, "core.users.manage"); err != nil {
		return nil, err
	}
	var settings map[string]any
	for _, p := range s.Providers() {
		if sp, ok := p.(SettingsProvider); ok && p.ID() == provider {
			settings = sp.Settings()
		}
	}
	for _, p := range s.RedirectProviders() {
		if sp, ok := p.(SettingsProvider); ok && p.ID() == provider {
			settings = sp.Settings()
		}
	}
	if settings == nil {
		return nil, apperr.NotFound("provider")
	}
	in.Username = strings.TrimSpace(in.Username)
	if in.Username == "" {
		return nil, apperr.BadRequest("username is required")
	}
	if len(in.Groups) > 500 {
		return nil, apperr.BadRequest("too many groups")
	}
	if in.Subject == "" {
		in.Subject = in.Username
	}
	ext := &ExternalIdentity{Provider: provider, Subject: in.Subject, Username: in.Username, Email: in.Email,
		EmailVerified: in.EmailVerified, Groups: in.Groups}
	MappingFromSettings(settings).Apply(ext)
	out := &Preview{Provider: provider, Groups: ext.Groups, NewGroups: []string{}, Roles: []string{}, MissingRoles: []string{},
		JIT: !ext.NoJIT, TrustEmail: ext.TrustEmail, Verified: ext.EmailVerified}
	for _, g := range ext.Groups {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE lower(name)=lower($1))`, g).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			out.NewGroups = append(out.NewGroups, g)
		}
	}
	for _, r := range ext.Roles {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM roles WHERE lower(name)=lower($1))`, r).Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			out.Roles = append(out.Roles, r)
		} else {
			out.MissingRoles = append(out.MissingRoles, r)
		}
	}
	var err error
	out.Outcome, out.User, err = s.previewAccount(ctx, ext)
	return out, err
}

// previewAccount mirrors linkedIdentity read-only.
func (s *Service) previewAccount(ctx context.Context, ext *ExternalIdentity) (string, string, error) {
	var handle string
	var disabled bool
	const user = `u.username || '@' || u.provider`
	err := s.pool.QueryRow(ctx, `SELECT `+user+`, u.disabled FROM user_identities i JOIN users u ON u.id=i.user_id
		WHERE i.provider=$1 AND i.subject=$2`, ext.Provider, ext.Subject).Scan(&handle, &disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.pool.QueryRow(ctx, `SELECT `+user+`, u.disabled FROM users u WHERE u.provider=$1 AND u.subject=$2`,
			ext.Provider, ext.Subject).Scan(&handle, &disabled)
	}
	switch {
	case err == nil && disabled:
		return "disabled", handle, nil
	case err == nil:
		return "existing", handle, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return "", "", err
	}
	if addr, _ := NormalizeEmail(ext.Email); addr != "" {
		err := s.pool.QueryRow(ctx, `SELECT `+user+`, u.disabled FROM user_emails e JOIN users u ON u.id=e.user_id
			WHERE e.email=$1 AND e.verified`, addr).Scan(&handle, &disabled)
		switch {
		case err == nil && disabled:
			return "disabled", handle, nil
		case err == nil && ext.TrustEmail && ext.EmailVerified:
			return "link_auto", handle, nil
		case err == nil:
			return "link_confirm", handle, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return "", "", err
		}
	}
	if ext.NoJIT {
		return "refused", "", nil
	}
	return "create", "", nil
}
