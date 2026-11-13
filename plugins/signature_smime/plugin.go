// Package signature_smime signs outgoing e-mails with S/MIME (RFC 8551):
// certificates of the organisation for the senders of the wiki and personal
// certificates for e-mails sent in the name of a person. It warns before
// certificates expire and can seal signature evidence additionally with the
// certificate of the organisation.
//
// Settings (plugin_settings."signature.smime"):
//
//	mail_signing:  sign e-mails when a certificate fits the sender (default true)
//	on_expired:    "unsigned" (default) or "hold": what happens to e-mails of a
//	               sender whose certificates have all expired
//	personal:      people may upload their own certificate (default true)
//	seal_evidence: seal signature evidence with the certificate of the
//	               organisation marked "seal" (default false)
package signature_smime

import (
	"context"
	"crypto"
	"crypto/x509"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/mail"
	"github.com/mtsonline/enterprisewiki/internal/notify"
	"github.com/mtsonline/enterprisewiki/internal/secrets"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// ID is the plugin id.
const ID = "signature.smime"

// table holds the certificates; its key column is sealed.
const table = "plugin_smime_certs"

// Plugin is the S/MIME plugin.
type Plugin struct {
	host *sdk.Host
	now  func() time.Time
}

func init() {
	sdk.Register(&Plugin{})
	secrets.Register(secrets.Column{Table: table, Key: "id", Value: "key"})
	notify.RegisterKind(ID+".expiry", "signature.smime.notify_expiry")
}

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Setup registers the signer of e-mails, the seal, the routes and the
// check of expiring certificates. Every site gets its own instance.
func (p *Plugin) Setup(host *sdk.Host) error {
	q := &Plugin{host: host, now: time.Now}
	host.Mail.SetSigner(q)
	host.Signatures.SetCMSSealer(q.seal)
	q.routes()
	host.RegisterWorker("expiry", 6*time.Hour, q.checkExpiry)
	return nil
}

// ---------------------------------------------------------------- settings

// Settings are the S/MIME settings of a site.
type Settings struct {
	MailSigning  bool   `json:"mail_signing"`
	OnExpired    string `json:"on_expired"` // unsigned | hold
	Personal     bool   `json:"personal"`
	SealEvidence bool   `json:"seal_evidence"`
}

func (p *Plugin) settings() Settings {
	s := p.host.Settings()
	b := func(k string, def bool) bool {
		if v, ok := s[k].(bool); ok {
			return v
		}
		return def
	}
	out := Settings{MailSigning: b("mail_signing", true), Personal: b("personal", true), SealEvidence: b("seal_evidence", false), OnExpired: "unsigned"}
	if v, _ := s["on_expired"].(string); v == "hold" {
		out.OnExpired = "hold"
	}
	return out
}

// ---------------------------------------------------------------- keys

func aad(id int64) string { return secrets.AAD(table, strconv.FormatInt(id, 10)) }

// loaded is a certificate ready to sign.
type loaded struct {
	id      int64
	userID  *int64
	cert    *x509.Certificate
	parents []*x509.Certificate
	key     crypto.Signer
}

func (p *Plugin) load(id int64, userID *int64, chain string, sealed []byte) (*loaded, error) {
	cert, parents, err := parseChainPEM(chain)
	if err != nil {
		return nil, fmt.Errorf("certificate %d: %w", id, err)
	}
	der, err := p.host.Secrets.Open(sealed, aad(id))
	if err != nil {
		return nil, fmt.Errorf("certificate %d: the private key cannot be opened: %w", id, err)
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("certificate %d: %w", id, err)
	}
	signer, ok := k.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("certificate %d: the key cannot sign", id)
	}
	return &loaded{id: id, userID: userID, cert: cert, parents: parents, key: signer}, nil
}

// ---------------------------------------------------------------- e-mail

// SignDetached implements mail.Signer: a valid personal certificate of the
// person who owns the sender address (confirmed address) comes first, then
// one of the organisation. The newest valid certificate wins, so a renewed
// certificate takes over at the start of its validity.
func (p *Plugin) SignDetached(ctx context.Context, from string, msgID int64, content []byte) ([]byte, error) {
	if !p.host.Active() {
		return nil, nil
	}
	st := p.settings()
	if !st.MailSigning {
		return nil, nil
	}
	from = strings.ToLower(strings.TrimSpace(from))
	now := p.now()
	pick := func(personal bool) (*loaded, error) {
		var (
			id     int64
			userID *int64
			chain  string
			key    []byte
		)
		sql := `SELECT c.id, c.user_id, c.chain, c.key FROM plugin_smime_certs c WHERE c.user_id IS NULL AND c.sign_mail
			AND $1 = ANY(c.emails) AND c.not_before <= $2 AND c.not_after > $2 ORDER BY c.not_before DESC, c.id DESC LIMIT 1`
		if personal {
			sql = `SELECT c.id, c.user_id, c.chain, c.key FROM plugin_smime_certs c JOIN users u ON u.id = c.user_id
				WHERE c.sign_mail AND $1 = ANY(c.emails) AND c.not_before <= $2 AND c.not_after > $2 AND NOT u.disabled
				AND EXISTS (SELECT 1 FROM user_emails e WHERE e.user_id = u.id AND e.email = $1 AND e.verified)
				ORDER BY c.not_before DESC, c.id DESC LIMIT 1`
		}
		err := p.host.DB.QueryRow(ctx, sql, from, now).Scan(&id, &userID, &chain, &key)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return p.load(id, userID, chain, key)
	}
	var (
		c   *loaded
		err error
	)
	if st.Personal {
		if c, err = pick(true); err != nil {
			return nil, err
		}
	}
	if c == nil {
		if c, err = pick(false); err != nil {
			return nil, err
		}
	}
	if c == nil {
		// no valid certificate: held only when one for this sender has expired
		var expired bool
		if err := p.host.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM plugin_smime_certs WHERE sign_mail AND $1 = ANY(emails)
			AND not_after <= $2 AND ($3 OR user_id IS NULL))`, from, now, st.Personal).Scan(&expired); err != nil {
			return nil, err
		}
		if expired && st.OnExpired == "hold" {
			return nil, fmt.Errorf("%w: the S/MIME certificate of %s has expired", mail.ErrHold, from)
		}
		return nil, nil
	}
	sig, err := signDetached(content, c.cert, c.key, c.parents)
	if err != nil {
		return nil, err
	}
	if c.userID != nil {
		// every use of a personal key is recorded
		_ = audit.Log(ctx, p.host.DB, "signature.smime.mail_signed", "mail", msgID, nil,
			map[string]any{"certificate": c.id, "user_id": *c.userID, "from": from})
	}
	return sig, nil
}

// ---------------------------------------------------------------- seal

// seal implements signature.CMSSealer with the newest valid certificate of
// the organisation marked "seal".
func (p *Plugin) seal(ctx context.Context, q db.Querier, msg []byte) ([]byte, error) {
	if !p.host.Active() || !p.settings().SealEvidence {
		return nil, nil
	}
	var (
		id    int64
		chain string
		key   []byte
	)
	err := q.QueryRow(ctx, `SELECT id, chain, key FROM plugin_smime_certs WHERE user_id IS NULL AND seal
		AND not_before <= $1 AND not_after > $1 ORDER BY not_before DESC, id DESC LIMIT 1`, p.now()).Scan(&id, &chain, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		p.host.Log.Warn("S/MIME seal of signature evidence is on, but no valid certificate is marked for it")
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c, err := p.load(id, nil, chain, key)
	if err != nil {
		return nil, err
	}
	return signDetached(msg, c.cert, c.key, c.parents)
}
