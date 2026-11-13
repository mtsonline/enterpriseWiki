package signature_smime

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/perm"
)

const permManage = "signature.smime.manage"

// Cert is a stored certificate as the API shows it (never the key).
type Cert struct {
	ID          int64     `json:"id"`
	UserID      *int64    `json:"user_id"`
	Owner       string    `json:"owner,omitempty"` // name of the person (administrators' list)
	Label       string    `json:"label"`
	Emails      []string  `json:"emails"`
	Subject     string    `json:"subject"`
	Issuer      string    `json:"issuer"`
	Serial      string    `json:"serial"`
	Fingerprint string    `json:"fingerprint"`
	NotBefore   time.Time `json:"not_before"`
	NotAfter    time.Time `json:"not_after"`
	Trusted     bool      `json:"trusted"`
	SignMail    bool      `json:"sign_mail"`
	Seal        bool      `json:"seal"`
	CreatedAt   time.Time `json:"created_at"`
	// State: valid, future (not yet valid) or expired; DaysLeft until it expires.
	State    string `json:"state"`
	DaysLeft int    `json:"days_left"`
}

const certCols = `c.id, c.user_id, COALESCE(NULLIF(u.display_name, ''), u.username, ''), c.label, c.emails, c.subject, c.issuer, c.serial, c.fingerprint,
	c.not_before, c.not_after, c.trusted, c.sign_mail, c.seal, c.created_at`

func (p *Plugin) scanCert(r pgx.CollectableRow) (Cert, error) {
	var c Cert
	err := r.Scan(&c.ID, &c.UserID, &c.Owner, &c.Label, &c.Emails, &c.Subject, &c.Issuer, &c.Serial, &c.Fingerprint,
		&c.NotBefore, &c.NotAfter, &c.Trusted, &c.SignMail, &c.Seal, &c.CreatedAt)
	now := p.now()
	switch {
	case !now.Before(c.NotAfter):
		c.State = "expired"
	case now.Before(c.NotBefore):
		c.State = "future"
	default:
		c.State = "valid"
	}
	c.DaysLeft = int(math.Ceil(c.NotAfter.Sub(now).Hours() / 24))
	return c, err
}

