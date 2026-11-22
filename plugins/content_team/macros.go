package content_team

// People blocks (plan of blocks, 3.2): the profile card of one person and
// the people of a group, in the text of any page. Unlike a team page they
// follow the rights of the reader: they show only people the reader could
// list (core.users.list_all, or the own groups with the list_own
// permissions); guests see none. A public list of people is a team page.

import (
	"context"
	"fmt"
	"html"
	"slices"
	"strconv"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

func lm(en, de string) map[string]string { return map[string]string{"en": en, "de": de} }

func tr(lang, en, de string) string {
	if strings.HasPrefix(lang, "de") {
		return de
	}
	return en
}

// peopleFields are the details the blocks show (the reader is signed in).
func peopleFields(v string) []string {
	if v == "short" {
		return []string{"photo", "job_title", "department"}
	}
	return []string{"photo", "job_title", "department", "email", "phone", "office"}
}

var peopleParams = []content.MacroParam{
	{Name: "details", Labels: lm("Details", "Angaben"), Type: "select", Options: []string{"contact", "short"}, Default: "contact",
		OptionLabels: map[string]map[string]string{"contact": lm("With contact details", "Mit Kontaktdaten"), "short": lm("Name, job title, department", "Name, Funktion, Abteilung")}},
}

func idParam(v string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// hidden: the note for people the reader may not see (the same for people
// that do not exist, so nothing is told about them).
func hidden(lang string, guest bool) string {
	if guest {
		return `<p class="ew-muted">` + html.EscapeString(tr(lang, "People are shown to signed-in people only.", "Personen sehen nur angemeldete Personen.")) + `</p>`
	}
	return `<p class="ew-muted">` + html.EscapeString(tr(lang, "You may not see these people.", "Diese Personen dürfen Sie nicht sehen.")) + `</p>`
}

// ---------------------------------------------------------------- profile card

type profileCard struct{ p *Plugin }

func (profileCard) Name() string              { return "profilecard" }
func (profileCard) Group() string             { return "media" }
func (profileCard) Labels() map[string]string { return lm("Profile card", "Profilkarte") }
func (profileCard) Description() map[string]string {
	return lm("A person of the wiki with photo, job title and contact details from their business card; shown only to people who may find this person.",
		"Eine Person des Wikis mit Foto, Funktion und Kontaktdaten aus ihrer Visitenkarte; nur für Personen, die diese Person finden dürfen.")
}
func (profileCard) HasBody() bool { return false }
func (profileCard) Params() []content.MacroParam {
	return append([]content.MacroParam{{Name: "user", Labels: lm("Person", "Person"), Type: "custom:user", Required: true}}, peopleParams...)
}

func (m profileCard) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	p := identity.From(ctx)
	if p == nil || p.ID() == 0 {
		return hidden(mc.Lang, true), nil
	}
	uid := idParam(mc.Params["user"])
	hs := m.p.host
	if uid == 0 || hs == nil {
		return hidden(mc.Lang, false), nil
	}
	ok := p.ID() == uid
	switch auth.ListScope(p, "users") {
	case auth.ListAll:
		ok = true
	case auth.ListOwn:
		if !ok {
			_ = hs.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_groups a JOIN user_groups b ON a.group_id = b.group_id WHERE a.user_id = $1 AND b.user_id = $2)`, uid, p.ID()).Scan(&ok)
		}
	}
	if !ok {
		return hidden(mc.Lang, false), nil
	}
	c := &Config{Fields: peopleFields(mc.Params["details"]), Layout: "cards", Labels: map[string]string{}}
	secs, err := people(ctx, hs, c, false, uid)
	if err != nil {
		return "", err
	}
	if len(secs[0].Members) == 0 {
		return hidden(mc.Lang, false), nil
	}
	return `<div class="ewt ewt--cards ewt--card">` + Render(secs, c, mc.Lang) + `</div>`, nil
}

// ---------------------------------------------------------------- people of a group

type groupPeople struct{ p *Plugin }

func (groupPeople) Name() string  { return "people" }
func (groupPeople) Group() string { return "media" }
func (groupPeople) Labels() map[string]string {
	return lm("People of a group", "Personen einer Gruppe")
}
func (groupPeople) Description() map[string]string {
	return lm("The members of a group with photo, job title and contact details; shown only to people who may list this group and its members.",
		"Die Mitglieder einer Gruppe mit Foto, Funktion und Kontaktdaten; nur für Personen, die die Gruppe und ihre Mitglieder auflisten dürfen.")
}
func (groupPeople) HasBody() bool { return false }
func (groupPeople) Params() []content.MacroParam {
	return append([]content.MacroParam{
		{Name: "group", Labels: lm("Group", "Gruppe"), Type: "custom:group", Required: true},
		{Name: "layout", Labels: lm("Display", "Darstellung"), Type: "select", Options: []string{"cards", "list", "table"}, Default: "cards",
			OptionLabels: map[string]map[string]string{"cards": lm("Cards", "Karten"), "list": lm("List", "Liste"), "table": lm("Table", "Tabelle")}},
	}, peopleParams...)
}

func (m groupPeople) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	p := identity.From(ctx)
	if p == nil || p.ID() == 0 {
		return hidden(mc.Lang, true), nil
	}
	gid := idParam(mc.Params["group"])
	hs := m.p.host
	groups, users := auth.ListScope(p, "groups"), auth.ListScope(p, "users")
	own := slices.Contains(p.Groups, gid)
	if gid == 0 || hs == nil || !((groups == auth.ListAll && users == auth.ListAll) || (own && groups != auth.ListNone && users != auth.ListNone)) {
		return hidden(mc.Lang, false), nil
	}
	layout := mc.Params["layout"]
	if layout != "list" && layout != "table" {
		layout = "cards"
	}
	c := &Config{Fields: peopleFields(mc.Params["details"]), Layout: layout, Labels: map[string]string{}}
	secs, err := people(ctx, hs, c, true, gid)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`<div class="ewt ewt--%s">`, layout) + Render(secs, c, mc.Lang) + `</div>`, nil
}
