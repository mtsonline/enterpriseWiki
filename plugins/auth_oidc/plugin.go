// Package auth_oidc provides single sign-on with OpenID Connect.
//
// Configuration (config.yaml -> plugin_settings."auth.oidc"):
//
//	name: Company login                 # button label
//	issuer: https://keycloak.example.com/realms/acme
//	client_id: enterprise-wiki
//	client_secret: ${OIDC_CLIENT_SECRET}
//	scopes: [openid, profile, email, groups]
//	claim_username: preferred_username
//	claim_email: email
//	claim_display_name: name
//	claim_groups: groups
//	+ group_mapping / role_mapping / default_groups / default_roles / keep_unmapped_groups
//
// Redirect URI to register at the provider:
// <server.base_url>/api/v1/auth/sso/oidc/callback
package auth_oidc

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// Plugin is the OIDC plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Setup registers the provider and an admin check endpoint.
func (p *Plugin) Setup(host *sdk.Host) error {
	prov := &Provider{settings: host.Settings}
	host.RegisterRedirectProvider(prov)
	host.Handle("GET", "/check", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := sdk.RequireAdmin(r); err != nil {
			return nil, err
		}
		s := host.Settings()
		_, cfg, err := prov.provider(r.Context(), s)
		if err != nil {
			return map[string]any{"ok": false, "message": err.Error()}, nil
		}
		return map[string]any{"ok": true, "authorization_endpoint": cfg.Endpoint.AuthURL,
			"redirect_uri": strings.TrimRight(host.Config.Get().Server.BaseURL, "/") + "/api/v1/auth/sso/oidc/callback"}, nil
	})
	return nil
}

// Provider implements auth.RedirectProvider.
type Provider struct {
	settings func() map[string]any
	mu       sync.Mutex
	cached   *oidc.Provider
	issuer   string
	at       time.Time
}

// Configured implements auth.Configurable.
func (p *Provider) Configured() bool {
	s := p.settings()
	return auth.Setting(s, "issuer", "") != "" && auth.Setting(s, "client_id", "") != ""
}

// ID implements auth.RedirectProvider.
func (p *Provider) ID() string { return "oidc" }

// Name implements auth.RedirectProvider.
func (p *Provider) Name() string { return auth.Setting(p.settings(), "name", "Single sign-on") }

// provider returns the (cached, 1h) discovery document and the OAuth2 config.
func (p *Provider) provider(ctx context.Context, s map[string]any) (*oidc.Provider, *oauth2.Config, error) {
	issuer := auth.Setting(s, "issuer", "")
	if issuer == "" || auth.Setting(s, "client_id", "") == "" {
		return nil, nil, apperr.BadRequest("OpenID Connect is not configured (issuer and client_id are required)")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cached == nil || p.issuer != issuer || time.Since(p.at) > time.Hour {
		dctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		prov, err := oidc.NewProvider(dctx, issuer)
		if err != nil {
			return nil, nil, fmt.Errorf("discovery %s: %w", issuer, err)
		}
		p.cached, p.issuer, p.at = prov, issuer, time.Now()
	}
	scopes := auth.StringList(s["scopes"])
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}
	return p.cached, &oauth2.Config{ClientID: auth.Setting(s, "client_id", ""), ClientSecret: auth.Setting(s, "client_secret", ""),
		Endpoint: p.cached.Endpoint(), Scopes: scopes}, nil
}

// Start implements auth.RedirectProvider: redirect to the provider with
// state, nonce and a PKCE challenge.
func (p *Provider) Start(ctx context.Context, r *http.Request, req *auth.SSORequest) (string, *auth.ExternalIdentity, error) {
	_, cfg, err := p.provider(ctx, p.settings())
	if err != nil {
		return "", nil, err
	}
	cfg.RedirectURL = req.CallbackURL
	return cfg.AuthCodeURL(req.State, oidc.Nonce(req.Nonce), oauth2.S256ChallengeOption(req.Verifier)), nil, nil
}

// Callback implements auth.RedirectProvider: exchange the code, verify the
// ID token (signature, issuer, audience, expiry, nonce) and map the claims.
func (p *Provider) Callback(ctx context.Context, r *http.Request, req *auth.SSORequest) (*auth.ExternalIdentity, error) {
	if e := r.FormValue("error"); e != "" {
		return nil, apperr.BadRequest("the identity provider reported: %s %s", e, r.FormValue("error_description"))
	}
	s := p.settings()
	prov, cfg, err := p.provider(ctx, s)
	if err != nil {
		return nil, err
	}
	cfg.RedirectURL = req.CallbackURL
	tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tok, err := cfg.Exchange(tctx, r.FormValue("code"), oauth2.VerifierOption(req.Verifier))
	if err != nil {
		return nil, fmt.Errorf("code exchange: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("no id_token in token response")
	}
	idt, err := prov.Verifier(&oidc.Config{ClientID: cfg.ClientID}).Verify(tctx, raw)
	if err != nil {
		return nil, fmt.Errorf("id token: %w", err)
	}
	if idt.Nonce != req.Nonce {
		return nil, errors.New("id token nonce mismatch")
	}
	claims := map[string]any{}
	if err := idt.Claims(&claims); err != nil {
		return nil, err
	}
	// Merge userinfo claims (some providers only put groups there).
	if ui, err := prov.UserInfo(tctx, oauth2.StaticTokenSource(tok)); err == nil {
		extra := map[string]any{}
		if ui.Claims(&extra) == nil && extra["sub"] == idt.Subject {
			for k, v := range extra {
				if _, exists := claims[k]; !exists {
					claims[k] = v
				}
			}
		}
	}
	return MapClaims(s, idt.Subject, claims), nil
}

// MapClaims converts ID token claims into an external identity.
func MapClaims(s map[string]any, subject string, claims map[string]any) *auth.ExternalIdentity {
	str := func(k string) string {
		if v, ok := claims[k].(string); ok {
			return v
		}
		return ""
	}
	id := &auth.ExternalIdentity{Provider: "oidc", Subject: subject,
		Username: str(auth.Setting(s, "claim_username", "preferred_username")), Email: str(auth.Setting(s, "claim_email", "email")),
		DisplayName: str(auth.Setting(s, "claim_display_name", "name")), Attributes: map[string]any{}}
	if id.Username == "" {
		id.Username = id.Email
	}
	if id.Username == "" {
		id.Username = subject
	}
	id.Username = sanitize(id.Username)
	for _, k := range []string{"email", "name", "preferred_username", "given_name", "family_name"} {
		if v, ok := claims[k]; ok {
			id.Attributes[k] = v
		}
	}
	id.Groups = auth.StringList(claims[auth.Setting(s, "claim_groups", "groups")])
	auth.MappingFromSettings(s).Apply(id)
	return id
}

func sanitize(u string) string {
	u = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._@+-", r) {
			return r
		}
		return '_'
	}, u)
	if len(u) > 128 {
		u = u[:128]
	}
	return u
}
