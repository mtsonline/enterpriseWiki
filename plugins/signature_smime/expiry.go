package signature_smime

import (
	"context"
	"math"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mtsonline/enterprisewiki/internal/notify"
)

// warnDays are the days before the end of a certificate with a warning.
var warnDays = []int{60, 30, 14, 7, 1}

// threshold returns the smallest warning threshold that daysLeft has
// reached (0: none yet).
func threshold(daysLeft int) int {
	t := 0
	for _, d := range warnDays {
		if daysLeft <= d {
			t = d
		}
	}
	return t
}

type expiring struct {
	id       int64
	userID   *int64
	label    string
	emails   []string
	notAfter string
	daysLeft int
	warned   *int
	expired  bool
}

// checkExpiry warns before certificates expire (60, 30, 14, 7 and 1 day
// before) and once when they have expired: administrators for the
// certificates of the organisation, the owner for personal ones, in the
// wiki and by e-mail. Certificates a newer one replaces (same owner, all
// its addresses, valid longer) are skipped.
func (p *Plugin) checkExpiry(ctx context.Context) error {
	now := p.now()
	rows, err := p.host.DB.Query(ctx, `SELECT c.id, c.user_id, c.label, c.emails, to_char(c.not_after AT TIME ZONE 'UTC', 'YYYY-MM-DD'),
			c.not_after, c.warned_days
		FROM plugin_smime_certs c
		WHERE (c.sign_mail OR c.seal) AND c.not_after < $1::timestamptz + interval '61 days' AND NOT c.expired_noted
		AND NOT EXISTS (SELECT 1 FROM plugin_smime_certs n WHERE n.id <> c.id AND n.user_id IS NOT DISTINCT FROM c.user_id
			AND n.emails @> c.emails AND n.not_after > c.not_after AND n.not_after > $1::timestamptz + interval '61 days' AND (n.sign_mail OR n.seal))
		ORDER BY c.id`, now)
	if err != nil {
		return err
	}
	var list []expiring
	for rows.Next() {
		var e expiring
		notAfter := now
		if err := rows.Scan(&e.id, &e.userID, &e.label, &e.emails, &e.notAfter, &notAfter, &e.warned); err != nil {
			rows.Close()
			return err
		}
		e.daysLeft = int(math.Ceil(notAfter.Sub(now).Hours() / 24))
		e.expired = !now.Before(notAfter)
		list = append(list, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, e := range list {
		if err := p.warn(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

func (p *Plugin) warn(ctx context.Context, e expiring) error {
	t := threshold(e.daysLeft)
	switch {
	case e.expired:
	case t == 0 || (e.warned != nil && *e.warned <= t):
		return nil // nothing new
	}
	return pgx.BeginFunc(ctx, p.host.DB, func(tx pgx.Tx) error {
		// claim the warning first (several nodes or overlapping runs)
		var (
			tag pgconn.CommandTag
			err error
		)
		if e.expired {
			tag, err = tx.Exec(ctx, `UPDATE plugin_smime_certs SET expired_noted=true WHERE id=$1 AND NOT expired_noted`, e.id)
		} else {
			tag, err = tx.Exec(ctx, `UPDATE plugin_smime_certs SET warned_days=$2 WHERE id=$1 AND (warned_days IS NULL OR warned_days > $2)`, e.id, t)
		}
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		var to []int64
		link := "/profile#smime"
		if e.userID != nil {
			to = []int64{*e.userID}
		} else {
			link = "/admin/smime"
			rows, err := tx.Query(ctx, `SELECT DISTINCT u.id FROM users u
				WHERE NOT u.disabled AND (
					EXISTS (SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = u.id AND r.is_admin)
					OR EXISTS (SELECT 1 FROM user_groups ug JOIN group_roles gr ON gr.group_id = ug.group_id JOIN roles r ON r.id = gr.role_id
						WHERE ug.user_id = u.id AND r.is_admin))`)
			if err != nil {
				return err
			}
			if to, err = pgx.CollectRows(rows, pgx.RowTo[int64]); err != nil {
				return err
			}
		}
		params := map[string]string{"emails": strings.Join(e.emails, ", "), "date": e.notAfter, "days": strconv.Itoa(e.daysLeft), "label": e.label}
		key := "signature.smime.expiry_soon"
		switch {
		case e.expired:
			key = "signature.smime.expired"
		case e.daysLeft <= 1:
			key = "signature.smime.expiry_tomorrow"
		}
		on := "unsigned"
		if p.settings().OnExpired == "hold" {
			on = "hold"
		}
		_, err = p.host.Notify.SendQ(ctx, tx, to, notify.Message{Kind: ID + ".expiry", EntityType: "smime_cert", EntityID: e.id, Link: link,
			Text: func(lang string) (string, string) {
				body := p.host.T(ctx, lang, key+"_body", params)
				if e.userID == nil {
					body += "\n\n" + p.host.T(ctx, lang, "signature.smime.expiry_after_"+on, params)
				}
				return p.host.T(ctx, lang, key+"_title", params), body
			}})
		return err
	})
}
