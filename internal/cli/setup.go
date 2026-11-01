package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/app"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/content"
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
			fmt.Fprintf(e.stdout, "%s [%s]: ", prompt, def)
		} else {
			fmt.Fprintf(e.stdout, "%s: ", prompt)
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

	fmt.Fprintln(e.stdout, "Enterprise Wiki setup")
	fmt.Fprintln(e.stdout, "=====================")
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
		if err := config.Validate(c); err != nil {
			return err
		}
		// secrets go to .env, the config references them
		envPath := filepath.Join(dir, ".env")
		if !dbFromEnv {
			f, err := os.OpenFile(envPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			fmt.Fprintf(f, "# Secrets for Enterprise Wiki (referenced from config.yaml). Keep this file private.\nEWIKI_DATABASE_URL=%s\n", dbURL)
			f.Close()
			os.Setenv("EWIKI_DATABASE_URL", dbURL)
			fmt.Fprintf(e.stdout, "wrote %s (mode 0600)\n", envPath)
		}
		if err := config.WriteFile(e.configPath, c); err != nil {
			return err
		}
		fmt.Fprintf(e.stdout, "wrote %s\n", e.configPath)
	} else {
		fmt.Fprintf(e.stdout, "using existing %s\n", e.configPath)
	}

	return e.withApp(true, func(ctx context.Context, a *app.App) error {
		fmt.Fprintln(e.stdout, "database migrations applied")
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
					fmt.Fprintln(e.stdout, perr)
					continue
				}
				break
			}
			u, err := a.Auth.CreateLocalUserUnchecked(ctx, auth.NewUser{Username: name, Password: pw, DisplayName: "Administrator", Roles: []int64{adminRole}})
			if err != nil {
				return err
			}
			fmt.Fprintf(e.stdout, "created administrator %s\n", u.Handle)
		} else {
			fmt.Fprintln(e.stdout, "an administrator already exists")
		}
		var cats int
		if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM categories`).Scan(&cats); err != nil {
			return err
		}
		if cats == 0 {
			if err := e.seed(ctx, a, editors); err != nil {
				return err
			}
			fmt.Fprintln(e.stdout, "created the welcome space")
		}
		fmt.Fprintln(e.stdout, "\nSetup complete. Start the server with: ewiki serve")
		return nil
	})
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func (e *env) seed(ctx context.Context, a *app.App, editors int64) error {
	c, err := a.Content.CreateCategory(ctx, content.CategoryInput{
		Titles:       map[string]string{"en": "Welcome", "de": "Willkommen"},
		Descriptions: map[string]string{"en": "Start here", "de": "Hier geht es los"},
		Tags:         []string{"start"},
	})
	if err != nil {
		return err
	}
	inherit := true
	if _, err := a.Content.AddRule(ctx, c.ID, content.RuleInput{SubjectType: "authenticated", Permissions: acl.View, Inherit: &inherit}); err != nil {
		return err
	}
	if _, err := a.Content.AddRule(ctx, c.ID, content.RuleInput{SubjectType: "role", SubjectID: &editors, Permissions: acl.View | acl.Edit | acl.Create | acl.Delete, Inherit: &inherit}); err != nil {
		return err
	}
	def := a.Config.Get().I18n.DefaultLanguage
	texts := map[string][2]string{
		"en": {"Welcome to Enterprise Wiki", `<h2>Welcome!</h2><p>This is your new knowledge base. Content is organised in <strong>categories</strong> (with unlimited subcategories) that contain <strong>elements</strong> such as pages.</p><ul><li>Use <em>Edit</em> to switch on the edit mode (requires permission).</li><li>Every change is versioned and can be restored.</li><li>Pages can be translated into every configured language.</li></ul>`},
		"de": {"Willkommen im Enterprise Wiki", `<h2>Willkommen!</h2><p>Das ist Ihre neue Wissensdatenbank. Inhalte sind in <strong>Kategorien</strong> (mit beliebig vielen Unterkategorien) organisiert, die <strong>Elemente</strong> wie Seiten enthalten.</p><ul><li>Mit <em>Bearbeiten</em> schalten Sie den Bearbeitungsmodus ein (Berechtigung erforderlich).</li><li>Jede Änderung wird versioniert und kann wiederhergestellt werden.</li><li>Seiten können in jede konfigurierte Sprache übersetzt werden.</li></ul>`},
	}
	first, ok := texts[def]
	if !ok {
		first, def = texts["en"], "en"
		if !contains(a.Config.Get().I18n.Languages, "en") {
			def = a.Config.Get().I18n.DefaultLanguage
		}
	}
	body, _ := json.Marshal(map[string]string{"format": "html", "source": first[1]})
	el, err := a.Content.CreateElement(ctx, content.ElementInput{CategoryID: c.ID, Type: "core.html", Lang: def, Title: first[0], Body: body, Tags: []string{"welcome"}})
	if err != nil {
		return err
	}
	for lang, t := range texts {
		if lang == def || !contains(a.Config.Get().I18n.Languages, lang) {
			continue
		}
		b, _ := json.Marshal(map[string]string{"format": "html", "source": t[1]})
		if _, err := a.Content.SaveElement(ctx, el.ID, content.SaveInput{Lang: lang, Title: t[0], Body: b, Comment: "Initial translation"}); err != nil {
			return err
		}
	}
	return nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