// list returns the certificates of the organisation (scope "org"), of the
// caller ("mine") or all, personal ones included ("all").
func (p *Plugin) list(ctx context.Context, scope string, uid int64) ([]Cert, error) {
	where := "c.user_id IS NULL"
	args := []any{}
	switch scope {
	case "mine":
		where, args = "c.user_id = $1", []any{uid}
	case "all":
		where = "true"
	}
	rows, err := p.host.DB.Query(ctx, `SELECT `+certCols+` FROM plugin_smime_certs c LEFT JOIN users u ON u.id = c.user_id
		WHERE `+where+` ORDER BY c.user_id NULLS FIRST, c.not_after DESC, c.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, p.scanCert)
}

func (p *Plugin) get(ctx context.Context, id int64) (*Cert, error) {
	rows, err := p.host.DB.Query(ctx, `SELECT `+certCols+` FROM plugin_smime_certs c LEFT JOIN users u ON u.id = c.user_id WHERE c.id = $1`, id)
	if err != nil {
		return nil, err
	}
	c, err := pgx.CollectExactlyOneRow(rows, p.scanCert)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("certificate")
	}
	return &c, err
}

// mayChange: own personal certificates; administrators (permission
// signature.smime.manage) all.
func (p *Plugin) mayChange(ctx context.Context, c *Cert) error {
	pr := identity.From(ctx)
	if pr == nil {
		return apperr.ErrUnauthorized
	}
	if c.UserID != nil && pr.UserID > 0 && *c.UserID == pr.UserID {
		return nil
	}
	if perm.Has(ctx, permManage) {
		return nil
	}
	return apperr.NotFound("certificate")
}

// Upload is the body of POST /certs.
type Upload struct {
	Scope    string `json:"scope"` // org | mine
	Label    string `json:"label"`
	File     string `json:"file"` // base64 of the PKCS#12 or PEM file
	Password string `json:"password"`
	SignMail *bool  `json:"sign_mail"`
	Seal     bool   `json:"seal"`
}

func (p *Plugin) upload(ctx context.Context, in Upload) (*Cert, error) {
	pr := identity.From(ctx)
	if pr == nil {
		return nil, apperr.ErrUnauthorized
	}
	var owner *int64
	switch in.Scope {
	case "org":
		if err := perm.Require(ctx, permManage); err != nil {
			return nil, err
		}
	case "mine":
		if !p.settings().Personal {
			return nil, apperr.Forbidden("personal S/MIME certificates are switched off")
		}
		if pr.UserID <= 0 {
			return nil, apperr.BadRequest("personal certificates need an account (ewiki ... --as USER)")
		}
		uid := pr.UserID
		owner = &uid
		if in.Seal {
			return nil, apperr.BadRequest("only certificates of the organisation can seal evidence")
		}
	default:
		return nil, apperr.BadRequest("scope: org or mine")
	}
	in.Label = strings.TrimSpace(in.Label)
	if len([]rune(in.Label)) > 100 {
		return nil, apperr.BadRequest("the label is longer than 100 characters")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.File))
	if err != nil {
		return nil, apperr.BadRequest("file: not base64")
	}
	c, err := parseUpload(data, in.Password, p.now())
	if err != nil {
		return nil, apperr.BadRequest("%s", err.Error())
	}
	// the certificate must name an address it will sign for
	if owner == nil {
		if !slices.ContainsFunc(c.emails, p.host.Mail.SenderAllowed) {
			return nil, apperr.BadRequest("no address of the certificate (%s) is a sender of the wiki (mail.from or mail.allowed_from)",
				strings.Join(c.emails, ", "))
		}
	} else {
		var ok bool
		if err := p.host.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_emails WHERE user_id=$1 AND verified AND email = ANY($2))`,
			*owner, c.emails).Scan(&ok); err != nil {
			return nil, err
		}
		if !ok {
			return nil, apperr.BadRequest("no address of the certificate (%s) is a confirmed address of your account", strings.Join(c.emails, ", "))
		}
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(c.key)
	if err != nil {
		return nil, err
	}
	signMail := in.SignMail == nil || *in.SignMail
	var createdBy *int64
	if pr.UserID > 0 {
		createdBy = &pr.UserID
	}
	var id int64
	err = pgx.BeginFunc(ctx, p.host.DB, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO plugin_smime_certs(user_id, label, emails, subject, issuer, serial, fingerprint, not_before, not_after,
				trusted, chain, key, sign_mail, seal, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'\x',$12,$13,$14)
			ON CONFLICT (fingerprint, COALESCE(user_id, 0)) DO NOTHING RETURNING id`,
			owner, in.Label, c.emails, c.cert.Subject.String(), c.cert.Issuer.String(), c.cert.SerialNumber.Text(16), fingerprintOf(c.cert),
			c.cert.NotBefore, c.cert.NotAfter, c.trusted, chainPEM(c.cert, c.parents), signMail, in.Seal, createdBy).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Conflict("this certificate is already stored")
		}
		if err != nil {
			return err
		}
		sealed, err := p.host.Secrets.Seal(keyDER, aad(id))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE plugin_smime_certs SET key=$2 WHERE id=$1`, id, sealed); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "signature.smime.cert_add", "smime_cert", id, nil,
			map[string]any{"user_id": owner, "emails": c.emails, "fingerprint": fingerprintOf(c.cert), "not_after": c.cert.NotAfter})
	})
	clear(keyDER)
	if err != nil {
		return nil, err
	}
	return p.get(ctx, id)
}

// Change is the body of PATCH /certs/{id}.
type Change struct {
	Label    *string `json:"label"`
	SignMail *bool   `json:"sign_mail"`
	Seal     *bool   `json:"seal"`
}

func (p *Plugin) change(ctx context.Context, id int64, in Change) (*Cert, error) {
	c, err := p.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := p.mayChange(ctx, c); err != nil {
		return nil, err
	}
	after := *c
	if in.Label != nil {
		l := strings.TrimSpace(*in.Label)
		if len([]rune(l)) > 100 {
			return nil, apperr.BadRequest("the label is longer than 100 characters")
		}
		after.Label = l
	}
	if in.SignMail != nil {
		after.SignMail = *in.SignMail
	}
	if in.Seal != nil {
		if c.UserID != nil && *in.Seal {
			return nil, apperr.BadRequest("only certificates of the organisation can seal evidence")
		}
		if err := perm.Require(ctx, permManage); err != nil {
			return nil, err
		}
		after.Seal = *in.Seal
	}
	err = pgx.BeginFunc(ctx, p.host.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE plugin_smime_certs SET label=$2, sign_mail=$3, seal=$4 WHERE id=$1`, id, after.Label, after.SignMail, after.Seal); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "signature.smime.cert_change", "smime_cert", id,
			map[string]any{"label": c.Label, "sign_mail": c.SignMail, "seal": c.Seal},
			map[string]any{"label": after.Label, "sign_mail": after.SignMail, "seal": after.Seal})
	})
	if err != nil {
		return nil, err
	}
	return p.get(ctx, id)
}

func (p *Plugin) remove(ctx context.Context, id int64) error {
	c, err := p.get(ctx, id)
	if err != nil {
		return err
	}
	if err := p.mayChange(ctx, c); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, p.host.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM plugin_smime_certs WHERE id=$1`, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, "signature.smime.cert_delete", "smime_cert", id,
			map[string]any{"user_id": c.UserID, "emails": c.Emails, "fingerprint": c.Fingerprint}, nil)
	})
}

