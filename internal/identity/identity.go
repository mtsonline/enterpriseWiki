// Package identity defines the authenticated principal that flows through
// every request (HTTP, API key, SSH token or CLI).
package identity

import "context"

// Principal is the caller of an operation. A nil *Principal is a guest.
type Principal struct {
	UserID      int64   `json:"id"`
	Username    string  `json:"username"`
	Provider    string  `json:"provider"`
	DisplayName string  `json:"display_name"`
	Email       string  `json:"email"`
	Language    string  `json:"language"`
	Groups      []int64 `json:"groups"`
	Roles       []int64 `json:"roles"`
	IsAdmin     bool    `json:"is_admin"`
	// InstanceAdmin manages sites, users, groups, roles and the license
	// (instance_admins or administrator of the site "main"); such users
	// are administrators on every site.
	InstanceAdmin bool `json:"instance_admin"`
	// Perms are the global permissions (from roles and the defaults for
	// signed-in users); see package perm. Administrators have all.
	Perms []string `json:"permissions"`
	// Via is how the caller authenticated: session, apikey, ssh, feed, cli
	// (the local superuser), cli-as (local CLI acting as a user via --as;
	// carries exactly that user's rights, never more) or demo (the demo
	// content created in-process with the rights of the administrator).
	Via string `json:"via"`
	// API key restrictions (Via == "apikey").
	KeyPerms      int   `json:"-"`
	KeyCategory   int64 `json:"-"`
	KeyAllowAdmin bool  `json:"-"`
	// Networks restricts where the account may be used (CIDR; empty = anywhere).
	Networks []string `json:"-"`
}

// Handle returns the unique login handle name@provider.
func (p *Principal) Handle() string {
	if p == nil {
		return "guest"
	}
	if p.IsCLI() {
		return "cli"
	}
	return p.Username + "@" + p.Provider
}

// ID returns the user id or 0 for guests / the CLI.
func (p *Principal) ID() int64 {
	if p == nil {
		return 0
	}
	return p.UserID
}

// Admin reports whether the principal may perform administrative actions.
// API keys only carry admin rights when created with allow_admin.
func (p *Principal) Admin() bool {
	if p == nil || !p.IsAdmin {
		return false
	}
	if p.Via == "apikey" && !p.KeyAllowAdmin {
		return false
	}
	return true
}

// IsCLI reports whether p is the local command line superuser. A user
// impersonated with --as (Via "cli-as") is not.
func (p *Principal) IsCLI() bool {
	return p != nil && p.Via == "cli" && p.UserID == 0
}

// InProcess reports whether p was created by the local CLI (superuser or
// --as USER); such requests never pass through HTTP authentication.
func (p *Principal) InProcess() bool {
	return p != nil && (p.IsCLI() || p.Via == "cli-as" || p.Via == "demo")
}

// CLI returns the principal used by local command line invocations
// (whoever can run the binary as the service user is a superadmin).
func CLI() *Principal {
	return &Principal{Username: "cli", Provider: "local", DisplayName: "Command line", IsAdmin: true, InstanceAdmin: true, Via: "cli"}
}

// Instance reports whether the principal may change data shared by all
// sites (API keys need allow_admin).
func (p *Principal) Instance() bool {
	return p != nil && p.InstanceAdmin && p.Admin()
}

type ctxKey struct{}

// With stores p in ctx.
func With(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// From returns the principal stored in ctx (nil for guests).
func From(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}

type siteKey struct{}

// WithSite stores the site a request or job belongs to.
func WithSite(ctx context.Context, site string) context.Context {
	return context.WithValue(ctx, siteKey{}, site)
}

// Site returns the site of ctx ("main" if none is set).
func Site(ctx context.Context) string {
	if s, ok := ctx.Value(siteKey{}).(string); ok && s != "" {
		return s
	}
	return "main"
}
