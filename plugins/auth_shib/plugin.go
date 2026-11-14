// Package auth_shib provides SAML 2.0 / Shibboleth single sign-on.
//
// Two modes (config.yaml -> plugin_settings."auth.shib"):
//
// mode: saml (native service provider)
//
//	idp_metadata_url: https://idp.example.org/idp/shibboleth   # or idp_metadata: <XML>
//	entity_id: https://wiki.example.org/api/v1/x/auth.shib/metadata   (default)
//	attr_username: urn:oid:1.3.6.1.4.1.5923.1.1.1.6   # eduPersonPrincipalName (name or FriendlyName)
//	attr_email: urn:oid:0.9.2342.19200300.100.1.3     # mail
//	attr_display_name: urn:oid:2.16.840.1.113730.3.1.241  # displayName
//	attr_groups: urn:oid:1.3.6.1.4.1.5923.1.5.1.1     # isMemberOf
//
// The SP metadata for the IdP is served at /api/v1/x/auth.shib/metadata,
// the assertion consumer service is /api/v1/auth/sso/shib/callback.
//
// mode: header (Apache mod_shib in front of the wiki)
//
//	header_username: REMOTE_USER   # or eppn
//	header_email: mail
//	header_display_name: displayName
//	header_groups: isMemberOf
//	header_separator: ";"
//
// Protect /api/v1/auth/sso/shib/start with Shibboleth in Apache and add
// Apache's address to server.trusted_proxies: headers are only accepted
// from trusted proxies.
//
// Both modes support group_mapping / role_mapping / default_groups /
// default_roles / keep_unmapped_groups.
package auth_shib

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"embed"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/crewjam/saml"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/secrets"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// Plugin is the Shibboleth / SAML plugin.
type Plugin struct{}

func init() {
	sdk.Register(&Plugin{})
	// the private key of the SP is a secret like any other: sealed with
	// secrets.mode, bound to its row, part of "rewrap"
	secrets.Register(secrets.Column{Table: "plugin_shib_sp", Key: "id", Value: "key_pem", Text: true})
}

// spKeyAAD binds the sealed SP key to its row.
var spKeyAAD = secrets.AAD("plugin_shib_sp", "1")

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Setup registers the provider and the metadata endpoint.
func (p *Plugin) Setup(host *sdk.Host) error {
	replay := host.Auth.ReplayCache("saml")
	set := auth.NewInstanceSet("shib", host.Settings, func(id string, s func() map[string]any) *Provider {
		return &Provider{id: id, settings: s, cfg: host.Config, pool: host.DB, box: host.Secrets, replay: replay, log: host.Log}
	})
	sdk.RegisterRedirectInstances(host, set)
	// SP metadata of an instance: /metadata?instance=<provider id> ("" = shib)
	host.Handle("GET", "/metadata", func(w http.ResponseWriter, r *http.Request) (any, error) {
		prov, ok := set.Get(r.URL.Query().Get("instance"))
		if !ok {
			return nil, apperr.NotFound("instance")
		}
		sp, err := prov.serviceProvider(r.Context(), false)
		if err != nil {
			return nil, err
		}
		b, err := xml.MarshalIndent(sp.Metadata(), "", "  ")
		if err != nil {
			return nil, err
		}
		w.Header().Set("Content-Type", "application/samlmetadata+xml")
		_, _ = w.Write([]byte(xml.Header))
		_, _ = w.Write(b) // #nosec G705 -- XML marshalled (escaped) metadata of a configured instance, not HTML
		return h.Handled{}, nil
	})
	return nil
}

// Provider implements auth.RedirectProvider.
type Provider struct {
	id       string // "shib" or "shib-<instance>"
	settings func() map[string]any
	cfg      *config.Store
	pool     *pgxpool.Pool
	box      *secrets.Box
	replay   saml.AssertionReplayCache
	log      *slog.Logger

	mu      sync.Mutex
	idpMeta *saml.EntityDescriptor
	idpKey  string
	idpAt   time.Time
	key     *rsa.PrivateKey
	cert    *x509.Certificate
}

// Configured implements auth.Configurable.
func (p *Provider) Configured() bool {
	s := p.settings()
	return p.mode() == "header" || auth.Setting(s, "idp_metadata_url", "") != "" || auth.Setting(s, "idp_metadata", "") != ""
}

// ID implements auth.RedirectProvider.
func (p *Provider) ID() string { return p.id }

// Settings implements auth.SettingsProvider.
func (p *Provider) Settings() map[string]any { return p.settings() }

// Name implements auth.RedirectProvider.
func (p *Provider) Name() string { return auth.Setting(p.settings(), "name", "Shibboleth") }

