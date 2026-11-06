package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/app"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
)

// setup installs Enterprise Wiki: it writes config.yaml and .env (if they do
// not exist), applies migrations, creates default roles, the first local
// administrator and a welcome page. It is idempotent and safe to re-run.
func (e *env) setup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	nonInteractive := fs.Bool("non-interactive", false, "take all values from environment variables")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := filepath.Dir(e.configPath)
	_ = config.LoadDotEnv(filepath.Join(dir, ".env"))
	in := bufio.NewReader(e.stdin)
	ask := func(prompt, envKey, def string, secret bool) (string, error) {
		if v := os.Getenv(envKey); v != "" {
			return v, nil
		}
		if *nonInteractive {
			if def == "" {
				return "", fmt.Errorf("%s must be set in non-interactive mode", envKey)
			}
			return def, nil
		}
		if def != "" && !secret {
			e.printf("%s [%s]: ", prompt, def)
		} else {
			e.printf("%s: ", prompt)
		}
		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			if def != "" {
				return def, nil
			}
			return "", fmt.Errorf("no value for %s", prompt)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return def, nil
		}
		return line, nil
	}

	e.println("Enterprise Wiki setup")
	e.println("=====================")
	if _, err := os.Stat(e.configPath); errors.Is(err, os.ErrNotExist) {
		dbFromEnv := os.Getenv("EWIKI_DATABASE_URL") != ""
		dbURL, err := ask("PostgreSQL URL (postgres://user:pass@host:5432/db)", "EWIKI_DATABASE_URL", "", false)
		if err != nil {
			return err
		}
		siteName, _ := ask("Site name", "EWIKI_SITE_NAME", "Enterprise Wiki", false)
		listen, _ := ask("Listen address", "EWIKI_LISTEN", ":8080", false)
		baseURL, _ := ask("Public base URL", "EWIKI_BASE_URL", "http://localhost:8080", false)
		langs, _ := ask("Languages (comma separated, first = default)", "EWIKI_LANGUAGES", "en,de", false)
		c := config.Defaults()
		c.UI.SiteName, c.Server.Listen, c.Server.BaseURL = siteName, listen, baseURL
		c.Server.SecureCookies = strings.HasPrefix(baseURL, "https://")
		c.I18n.Languages = nil
		for _, l := range strings.Split(langs, ",") {
			if l = strings.TrimSpace(l); l != "" {
				c.I18n.Languages = append(c.I18n.Languages, l)
			}
		}
		if len(c.I18n.Languages) > 0 {
			c.I18n.DefaultLanguage = c.I18n.Languages[0]
		}
		if d := os.Getenv("EWIKI_DEFAULT_LANGUAGE"); d != "" {
			c.I18n.DefaultLanguage = d
		}
		c.Plugins.Disabled = []string{}
		c.Plugins.Catalog = []config.CatalogEntry{}
		// outgoing e-mail (optional, e.g. for Docker): the password stays in the environment / .env
		c.Mail.From = os.Getenv("EWIKI_MAIL_FROM")
		if h := os.Getenv("EWIKI_SMTP_HOST"); h != "" {
			c.Mail.SMTP.Host = h
			c.Mail.SMTP.Port, _ = strconv.Atoi(os.Getenv("EWIKI_SMTP_PORT"))
			if sec := os.Getenv("EWIKI_SMTP_SECURITY"); sec != "" {
				c.Mail.SMTP.Security = sec
			}
			c.Mail.SMTP.Username = os.Getenv("EWIKI_SMTP_USER")
			if os.Getenv("EWIKI_SMTP_PASSWORD") != "" {
				c.Mail.SMTP.Password = "${EWIKI_SMTP_PASSWORD}"
			}
		}
		if err := config.Validate(c); err != nil {
			return err
		}
		// secrets go to .env, the config references them
		envPath := filepath.Join(dir, ".env")
		if !dbFromEnv {
			f, err := os.OpenFile(envPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- .env of the installation
			if err != nil {
				return err
			}
			_, werr := fmt.Fprintf(f, "# Secrets for Enterprise Wiki (referenced from config.yaml). Keep this file private.\nEWIKI_DATABASE_URL=%s\n", dbURL)
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				return fmt.Errorf("write %s: %w", envPath, werr)
			}
			if err := os.Setenv("EWIKI_DATABASE_URL", dbURL); err != nil {
				return err
			}
			e.printf("wrote %s (mode 0600)\n", envPath)
		}
		if err := config.WriteFile(e.configPath, c); err != nil {
			return err
		}
		e.printf("wrote %s\n", e.configPath)
	} else {
		e.printf("using existing %s\n", e.configPath)
	}

	return e.withApp(true, func(ctx context.Context, a *app.App) error {
		e.println("database migrations applied")
		adminRole, err := a.Auth.EnsureRole(ctx, "Administrators", "Full access to everything", true)
		if err != nil {
			return err
		}
		editors, err := a.Auth.EnsureRole(ctx, "Editors", "Create and edit content where granted", false)
		if err != nil {
			return err
		}
		if _, err := a.Auth.EnsureRole(ctx, "Readers", "Read content where granted", false); err != nil {
			return err
		}
		var admins int
		if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM user_roles ur JOIN roles r ON r.id=ur.role_id JOIN users u ON u.id=ur.user_id
			WHERE r.is_admin AND NOT u.disabled`).Scan(&admins); err != nil {
			return err
		}
		if admins == 0 {
			name, err := ask("Administrator username", "EWIKI_ADMIN_USER", "admin", false)
			if err != nil {
				return err
			}
			var pw string
			for {
				pw, err = ask("Administrator password (min. 12 characters, 3 character classes)", "EWIKI_ADMIN_PASSWORD", "", true)
				if err != nil {
					return err
				}
				if perr := auth.CheckPasswordPolicy(pw, a.Config.Get().Auth.MinPasswordLen); perr != nil {
					if *nonInteractive || os.Getenv("EWIKI_ADMIN_PASSWORD") != "" {
						return perr
					}
					e.println(perr)
					continue
				}
				break
			}
			u, err := a.Auth.CreateLocalUserUnchecked(ctx, auth.NewUser{Username: name, Password: pw, DisplayName: "Administrator", Roles: []int64{adminRole}})
			if err != nil {
				return err
			}
			e.printf("created administrator %s\n", u.Handle)
		} else {
			e.println("an administrator already exists")
		}
		var cats int
		if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM categories`).Scan(&cats); err != nil {
			return err
		}
		if cats == 0 {
			if err := a.SeedWelcome(ctx, editors); err != nil {
				return err
			}
			e.println("created the welcome space")
		}
		e.println("\nSetup complete. Start the server with: ewiki serve")
		return nil
	})
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
