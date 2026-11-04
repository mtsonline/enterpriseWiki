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
	// Perms are the global permissions (from roles and the defaults for
	// signed-in users); see package perm. Administrators have all.
	Perms []string `json:"permissions"`
	// Via is how the caller authenticated: session, apikey, ssh, cli.
	Via string `json:"via"`
	// API key restrictions (Via == "apikey").
	KeyPerms      int   `json:"-"`
	KeyCategory   int64 `json:"-"`
	KeyAllowAdmin bool  `json:"-"`
}

// Handle returns the unique login handle name@provider.
func (p *Principal) Handle() string {
	if p == nil {
		return "guest"
	}
	if p.Via == "cli" && p.UserID == 0 {
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

// CLI returns the principal used by local command line invocations
// (whoever can run the binary as the service user is a superadmin).
func CLI() *Principal {
	return &Principal{Username: "cli", Provider: "local", DisplayName: "Command line", IsAdmin: true, Via: "cli"}
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
