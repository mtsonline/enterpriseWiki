package server

import (
	"net/http"
	"reflect"
	"slices"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/media"
	"github.com/mtsonline/enterprisewiki/internal/readability"
)

// routesSettings registers the routes of settings of the site.
func (s *Server) routesSettings() {
	// action: core.settings.get
	s.handle("GET /api/v1/admin/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		c := s.app.Config.Raw()
		maskConfig(c)
		return map[string]any{"config": c, "path": s.app.Config.Path()}, nil
	})
	// action: core.settings.update
	s.handle("PUT /api/v1/admin/settings", s.updateSettings)
	// action: core.retention.run
	s.handle("POST /api/v1/admin/retention/run", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		res, err := s.app.Content.RunRetention(r.Context())
		if err != nil {
			return nil, err
		}
		files, err := s.app.Files.GC(r.Context())
		return map[string]any{"revisions": res.Revisions, "trash": res.Trash, "audit": res.Audit, "files": files}, err
	})
}

func (s *Server) auditSettings(r *http.Request, action, detail string) {
	_ = audit.Log(r.Context(), s.app.DB, action, "settings", 0, nil, map[string]string{"detail": detail})
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) (any, error) {
	if err := requireAdmin(r); err != nil {
		return nil, err
	}
	var in SettingsUpdate
	if err := h.Decode(r, &in); err != nil {
		return nil, err
	}
	before := s.app.Config.Raw()
	err := s.app.Config.Update(func(c *config.Config) error {
		if in.UI != nil {
			if len(in.UI.DashboardTiles) > 200 {
				return apperr.BadRequest("ui.dashboard_tiles: at most 200 tiles")
			}
			for _, b := range in.UI.DashboardTiles {
				if !content.ValidBlockID(b) {
					return apperr.BadRequest("ui.dashboard_tiles: %q is no block id", b)
				}
			}
			// the template of the site: an active one (an unchanged value may stay)
			if in.UI.Template != "" && in.UI.Template != c.UI.Template && in.UI.Template != "core.basic" && !s.app.Content.TemplateOK(r.Context(), in.UI.Template) {
				return apperr.BadRequest("ui.template: %q is not an active website template", in.UI.Template)
			}
			if len(in.UI.EditorTools) > 0 {
				if err := content.CheckEditorTools(in.UI.EditorTools); err != nil {
					return err
				}
			}
			if err := checkReadability(in.UI.Readability); err != nil {
				return err
			}
			if n := in.UI.ChoiceCheckboxes; n < 0 || n > config.ChoiceCheckboxesMax {
				return apperr.BadRequest("ui.choice_checkboxes must be 0 (default %d) to %d", config.ChoiceCheckboxesDefault, config.ChoiceCheckboxesMax)
			}
			c.UI = *in.UI
		}
		if in.I18n != nil {
			c.I18n = *in.I18n
		}
		if in.Auth != nil {
			if in.Auth.SessionIdle != "" && in.Auth.SessionIdle != "0" {
				if _, err := time.ParseDuration(in.Auth.SessionIdle); err != nil {
					return apperr.BadRequest("invalid duration %q (use e.g. 12h, 30m, or 0)", in.Auth.SessionIdle)
				}
			}
			for _, d := range []string{in.Auth.SessionTTL, in.Auth.APIKeyMaxTTL, in.Auth.SSHTokenTTL, in.Auth.LockoutDuration} {
				if _, err := time.ParseDuration(d); err != nil {
					return apperr.BadRequest("invalid duration %q (use e.g. 24h, 30m)", d)
				}
			}
			c.Auth = *in.Auth
		}
		if in.Retention != nil {
			c.Retention = *in.Retention
		}
		if in.Media != nil {
			if v := in.Media.Viewer; v != "" && v != media.Internal && s.app.Media.Opener(v) == nil {
				return apperr.BadRequest("unknown or inactive program %q", v)
			}
			c.Media = *in.Media
		}
		if in.Lists != nil {
			if in.Lists.MaxItems < 0 || in.Lists.MaxItems > 10000 {
				return apperr.BadRequest("lists.max_items: 0 (default 1000) or 50 to 10000")
			}
			c.Lists = *in.Lists
		}
		if in.People != nil {
			if v := in.People.EmailVisibility; v != "" && !slices.Contains(config.EmailLevels, v) {
				return apperr.BadRequest("people.email_visibility must be users, permission or nobody")
			}
			if v := in.People.MentionDisplay; v != "" && !slices.Contains(config.MentionLevels, v) {
				return apperr.BadRequest("people.mention_display must be masked or name")
			}
			c.People = *in.People
		}
		if in.Macros != nil && !reflect.DeepEqual(*in.Macros, c.Plugins.Macros) {
			return apperr.BadRequest("macro security settings can only be changed in config.yaml")
		}
		if in.Catalog != nil {
			c.Plugins.Catalog = in.Catalog
		}
		if in.Mail != nil {
			// the stored password only for the same server and protection (else enter it again)
			if in.Mail.SMTP.Password == secretMask {
				n, o := in.Mail.SMTP, c.Mail.SMTP
				if n.Host != o.Host || n.Port != o.Port || n.Security != o.Security || (n.SkipVerify && !o.SkipVerify) {
					return errSecretAgain("mail.smtp.password")
				}
				in.Mail.SMTP.Password = c.Mail.SMTP.Password
			}
			if in.Mail.Bounce.Password == secretMask {
				n, o := in.Mail.Bounce, c.Mail.Bounce
				if n.IMAPHost != o.IMAPHost || n.IMAPPort != o.IMAPPort || n.Security != o.Security || (n.SkipVerify && !o.SkipVerify) {
					return errSecretAgain("mail.bounce.password")
				}
				in.Mail.Bounce.Password = c.Mail.Bounce.Password
			}
			c.Mail = *in.Mail
		}
		for id, m := range in.PluginSettings {
			if c.Settings == nil {
				c.Settings = map[string]map[string]any{}
			}
			if err := checkInstances(m); err != nil {
				return err
			}
			// keep stored secrets the UI only saw masked
			if err := keepMasked(m, c.Settings[id], id); err != nil {
				return err
			}
			c.Settings[id] = m
		}
		return nil
	})
	if err != nil {
		var ae *apperr.Error
		if !asAppErr(err, &ae) {
			return nil, apperr.BadRequest("%s", err.Error())
		}
		return nil, err
	}
	maskConfig(before)
	c := s.app.Config.Raw()
	maskConfig(c)
	_ = audit.Log(r.Context(), s.app.DB, "settings.update", "settings", 0, before, c)
	return map[string]any{"config": c, "path": s.app.Config.Path()}, nil
}

// checkReadability checks the settings of the readability hint (the same
// rules as config.ValidateReadability, with messages in the language of
// the user).
func checkReadability(r config.Readability) error {
	if r.Mode != "" && !slices.Contains(readability.Modes, r.Mode) {
		return apperr.BadRequest("ui.readability.mode must be off, offered or on")
	}
	if n := r.SentenceWords; n != 0 && (n < readability.MinSentenceWords || n > readability.MaxSentenceWords) {
		return apperr.BadRequest("ui.readability.sentence_words must be 0 (default %d) or %d to %d", readability.DefaultSentenceWords, readability.MinSentenceWords, readability.MaxSentenceWords)
	}
	if n := r.WordLetters; n != 0 && (n < readability.MinWordLetters || n > readability.MaxWordLetters) {
		return apperr.BadRequest("ui.readability.word_letters must be 0 (default %d) or %d to %d", readability.DefaultWordLetters, readability.MinWordLetters, readability.MaxWordLetters)
	}
	return nil
}