func (p *Plugin) saveSettings(ctx context.Context, in Settings) (Settings, error) {
	if err := perm.Require(ctx, permManage); err != nil {
		return Settings{}, err
	}
	if in.OnExpired != "unsigned" && in.OnExpired != "hold" {
		return Settings{}, apperr.BadRequest("on_expired: unsigned or hold")
	}
	before := p.settings()
	err := p.host.Config.Update(func(c *config.Config) error {
		if c.Settings == nil {
			c.Settings = map[string]map[string]any{}
		}
		if c.Settings[ID] == nil {
			c.Settings[ID] = map[string]any{}
		}
		s := c.Settings[ID]
		s["mail_signing"], s["on_expired"], s["personal"], s["seal_evidence"] = in.MailSigning, in.OnExpired, in.Personal, in.SealEvidence
		return nil
	})
	if err != nil {
		return Settings{}, err
	}
	return p.settings(), audit.Log(ctx, p.host.DB, "signature.smime.settings", "settings", 0, before, in)
}

// Me is what the profile shows.
type Me struct {
	Personal bool     `json:"personal"` // personal certificates allowed
	Emails   []string `json:"emails"`   // confirmed addresses of the account
	Certs    []Cert   `json:"certs"`
}

func (p *Plugin) routes() {
	host := p.host
	// action: signature.smime.settings.get
	host.Handle("GET", "/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := perm.Require(r.Context(), permManage); err != nil {
			return nil, err
		}
		return p.settings(), nil
	})
	// action: signature.smime.settings.set
	host.Handle("PUT", "/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in Settings
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return p.saveSettings(r.Context(), in)
	})
	// action: signature.smime.cert.list (scope=org|all: administrators; scope=mine: own)
	host.Handle("GET", "/certs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		pr := identity.From(ctx)
		if pr == nil {
			return nil, apperr.ErrUnauthorized
		}
		scope := r.URL.Query().Get("scope")
		switch scope {
		case "", "org", "all":
			if err := perm.Require(ctx, permManage); err != nil {
				return nil, err
			}
			if scope == "" {
				scope = "org"
			}
		case "mine":
			if pr.UserID <= 0 {
				return nil, apperr.ErrUnauthorized
			}
		default:
			return nil, apperr.BadRequest("scope: org, all or mine")
		}
		return p.list(ctx, scope, pr.UserID)
	})
	// action: signature.smime.cert.add
	host.Handle("POST", "/certs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in Upload
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		c, err := p.upload(r.Context(), in)
		in.Password, in.File = "", ""
		if err != nil {
			return nil, err
		}
		return h.Created{V: c}, nil
	})
	// action: signature.smime.cert.get
	host.Handle("GET", "/certs/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		c, err := p.get(r.Context(), id)
		if err != nil {
			return nil, err
		}
		return c, p.mayChange(r.Context(), c)
	})
	// action: signature.smime.cert.change
	host.Handle("PATCH", "/certs/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var in Change
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return p.change(r.Context(), id, in)
	})
	// action: signature.smime.cert.delete
	host.Handle("DELETE", "/certs/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return h.NoContent{}, p.remove(r.Context(), id)
	})
	// action: signature.smime.me
	host.Handle("GET", "/me", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		pr := identity.From(ctx)
		if pr == nil || pr.UserID == 0 {
			return nil, apperr.ErrUnauthorized
		}
		me := Me{Personal: p.settings().Personal, Emails: []string{}}
		rows, err := host.DB.Query(ctx, `SELECT email FROM user_emails WHERE user_id=$1 AND verified ORDER BY is_primary DESC, email`, pr.UserID)
		if err != nil {
			return nil, err
		}
		if me.Emails, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return nil, err
		}
		if me.Certs, err = p.list(ctx, "mine", pr.UserID); err != nil {
			return nil, err
		}
		return me, nil
	})
}
