package app

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/content"
)

// SeedWelcome creates the welcome space of a new site (category with a page,
// visible for signed-in users, editable for the role editors) and makes the
// page the start page.
func (a *App) SeedWelcome(ctx context.Context, editors int64) error {
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
		if !slices.Contains(a.Config.Get().I18n.Languages, "en") {
			def = a.Config.Get().I18n.DefaultLanguage
		}
	}
	body, _ := json.Marshal(map[string]string{"format": "html", "source": first[1]})
	el, err := a.Content.CreateElement(ctx, content.ElementInput{CategoryID: c.ID, Type: "core.html", Lang: def, Title: first[0], Body: body, Tags: []string{"welcome"}})
	if err != nil {
		return err
	}
	for lang, t := range texts {
		if lang == def || !slices.Contains(a.Config.Get().I18n.Languages, lang) {
			continue
		}
		b, _ := json.Marshal(map[string]string{"format": "html", "source": t[1]})
		if _, err := a.Content.SaveElement(ctx, el.ID, content.SaveInput{Lang: lang, Title: t[0], Body: b, Comment: "Initial translation"}); err != nil {
			return err
		}
	}
	// the welcome page is the start page of new installations
	if a.Config.Get().UI.HomeElement == 0 && a.Config.Get().UI.HomeCategory == 0 {
		return a.Config.Update(func(c *config.Config) error { c.UI.HomeElement = el.ID; return nil })
	}
	return nil
}
