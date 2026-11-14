// Package auth_ldap authenticates users against LDAP / Active Directory.
//
// Configuration (config.yaml -> plugin_settings."auth.ldap"):
//
//	url: ldaps://ldap.example.com:636      # or ldap://...:389 with start_tls: true
//	bind_dn: cn=wiki,ou=services,dc=example,dc=com
//	bind_password: ${LDAP_BIND_PASSWORD}
//	base_dn: ou=people,dc=example,dc=com
//	user_filter: (&(objectClass=person)(uid={username}))   # AD: (sAMAccountName={username})
//	attr_username: uid        # AD: sAMAccountName
//	attr_email: mail
//	attr_display_name: cn     # or displayName
//	attr_subject: entryUUID   # stable id; AD: objectGUID; empty = DN
//	group_attribute: memberOf # groups from the user entry, and/or:
//	group_base_dn: ou=groups,dc=example,dc=com
//	group_filter: (|(member={dn})(uniqueMember={dn})(memberUid={username}))
//	group_name_attribute: cn
//	+ group_mapping / role_mapping / default_groups / default_roles / keep_unmapped_groups
package auth_ldap

import (
	"context"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"io/fs"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-ldap/ldap/v3"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/tlsutil"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// Plugin is the LDAP plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Setup registers one provider per configured instance ("ldap",
// "ldap-<id>") and the admin test endpoint.
func (p *Plugin) Setup(host *sdk.Host) error {
	set := auth.NewInstanceSet("ldap", host.Settings, func(id string, s func() map[string]any) *Provider {
		return &Provider{id: id, settings: s}
	})
	sdk.RegisterAuthInstances(host, set)
	registerSync(host, set)
	// Admin: test a login with the current settings without creating a session.
	// action: auth.ldap.test
	host.Handle("POST", "/test", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := perm.Require(r.Context(), "core.settings.manage"); err != nil {
			return nil, err
		}
		var in struct {
			Instance string `json:"instance"` // provider id; "" = ldap
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		prov, ok := set.Get(in.Instance)
		if !ok {
			return nil, apperr.NotFound("instance")
		}
		id, err := prov.Authenticate(r.Context(), in.Username, in.Password)
		switch {
		case errors.Is(err, auth.ErrUnknownUser):
			return map[string]any{"ok": false, "message": "user not found"}, nil
		case errors.Is(err, auth.ErrBadCredentials):
			return map[string]any{"ok": false, "message": "wrong password"}, nil
		case err != nil:
			return map[string]any{"ok": false, "message": err.Error()}, nil
		}
		return map[string]any{"ok": true, "identity": id}, nil
	})
	return nil
}

// Provider implements auth.PasswordProvider.
type Provider struct {
	id       string // "ldap" or "ldap-<instance>"
	settings func() map[string]any
}

// Settings implements auth.SettingsProvider.
func (p *Provider) Settings() map[string]any { return p.settings() }

// Configured implements auth.Configurable.
func (p *Provider) Configured() bool { return auth.Setting(p.settings(), "url", "") != "" }

// ID implements auth.PasswordProvider.
func (p *Provider) ID() string { return p.id }

// Name implements auth.PasswordProvider.
func (p *Provider) Name() string { return auth.Setting(p.settings(), "name", "LDAP") }

func (p *Provider) dial(s map[string]any) (*ldap.Conn, error) {
	url := auth.Setting(s, "url", "")
	if url == "" {
		return nil, errors.New("LDAP is not configured (url missing)")
	}
	timeout := 10 * time.Second
	if v, err := strconv.Atoi(auth.Setting(s, "timeout_seconds", "")); err == nil && v > 0 {
		timeout = time.Duration(v) * time.Second
	}
	host := ""
	if u, err := neturl.Parse(url); err == nil {
		host = u.Hostname()
	}
	tlsConf, err := tlsutil.Client(host, auth.Setting(s, "ca_cert", ""), auth.BoolSetting(s, "insecure_skip_verify", false))
	if err != nil {
		return nil, err
	}
	conn, err := ldap.DialURL(url, ldap.DialWithTLSConfig(tlsConf), ldap.DialWithDialer(dialer(timeout)))
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", url, err)
	}
	conn.SetTimeout(timeout)
	if auth.BoolSetting(s, "start_tls", false) {
		if err := conn.StartTLS(tlsConf); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("starttls: %w", err)
		}
	}
	return conn, nil
}

func (p *Provider) serviceBind(conn *ldap.Conn, s map[string]any) error {
	if dn := auth.Setting(s, "bind_dn", ""); dn != "" {
		if err := conn.Bind(dn, auth.Setting(s, "bind_password", "")); err != nil {
			return fmt.Errorf("service account bind failed: %w", err)
		}
	}
	return nil
}