func (p *Provider) mode() string { return auth.Setting(p.settings(), "mode", "saml") }

// Start implements auth.RedirectProvider.
func (p *Provider) Start(ctx context.Context, r *http.Request, req *auth.SSORequest) (string, *auth.ExternalIdentity, error) {
	s := p.settings()
	if p.mode() == "header" {
		if !req.TrustedProxy {
			return "", nil, apperr.ErrForbidden
		}
		id, err := IdentityFromHeaders(s, r.Header)
		if id != nil {
			id.Provider = p.id
		}
		return "", id, err
	}
	sp, err := p.serviceProvider(ctx, true)
	if err != nil {
		return "", nil, err
	}
	areq, err := sp.MakeAuthenticationRequest(sp.GetSSOBindingLocation(saml.HTTPRedirectBinding), saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		return "", nil, fmt.Errorf("authn request: %w", err)
	}
	u, err := areq.Redirect(req.State, sp)
	if err != nil {
		return "", nil, err
	}
	req.Data["request_id"] = areq.ID
	return u.String(), nil, nil
}

// Callback implements auth.RedirectProvider (assertion consumer service).
func (p *Provider) Callback(ctx context.Context, r *http.Request, req *auth.SSORequest) (*auth.ExternalIdentity, error) {
	if p.mode() == "header" {
		return nil, apperr.BadRequest("not used in header mode")
	}
	sp, err := p.serviceProvider(ctx, true)
	if err != nil {
		return nil, err
	}
	a, err := parseResponse(sp, r, []string{req.Data["request_id"]})
	if err != nil {
		var ire *saml.InvalidResponseError
		if errors.As(err, &ire) {
			return nil, fmt.Errorf("invalid SAML response: %w", ire.PrivateErr)
		}
		return nil, err
	}
	id, err := IdentityFromAssertion(p.settings(), a)
	if err != nil {
		return nil, err
	}
	id.Provider = p.id
	// for single logout: the IdP session and the NameID it knows the user by
	for _, st := range a.AuthnStatements {
		if st.SessionIndex != "" {
			id.SessionID = st.SessionIndex
			break
		}
	}
	if a.Subject != nil && a.Subject.NameID != nil {
		id.LogoutHint = a.Subject.NameID.Value
	}
	return id, nil
}

// LogoutURL implements auth.LogoutProvider: a signed LogoutRequest to the
// IdP (HTTP-Redirect binding), if the IdP offers single logout and the
// setting single_logout is on (default).
func (p *Provider) LogoutURL(ctx context.Context, sess auth.SSOSession, _ string) (string, error) {
	if p.mode() == "header" || !auth.BoolSetting(p.settings(), "single_logout", true) || sess.Hint == "" {
		return "", nil
	}
	sp, err := p.serviceProvider(ctx, true)
	if err != nil {
		return "", err
	}
	if sp.GetSLOBindingLocation(saml.HTTPRedirectBinding) == "" {
		return "", nil
	}
	u, err := sp.MakeRedirectLogoutRequest(sess.Hint, "", saml.WithSessionIndex(sess.SessionID))
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// HandleLogout implements auth.LogoutHandler: a LogoutRequest of the IdP
// (signature, issuer, destination, age and replay are checked) ends the
// sessions it names and is answered with a LogoutResponse; the
// LogoutResponse to our own request leads back to the login page.
func (p *Provider) HandleLogout(ctx context.Context, r *http.Request, base string) (m *auth.LogoutMatch, respond func(http.ResponseWriter, *http.Request), err error) {
	if p.mode() == "header" {
		return nil, nil, apperr.NotFound("provider")
	}
	sp, err := p.serviceProvider(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if x := recover(); x != nil {
			m, respond, err = nil, nil, guardFail("SAML library failed: %v", x)
		}
	}()
	done := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, base+"/login?logged_out=1", http.StatusSeeOther)
	}
	if err := r.ParseForm(); err != nil {
		return nil, nil, apperr.BadRequest("invalid form")
	}
	if r.Form.Get("SAMLResponse") != "" {
		// the answer to our LogoutRequest: the wiki session ended before
		if err := sp.ValidateLogoutResponseRequest(r); err != nil {
			var ire *saml.InvalidResponseError
			if errors.As(err, &ire) {
				err = ire.PrivateErr
			}
			p.logWarn("invalid SAML LogoutResponse", err)
		}
		return nil, done, nil
	}
	if r.Form.Get("SAMLRequest") == "" {
		return nil, nil, apperr.BadRequest("no SAML logout message")
	}
	req, err := sp.ValidateLogoutRequest(r)
	if err != nil {
		var ire *saml.InvalidResponseError
		if errors.As(err, &ire) {
			return nil, nil, fmt.Errorf("invalid SAML LogoutRequest: %w", ire.PrivateErr)
		}
		return nil, nil, err
	}
	m = &auth.LogoutMatch{}
	if req.NameID != nil {
		m.Hint = req.NameID.Value
	}
	for _, x := range req.AllSessionIndexes() {
		m.SessionIDs = append(m.SessionIDs, x.Value)
	}
	if m.Hint == "" && len(m.SessionIDs) == 0 {
		return nil, nil, apperr.BadRequest("LogoutRequest names no session")
	}
	relay := r.Form.Get("RelayState")
	if len(relay) > 80 {
		relay = "" // saml-bindings 3.4.3: at most 80 bytes
	}
	respond = func(w http.ResponseWriter, r *http.Request) {
		if sp.GetSLOBindingLocation(saml.HTTPRedirectBinding) == "" {
			w.WriteHeader(http.StatusOK)
			return
		}
		u, err := sp.MakeRedirectLogoutResponse(req.ID, relay)
		if err != nil {
			http.Error(w, "logout response failed", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, u.String(), http.StatusFound) // #nosec G710 -- SLO endpoint of the configured IdP
	}
	return m, respond, nil
}

