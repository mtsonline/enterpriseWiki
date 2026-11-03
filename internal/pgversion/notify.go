package pgversion

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/mail"
)

// Notifier writes the version status to the log and e-mails administrators
// once per notice (worker "pgversion", daily).
type Notifier struct {
	Pool *pgxpool.Pool
	Cfg  *config.Store
	Mail *mail.Service
	Log  *slog.Logger
	T    func(ctx context.Context, lang, key string, params map[string]string) string
}

// Params are the placeholders of the texts.
func Params(st Status) map[string]string {
	cmd := "./docker-postgres-upgrade.sh"
	if !st.Docker {
		cmd = "pg_upgrade"
	}
	return map[string]string{"version": st.Version, "major": fmt.Sprint(st.Major), "recommended": fmt.Sprint(st.Recommended),
		"eol": st.EOL, "days": fmt.Sprint(st.DaysLeft), "command": cmd}
}

// LogStatus writes a warning when the version is not current.
func (n *Notifier) LogStatus(ctx context.Context) {
	st, err := Check(ctx, n.Pool, time.Now())
	if err != nil {
		n.Log.Warn("could not determine the PostgreSQL version", "err", err)
		return
	}
	attrs := []any{"version", st.Version, "recommended", st.Recommended, "end_of_support", st.EOL}
	switch st.Level {
	case "error":
		n.Log.Error("PostgreSQL "+fmt.Sprint(st.Major)+" is no longer supported by the PostgreSQL project – please upgrade", attrs...)
	case "warning":
		n.Log.Warn(fmt.Sprintf("support of PostgreSQL %d ends in %d days – please upgrade", st.Major, st.DaysLeft), attrs...)
	case "info":
		n.Log.Info(fmt.Sprintf("PostgreSQL %d is recommended (running %s)", st.Recommended, st.Version), attrs...)
	}
}

// Run checks the version and sends due notices (worker).
func (n *Notifier) Run(ctx context.Context) error {
	st, err := Check(ctx, n.Pool, time.Now())
	if err != nil {
		return err
	}
	keys := Notices(st)
	if len(keys) == 0 || !n.Mail.Enabled() {
		return nil
	}
	return pgx.BeginFunc(ctx, n.Pool, func(tx pgx.Tx) error {
		var send []string
		for _, k := range keys {
			tag, err := tx.Exec(ctx, `INSERT INTO system_notices(key) VALUES ($1) ON CONFLICT DO NOTHING`, k)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 1 {
				send = append(send, k)
			}
		}
		if len(send) == 0 {
			return nil
		}
		// only the most urgent notice of this run (e.g. not "180 days" and "30 days" at once)
		key := send[len(send)-1]
		kind := "recommended"
		if strings.HasPrefix(key, "pg.eol.") {
			kind = "eol"
			if st.Level == "error" {
				kind = "ended"
			}
		}
		rows, err := tx.Query(ctx, `SELECT DISTINCT u.id FROM users u
			WHERE NOT u.disabled AND u.email <> '' AND (
				EXISTS (SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = u.id AND r.is_admin)
				OR EXISTS (SELECT 1 FROM user_groups ug JOIN group_roles gr ON gr.group_id = ug.group_id JOIN roles r ON r.id = gr.role_id WHERE ug.user_id = u.id AND r.is_admin))`)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		users, err := n.Mail.Users(ctx, tx, ids)
		if err != nil {
			return err
		}
		c := n.Cfg.Get()
		base := strings.TrimSuffix(c.Server.BaseURL, "/")
		for _, u := range users {
			p := Params(st)
			p["name"], p["site"], p["url"] = u.Name, c.UI.SiteName, base+"/admin/settings"
			t := func(k string) string { return n.T(ctx, u.Lang, "pgversion.mail_"+kind+"_"+k, p) }
			body := t("body") + "\n\n" + n.T(ctx, u.Lang, "pgversion.mail_howto", p) + "\n\n" + p["url"] + "\n"
			if _, err := n.Mail.Enqueue(ctx, tx, mail.Message{To: []string{u.Email}, Subject: t("subject"), Text: body,
				Source: "system", Ref: key, Owner: -1}); err != nil {
				return err
			}
		}
		n.Log.Info("PostgreSQL version notice sent", "notice", key, "admins", len(users))
		return nil
	})
}