// Authenticate implements auth.PasswordProvider.
func (p *Provider) Authenticate(ctx context.Context, username, password string) (*auth.ExternalIdentity, error) {
	s := p.settings()
	if auth.Setting(s, "url", "") == "" {
		return nil, auth.ErrUnknownUser
	}
	conn, err := p.dial(s)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	if err := p.serviceBind(conn, s); err != nil {
		return nil, err
	}
	filter := strings.ReplaceAll(auth.Setting(s, "user_filter", "(uid={username})"), "{username}", ldap.EscapeFilter(username))
	res, err := conn.Search(ldap.NewSearchRequest(auth.Setting(s, "base_dn", ""), ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
		2, 10, false, filter, entryAttributes(s), nil))
	if err != nil && !ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) {
		return nil, fmt.Errorf("user search: %w", err)
	}
	if res == nil || len(res.Entries) == 0 {
		return nil, auth.ErrUnknownUser
	}
	if len(res.Entries) > 1 {
		return nil, apperr.Conflict("the LDAP user filter matches more than one entry for %q", username)
	}
	e := res.Entries[0]
	if password == "" {
		return nil, auth.ErrBadCredentials // never allow unauthenticated binds
	}
	if err := conn.Bind(e.DN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return nil, auth.ErrBadCredentials
		}
		return nil, fmt.Errorf("user bind: %w", err)
	}
	id := p.entryIdentity(s, e, username)
	if err := p.lookupGroups(conn, s, e, id); err != nil {
		return nil, err
	}
	auth.MappingFromSettings(s).Apply(id)
	return id, nil
}

// entryAttributes are the attributes read from a person's entry.
func entryAttributes(s map[string]any) []string {
	attrs := []string{"dn", auth.Setting(s, "attr_username", "uid"), auth.Setting(s, "attr_email", "mail"),
		auth.Setting(s, "attr_display_name", "cn"), auth.Setting(s, "group_attribute", "memberOf")}
	if a := auth.Setting(s, "attr_subject", ""); a != "" {
		attrs = append(attrs, a)
	}
	if a := auth.Setting(s, "username_from", ""); a != "" {
		attrs = append(attrs, a)
	}
	return attrs
}

// entryIdentity builds the identity of a directory entry (without groups).
// The sign-in and the directory sync use it, so both see the same values.
func (p *Provider) entryIdentity(s map[string]any, e *ldap.Entry, fallbackUser string) *auth.ExternalIdentity {
	attrUser := auth.Setting(s, "attr_username", "uid")
	id := &auth.ExternalIdentity{Provider: p.id, Subject: e.DN, Username: e.GetAttributeValue(attrUser),
		Email: e.GetAttributeValue(auth.Setting(s, "attr_email", "mail")), DisplayName: e.GetAttributeValue(auth.Setting(s, "attr_display_name", "cn")),
		Attributes: map[string]any{"dn": e.DN}}
	if a := auth.Setting(s, "username_from", ""); a != "" {
		id.Attributes[a] = e.GetAttributeValue(a)
	}
	if id.Username == "" {
		id.Username = fallbackUser
	}
	id.Username = sanitizeUsername(id.Username)
	if a := auth.Setting(s, "attr_subject", ""); a != "" {
		id.Subject = subjectOf(e, a)
	}
	return id
}

// subjectOf reads the stable id of an entry; binary ids (objectGUID) as hex.
// Without the attribute the DN is the id.
func subjectOf(e *ldap.Entry, attr string) string {
	raw := e.GetRawAttributeValue(attr)
	switch {
	case len(raw) == 0:
		return e.DN
	case utf8.Valid(raw):
		return string(raw)
	default:
		return hex.EncodeToString(raw) // binary ids such as objectGUID
	}
}

// lookupGroups sets id.Groups: the values of group_attribute in the entry
// and, with group_base_dn, the groups a search with group_filter finds.
func (p *Provider) lookupGroups(conn *ldap.Conn, s map[string]any, e *ldap.Entry, id *auth.ExternalIdentity) error {
	groups := e.GetAttributeValues(auth.Setting(s, "group_attribute", "memberOf"))
	if base := auth.Setting(s, "group_base_dn", ""); base != "" {
		if err := p.serviceBind(conn, s); err != nil {
			return err
		}
		gf := auth.Setting(s, "group_filter", "(|(member={dn})(uniqueMember={dn})(memberUid={username}))")
		gf = strings.ReplaceAll(strings.ReplaceAll(gf, "{dn}", ldap.EscapeFilter(e.DN)), "{username}", ldap.EscapeFilter(id.Username))
		nameAttr := auth.Setting(s, "group_name_attribute", "cn")
		gres, err := conn.Search(ldap.NewSearchRequest(base, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1000, 10, false, gf, []string{nameAttr}, nil))
		if err != nil && !ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) {
			return fmt.Errorf("group search: %w", err)
		}
		if gres != nil {
			for _, g := range gres.Entries {
				if n := g.GetAttributeValue(nameAttr); n != "" {
					groups = append(groups, n)
				} else {
					groups = append(groups, g.DN)
				}
			}
		}
	}
	id.Groups = groups
	return nil
}

// sanitizeUsername keeps the characters allowed for user names.
func sanitizeUsername(u string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._@+-", r) {
			return r
		}
		return '_'
	}, u)
}