func (p *Provider) logWarn(msg string, err error) {
	if p.log != nil {
		p.log.Warn(msg, "err", err)
	}
}

// IdentityFromAssertion maps SAML attributes to an identity.
func IdentityFromAssertion(s map[string]any, a *saml.Assertion) (*auth.ExternalIdentity, error) {
	attrs := map[string][]string{}
	for _, st := range a.AttributeStatements {
		for _, at := range st.Attributes {
			for _, v := range at.Values {
				attrs[at.Name] = append(attrs[at.Name], v.Value)
				if at.FriendlyName != "" {
					attrs[at.FriendlyName] = append(attrs[at.FriendlyName], v.Value)
				}
			}
		}
	}
	first := func(key, def string) string {
		if v := attrs[auth.Setting(s, key, def)]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	id := &auth.ExternalIdentity{Provider: "shib",
		Username:    first("attr_username", "urn:oid:1.3.6.1.4.1.5923.1.1.1.6"),
		Email:       first("attr_email", "urn:oid:0.9.2342.19200300.100.1.3"),
		DisplayName: first("attr_display_name", "urn:oid:2.16.840.1.113730.3.1.241"),
		Attributes:  map[string]any{}}
	nameID := ""
	if a.Subject != nil && a.Subject.NameID != nil {
		nameID = a.Subject.NameID.Value
	}
	if id.Username == "" {
		id.Username = nameID
	}
	if id.Username == "" {
		return nil, errors.New("SAML assertion contains no user name attribute")
	}
	id.Subject = id.Username
	if auth.BoolSetting(s, "subject_from_nameid", false) && nameID != "" {
		id.Subject = nameID
	}
	id.Username = sanitize(id.Username)
	id.Groups = attrs[auth.Setting(s, "attr_groups", "urn:oid:1.3.6.1.4.1.5923.1.5.1.1")]
	for k, v := range attrs {
		id.Attributes[k] = v
	}
	auth.MappingFromSettings(s).Apply(id)
	return id, nil
}

// IdentityFromHeaders maps headers set by mod_shib to an identity.
func IdentityFromHeaders(s map[string]any, hdr http.Header) (*auth.ExternalIdentity, error) {
	get := func(key, def string) string { return strings.TrimSpace(hdr.Get(auth.Setting(s, key, def))) }
	user := get("header_username", "REMOTE_USER")
	if user == "" {
		user = strings.TrimSpace(hdr.Get("eppn"))
	}
	if user == "" {
		return nil, apperr.BadRequest("no Shibboleth session (user header missing)")
	}
	sep := auth.Setting(s, "header_separator", ";")
	var groups []string
	for _, g := range strings.Split(get("header_groups", "isMemberOf"), sep) {
		if g = strings.TrimSpace(g); g != "" {
			groups = append(groups, g)
		}
	}
	id := &auth.ExternalIdentity{Provider: "shib", Subject: user, Username: sanitize(user), Email: get("header_email", "mail"),
		DisplayName: get("header_display_name", "displayName"), Groups: groups, Attributes: map[string]any{}}
	auth.MappingFromSettings(s).Apply(id)
	return id, nil
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

// serviceProvider builds the SP. needIdP requires IdP metadata.
func (p *Provider) serviceProvider(ctx context.Context, needIdP bool) (*saml.ServiceProvider, error) {
	s := p.settings()
	base := strings.TrimRight(p.cfg.Get().Server.BaseURL, "/")
	acs, _ := url.Parse(base + "/api/v1/auth/sso/" + p.id + "/callback")
	md, _ := url.Parse(base + "/api/v1/x/auth.shib/metadata")
	if p.id != "shib" {
		md.RawQuery = url.Values{"instance": {p.id}}.Encode()
	}
	slo, _ := url.Parse(base + "/api/v1/auth/sso/" + p.id + "/logout")
	key, cert, err := p.keyPair(ctx)
	if err != nil {
		return nil, err
	}
	sp := &saml.ServiceProvider{EntityID: auth.Setting(s, "entity_id", md.String()), Key: key, Certificate: cert,
		MetadataURL: *md, AcsURL: *acs, SloURL: *slo, LogoutBindings: []string{saml.HTTPRedirectBinding, saml.HTTPPostBinding}, ReplayCache: p.replay, AllowIDPInitiated: false, SignatureMethod: "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"}
	idp, err := p.idpMetadata(ctx, s)
	if err != nil && needIdP {
		return nil, err
	}
	sp.IDPMetadata = idp
	return sp, nil
}

func (p *Provider) idpMetadata(ctx context.Context, s map[string]any) (*saml.EntityDescriptor, error) {
	raw := auth.Setting(s, "idp_metadata", "")
	u := auth.Setting(s, "idp_metadata_url", "")
	if raw == "" && u == "" {
		return nil, apperr.BadRequest("SAML is not configured (idp_metadata_url or idp_metadata required)")
	}
	key := u + "\x00" + raw
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.idpMeta != nil && p.idpKey == key && time.Since(p.idpAt) < time.Hour {
		return p.idpMeta, nil
	}
	data := []byte(raw)
	if raw == "" {
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(cctx, http.MethodGet, u, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch IdP metadata: %w", err)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("fetch IdP metadata: HTTP %d", res.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(res.Body, 10<<20))
		if err != nil {
			return nil, err
		}
	}
	ed, err := ParseMetadata(data)
	if err != nil {
		return nil, err
	}
	p.idpMeta, p.idpKey, p.idpAt = ed, key, time.Now()
	return ed, nil
}

// ParseMetadata parses an EntityDescriptor or the first IdP of an
// EntitiesDescriptor (federation metadata).
func ParseMetadata(data []byte) (*saml.EntityDescriptor, error) {
	ed := &saml.EntityDescriptor{}
	if err := xml.Unmarshal(data, ed); err == nil && len(ed.IDPSSODescriptors) > 0 {
		return ed, nil
	}
	var all saml.EntitiesDescriptor
	if err := xml.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("invalid IdP metadata: %w", err)
	}
	for i := range all.EntityDescriptors {
		if len(all.EntityDescriptors[i].IDPSSODescriptors) > 0 {
			return &all.EntityDescriptors[i], nil
		}
	}
	return nil, errors.New("IdP metadata contains no IDPSSODescriptor")
}

// keyPair loads or creates the SP key and self-signed certificate.
func (p *Provider) keyPair(ctx context.Context) (*rsa.PrivateKey, *x509.Certificate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.key != nil {
		return p.key, p.cert, nil
	}
	var keyPEM, certPEM string
	err := p.pool.QueryRow(ctx, `SELECT key_pem, cert_pem FROM plugin_shib_sp WHERE id=1`).Scan(&keyPEM, &certPEM)
	if errors.Is(err, pgx.ErrNoRows) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, nil, err
		}
		tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "Enterprise Wiki SAML SP"},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(20, 0, 0),
			KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			return nil, nil, err
		}
		keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
		certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
		sealed, err := p.box.SealString(keyPEM, spKeyAAD)
		if err != nil {
			return nil, nil, err
		}
		if _, err := p.pool.Exec(ctx, `INSERT INTO plugin_shib_sp(id, key_pem, cert_pem) VALUES (1,$1,$2) ON CONFLICT (id) DO NOTHING`, sealed, certPEM); err != nil {
			return nil, nil, err
		}
		if err := p.pool.QueryRow(ctx, `SELECT key_pem, cert_pem FROM plugin_shib_sp WHERE id=1`).Scan(&keyPEM, &certPEM); err != nil {
			return nil, nil, err
		}
	} else if err != nil {
		return nil, nil, err
	}
	if keyPEM, err = p.box.OpenString(keyPEM, spKeyAAD); err != nil {
		return nil, nil, fmt.Errorf("SAML key pair: %w", err)
	}
	kb, _ := pem.Decode([]byte(keyPEM))
	cb, _ := pem.Decode([]byte(certPEM))
	if kb == nil || cb == nil {
		return nil, nil, errors.New("stored SAML key pair is invalid")
	}
	key, err := x509.ParsePKCS1PrivateKey(kb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	p.key, p.cert = key, cert
	return key, cert, nil
}
