// Package content_team provides the "Team" element type: members of one or
// more groups with the attributes chosen by the editor (profile fields of
// the user's business card, e-mail, groups or identity-provider attributes).
package content_team

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/limits"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"github.com/mtsonline/enterprisewiki/plugins/core_html"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// TypeID is the element type.
const TypeID = "content.team"

// Plugin is the team plugin.
type Plugin struct{ host *coreplugin.Host }

// plugins holds the plugin instance of each site (for renderers).
var plugins coreplugin.PerSite[*Plugin]

func init() { coreplugin.Register(&Plugin{}) }

// Manifest implements coreplugin.Plugin.
func (p *Plugin) Manifest() coreplugin.Manifest { return coreplugin.ParseManifest(manifest) }

// Locales implements coreplugin.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Fields that can be shown. "attr:<name>" shows an attribute delivered by
// the identity provider (LDAP, OIDC, SAML), e.g. attr:telephoneNumber.
var Fields = []string{"photo", "job_title", "department", "email", "phone", "mobile", "office", "about", "groups"}

var attrRe = regexp.MustCompile(`^attr:[A-Za-z0-9_.:-]{1,64}$`)

// Config is the body of a team element.
type Config struct {
	Description core_html.Body    `json:"description"`
	Groups      []int64           `json:"groups"`
	Fields      []string          `json:"fields"`
	Labels      map[string]string `json:"labels"`   // custom labels for attr: fields
	Layout      string            `json:"layout"`   // cards | list | table
	ByGroup     bool              `json:"by_group"` // one section per group
	Sort        string            `json:"sort"`     // name | job_title | department
	// Public: guests see the contact details too (only set with the
	// permission content.team.public); without it they see the name,
	// job title, department and the photo (as the person allows).
	Public bool `json:"public"`
}

// PermPublic lets guests see the contact details of a team page.
const PermPublic = "content.team.public"

// publicFields are shown to guests on every team page; the other fields
// (contact details, groups, attributes of the identity provider) only on
// pages that are public.
var publicFields = map[string]bool{"photo": true, "job_title": true, "department": true}

// forViewer: the config as the caller of ctx sees it (guests without the
// contact details unless the page is public).
func forViewer(ctx context.Context, c *Config) *Config {
	if c.Public || identity.From(ctx).ID() > 0 {
		return c
	}
	v := *c
	v.Fields = []string{}
	for _, f := range c.Fields {
		if !contact(f) {
			v.Fields = append(v.Fields, f)
		}
	}
	return &v
}

func sameSet[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		if !slices.Contains(b, x) {
			return false
		}
	}
	return true
}

// contact reports whether field f is a contact detail (not for guests
// unless the page is public).
func contact(f string) bool { return !publicFields[f] }

func decode(raw []byte) (*Config, error) {
	c := &Config{}
	if err := json.Unmarshal(raw, c); err != nil {
		return nil, apperr.BadRequest("invalid team settings: %v", err)
	}
	if c.Description.Format == "" {
		c.Description.Format = "markdown"
	}
	d, _ := json.Marshal(c.Description)
	clean, err := (&core_html.Type{}).Normalize(d)
	if err != nil {
		return nil, apperr.BadRequest("description: %v", err)
	}
	_ = json.Unmarshal(clean, &c.Description)
	if len(c.Groups) > 50 {
		return nil, apperr.BadRequest("at most 50 groups")
	}
	if c.Groups == nil {
		c.Groups = []int64{}
	}
	allowed := map[string]bool{}
	for _, f := range Fields {
		allowed[f] = true
	}
	seen := map[string]bool{}
	fields := []string{}
	for _, f := range c.Fields {
		if !(allowed[f] || attrRe.MatchString(f)) {
			return nil, apperr.BadRequest("unknown field %q", f)
		}
		if !seen[f] {
			fields = append(fields, f)
			seen[f] = true
		}
	}
	c.Fields = fields
	if c.Labels == nil {
		c.Labels = map[string]string{}
	}
	for k, v := range c.Labels {
		if !attrRe.MatchString(k) || len(v) > 60 {
			delete(c.Labels, k)
		}
	}
	if c.Layout != "list" && c.Layout != "table" {
		c.Layout = "cards"
	}
	if c.Sort != "job_title" && c.Sort != "department" {
		c.Sort = "name"
	}
	return c, nil
}

// Member is a person shown on a team page (only the chosen fields are set).
type Member struct {
	uid int64
	// ID is set only for viewers who may open the person (menu on the
	// name, person page); guests never get it.
	ID      *int64            `json:"id,omitempty"`
	Name    string            `json:"name"`
	Fields  map[string]string `json:"fields"`
	Groups  []string          `json:"groups,omitempty"`
	GroupID []int64           `json:"group_ids"`
}

// Section is a group with its members (or all members when not by group).
type Section struct {
	GroupID     int64    `json:"group_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Members     []Member `json:"members"`
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// members loads the members of the configured groups. Profile photos are
// only included if the viewer (ctx) may see them.
func members(ctx context.Context, hs *coreplugin.Host, c *Config) ([]Section, error) {
	q := hs.DB
	if len(c.Groups) == 0 {
		return []Section{}, nil
	}
	rows, err := q.Query(ctx, `SELECT g.id, g.name, g.description, u.id, CASE WHEN u.display_name = '' THEN u.username ELSE u.display_name END,
			u.email, u.profile, u.attributes,
			ARRAY(SELECT g2.name FROM user_groups ug2 JOIN groups g2 ON g2.id = ug2.group_id WHERE ug2.user_id = u.id ORDER BY g2.name)
		FROM groups g JOIN user_groups ug ON ug.group_id = g.id JOIN users u ON u.id = ug.user_id
		WHERE g.id = ANY($1) AND NOT u.disabled ORDER BY array_position($1, g.id), u.display_name, u.username`, c.Groups)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byGroup := map[int64]*Section{}
	order := []int64{}
	all := map[int64]*Member{}
	allOrder := []int64{}
	for rows.Next() {
		var gid, uid int64
		var gname, gdesc, name, email string
		var profile map[string]string
		var attrs map[string]any
		var groups []string
		if err := rows.Scan(&gid, &gname, &gdesc, &uid, &name, &email, &profile, &attrs, &groups); err != nil {
			return nil, err
		}
		m := Member{uid: uid, Name: name, Fields: map[string]string{}, GroupID: []int64{gid}}
		for _, f := range c.Fields {
			switch {
			case f == "email":
				m.Fields[f] = email
			case f == "groups":
				m.Groups = groups
			case strings.HasPrefix(f, "attr:"):
				m.Fields[f] = attrString(attrs[strings.TrimPrefix(f, "attr:")])
			case f == "photo":
				if hs.Auth.PhotoVisible(ctx, uid, profile["photo_visibility"]) {
					m.Fields[f] = profile[f]
				}
			default:
				m.Fields[f] = profile[f]
			}
		}
		for k, v := range m.Fields {
			if v == "" {
				delete(m.Fields, k)
			}
		}
		if _, ok := byGroup[gid]; !ok {
			byGroup[gid] = &Section{GroupID: gid, Name: gname, Description: gdesc}
			order = append(order, gid)
		}
		byGroup[gid].Members = append(byGroup[gid].Members, m)
		if x, ok := all[uid]; ok {
			x.GroupID = append(x.GroupID, gid)
		} else {
			mm := m
			all[uid] = &mm
			allOrder = append(allOrder, uid)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortMembers := func(ms []Member) {
		key := func(m Member) string {
			if c.Sort != "name" {
				return strings.ToLower(m.Fields[c.Sort]) + "\x00" + strings.ToLower(m.Name)
			}
			return strings.ToLower(m.Name)
		}
		sort.SliceStable(ms, func(a, b int) bool { return key(ms[a]) < key(ms[b]) })
	}
	if !c.ByGroup {
		s := Section{Members: []Member{}}
		for _, uid := range allOrder {
			s.Members = append(s.Members, *all[uid])
		}
		sortMembers(s.Members)
		return personIDs(ctx, hs, []Section{s})
	}
	out := []Section{}
	for _, gid := range order {
		sortMembers(byGroup[gid].Members)
		out = append(out, *byGroup[gid])
	}
	return personIDs(ctx, hs, out)
}

// personIDs sets the ids of the members the viewer may open (one query);
// the others keep none, so their names stay plain text.
func personIDs(ctx context.Context, hs *coreplugin.Host, secs []Section) ([]Section, error) {
	var ids []int64
	for _, s := range secs {
		for _, m := range s.Members {
			ids = append(ids, m.uid)
		}
	}
	ok, err := hs.Auth.Viewable(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, s := range secs {
		for j, m := range s.Members {
			if uid := m.uid; ok[uid] {
				s.Members[j].ID = &uid
			}
		}
	}
	return secs, nil
}

// people loads the active members of the group id (byGroup) or the user id
// as one section, with the fields of c, at most the limit content.team.people.
func people(ctx context.Context, hs *coreplugin.Host, c *Config, byGroup bool, id int64) ([]Section, error) {
	max := limits.Value(hs.Config.Get(), "content.team.people")
	rows, err := hs.DB.Query(ctx, `SELECT u.id, CASE WHEN u.display_name = '' THEN u.username ELSE u.display_name END, u.email, u.profile
		FROM users u WHERE NOT u.disabled AND CASE WHEN $3 THEN u.id IN (SELECT user_id FROM user_groups WHERE group_id = $1) ELSE u.id = $1 END
		ORDER BY lower(u.display_name), u.username LIMIT $2`, id, max, byGroup)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	s := Section{Members: []Member{}}
	for rows.Next() {
		var uid int64
		var name, email string
		var profile map[string]string
		if err := rows.Scan(&uid, &name, &email, &profile); err != nil {
			return nil, err
		}
		m := Member{uid: uid, Name: name, Fields: map[string]string{}}
		for _, f := range c.Fields {
			switch f {
			case "email":
				// people blocks follow the rule for e-mail addresses
				// (team pages do not: their editor decides)
				if hs.Auth.EmailVisible(ctx, uid, profile["email_hidden"] == "true") {
					m.Fields[f] = email
				}
			case "photo":
				if hs.Auth.PhotoVisible(ctx, uid, profile["photo_visibility"]) {
					m.Fields[f] = profile[f]
				}
			default:
				m.Fields[f] = profile[f]
			}
			if m.Fields[f] == "" {
				delete(m.Fields, f)
			}
		}
		s.Members = append(s.Members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return personIDs(ctx, hs, []Section{s})
}

func attrString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []any:
		var parts []string
		for _, y := range x {
			parts = append(parts, fmt.Sprint(y))
		}
		return strings.Join(parts, ", ")
	default:
		return fmt.Sprint(x)
	}
}

var labels = map[string]map[string]string{
	"de": {"job_title": "Funktion", "department": "Abteilung", "email": "E-Mail", "phone": "Telefon", "mobile": "Mobil", "office": "Büro", "about": "Über mich", "groups": "Gruppen", "empty": "Keine Mitglieder.", "nogroups": "Noch keine Gruppen gewählt.", "name": "Name", "photo": "Foto"},
	"en": {"job_title": "Job title", "department": "Department", "email": "E-mail", "phone": "Phone", "mobile": "Mobile", "office": "Office", "about": "About", "groups": "Groups", "empty": "No members.", "nogroups": "No groups chosen yet.", "name": "Name", "photo": "Photo"},
}

func label(lang, f string, c *Config) string {
	if v := c.Labels[f]; v != "" {
		return v
	}
	if strings.HasPrefix(f, "attr:") {
		return strings.TrimPrefix(f, "attr:")
	}
	m := labels["en"]
	if x, ok := labels[lang]; ok {
		m = x
	}
	return m[f]
}

func initials(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		r := []rune(w)
		if len(r) > 0 {
			out = append(out, r[0])
		}
		if len(out) == 2 {
			break
		}
	}
	return strings.ToUpper(string(out))
}

// value renders a field value (links for e-mail and phone numbers).
func value(f, v string) string {
	esc := html.EscapeString
	switch f {
	case "email":
		return `<a href="mailto:` + esc(v) + `">` + esc(v) + `</a>`
	case "phone", "mobile":
		tel := strings.Map(func(r rune) rune {
			if (r >= '0' && r <= '9') || r == '+' {
				return r
			}
			return -1
		}, v)
		return `<a href="tel:` + esc(tel) + `">` + esc(v) + `</a>`
	case "about":
		return core_html.ToHTML(&core_html.Body{Format: "markdown", Source: v})
	}
	return esc(v)
}

// Render renders the team (server-side; the widget adds a filter).
func Render(secs []Section, c *Config, lang string) string {
	esc := html.EscapeString
	var b strings.Builder
	l := func(f string) string { return label(lang, f, c) }
	photo := has(c.Fields, "photo")
	other := []string{}
	for _, f := range c.Fields {
		if f != "photo" {
			other = append(other, f)
		}
	}
	member := func(m Member) {
		switch c.Layout {
		case "table":
			b.WriteString(`<tr class="ewt-member">`)
			b.WriteString(`<th scope="row">`)
			if photo {
				b.WriteString(avatar(m, "ewt-photo ewt-photo--small"))
			}
			b.WriteString(personName(m) + `</th>`)
			for _, f := range other {
				b.WriteString(`<td>` + cell(m, f) + `</td>`)
			}
			b.WriteString(`</tr>`)
		default:
			fmt.Fprintf(&b, `<li class="ewt-member" data-name="%s">`, esc(strings.ToLower(m.Name)))
			if photo {
				b.WriteString(avatar(m, "ewt-photo"))
			}
			b.WriteString(`<div class="ewt-info"><h3 class="ewt-name">` + personName(m) + `</h3>`)
			if v := m.Fields["job_title"]; v != "" && has(c.Fields, "job_title") {
				b.WriteString(`<p class="ewt-title">` + esc(v) + `</p>`)
			}
			var dl strings.Builder
			for _, f := range other {
				if f == "job_title" {
					continue
				}
				if cv := cell(m, f); cv != "" {
					fmt.Fprintf(&dl, `<div class="ewt-field ewt-field--%s"><dt>%s</dt><dd>%s</dd></div>`, esc(strings.ReplaceAll(f, ":", "-")), esc(l(f)), cv)
				}
			}
			if dl.Len() > 0 {
				b.WriteString(`<dl class="ewt-fields">` + dl.String() + `</dl>`)
			}
			b.WriteString(`</div></li>`)
		}
	}
	section := func(s Section, level int) {
		if c.ByGroup && s.Name != "" {
			fmt.Fprintf(&b, `<section class="ewt-group"><h%d class="ewt-group-name">%s</h%d>`, level, esc(s.Name), level)
			if s.Description != "" {
				b.WriteString(`<p class="ewt-group-desc">` + esc(s.Description) + `</p>`)
			}
		}
		if len(s.Members) == 0 {
			b.WriteString(`<p class="ewt-empty">` + esc(labels[pickLang(lang)]["empty"]) + `</p>`)
		} else if c.Layout == "table" {
			b.WriteString(`<div class="ew-table-wrap"><table class="ew-table ewt-table"><thead><tr><th scope="col">` + esc(l("name")) + `</th>`)
			for _, f := range other {
				b.WriteString(`<th scope="col">` + esc(l(f)) + `</th>`)
			}
			b.WriteString(`</tr></thead><tbody>`)
			for _, m := range s.Members {
				member(m)
			}
			b.WriteString(`</tbody></table></div>`)
		} else {
			fmt.Fprintf(&b, `<ul class="ewt-members ewt-members--%s">`, esc(c.Layout))
			for _, m := range s.Members {
				member(m)
			}
			b.WriteString(`</ul>`)
		}
		if c.ByGroup && s.Name != "" {
			b.WriteString(`</section>`)
		}
	}
	if len(c.Groups) == 0 {
		b.WriteString(`<p class="ewt-empty">` + esc(labels[pickLang(lang)]["nogroups"]) + `</p>`)
	}
	for _, s := range secs {
		section(s, 2)
	}
	return b.String()
}

// personName is the name of a member; for viewers who may open the
// person it carries the id, and the team widget turns it into the menu on
// names (the same as everywhere else).
func personName(m Member) string {
	if m.ID == nil {
		return html.EscapeString(m.Name)
	}
	return fmt.Sprintf(`<span class="ewt-person" data-person="%d">%s</span>`, *m.ID, html.EscapeString(m.Name))
}

func pickLang(lang string) string {
	if _, ok := labels[lang]; ok {
		return lang
	}
	return "en"
}

func cell(m Member, f string) string {
	if f == "groups" {
		if len(m.Groups) == 0 {
			return ""
		}
		return html.EscapeString(strings.Join(m.Groups, ", "))
	}
	v := m.Fields[f]
	if v == "" {
		return ""
	}
	return value(f, v)
}

func avatar(m Member, class string) string {
	if p := m.Fields["photo"]; p != "" {
		return fmt.Sprintf(`<img class="%s" src="/api/v1/files/%s" alt="" loading="lazy">`, class, html.EscapeString(p))
	}
	return fmt.Sprintf(`<span class="%s ewt-initials" aria-hidden="true">%s</span>`, class, html.EscapeString(initials(m.Name)))
}

// Type implements content.ElementType.
type Type struct{}

// TypeID implements content.ElementType.
func (Type) TypeID() string { return TypeID }

// Labels implements content.ElementType.
func (Type) Labels() map[string]string { return map[string]string{"en": "Team", "de": "Team"} }

// EmptyBody implements content.ElementType.
func (Type) EmptyBody() json.RawMessage {
	return json.RawMessage(`{"description":{"format":"markdown","source":""},"groups":[],"fields":["photo","job_title","department","email","phone"],"layout":"cards","by_group":false,"sort":"name","labels":{}}`)
}

// Normalize implements content.ElementType.
func (Type) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	c, err := decode(raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

// Text implements content.ElementType (the description is searchable).
func (Type) Text(raw json.RawMessage) string {
	var c Config
	_ = json.Unmarshal(raw, &c)
	return c.Description.Source
}

// CheckCreate implements content.Guard (the groups are checked with the body).
func (Type) CheckCreate(context.Context, *content.Service, int64) error { return nil }

// CheckChange implements content.Guard: a team page shows the members of
// its groups to everybody who may see the page. A group is added only by
// someone who may list it and its members (the own groups with
// core.groups.list_own and core.users.list_own, every group with
// core.groups.list_all and core.users.list_all); groups that were there
// stay for every editor.
func (Type) CheckChange(ctx context.Context, _ *content.Service, _ *content.Element, oldBody, newBody json.RawMessage) error {
	if newBody == nil {
		return nil
	}
	var before, after Config
	if len(oldBody) > 0 {
		_ = json.Unmarshal(oldBody, &before)
	}
	if err := json.Unmarshal(newBody, &after); err != nil {
		return apperr.BadRequest("invalid team settings: %v", err)
	}
	p := identity.From(ctx)
	// public: contact details for guests need the permission, also to keep
	// them public while the groups or the fields change
	if after.Public && !perm.HasP(p, PermPublic) {
		if !before.Public || !sameSet(before.Groups, after.Groups) || !sameSet(before.Fields, after.Fields) {
			return apperr.Forbidden("showing contact details to guests needs the permission %q", "Show contact details publicly")
		}
	}
	// attributes of the identity provider: only seen by the administration
	// of users, so only chosen by it
	for _, f := range after.Fields {
		if strings.HasPrefix(f, "attr:") && !slices.Contains(before.Fields, f) && !perm.HasP(p, "core.users.manage") {
			return apperr.Forbidden("field %s: attributes of the identity provider are chosen by who manages users (core.users.manage)", f)
		}
	}
	groups, users := auth.ListScope(p, "groups"), auth.ListScope(p, "users")
	for _, g := range after.Groups {
		if slices.Contains(before.Groups, g) {
			continue
		}
		own := p != nil && slices.Contains(p.Groups, g)
		if !(groups == auth.ListAll && users == auth.ListAll) && !(own && groups != auth.ListNone && users != auth.ListNone) {
			return apperr.Forbidden("group %d: showing the members of a group needs the permissions to list it and its members (core.groups.list_all and core.users.list_all, or the own groups)", g)
		}
	}
	return nil
}

// Render implements content.ElementType.
func (Type) Render(ctx context.Context, rc content.RenderContext, raw json.RawMessage) (string, error) {
	c, err := decode(raw)
	if err != nil {
		return "", err
	}
	c = forViewer(ctx, c)
	secs, err := members(ctx, plugins.Get(ctx).host, c)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="ewt ewt--%s" data-ew-widget="team" data-element="%d">`, c.Layout, rc.ElementID)
	if strings.TrimSpace(c.Description.Source) != "" {
		b.WriteString(`<div class="ewt-desc">` + core_html.ToHTML(&c.Description) + `</div>`)
	}
	b.WriteString(`<div class="ewt-body">` + Render(secs, c, rc.Lang) + `</div></div>`)
	return b.String(), nil
}

// Setup registers the element type and the API.
func (p *Plugin) Setup(hs *coreplugin.Host) error {
	p.host = hs
	plugins.Set(hs, p)
	hs.RegisterElementType(Type{})
	// people blocks: read with the rights of the reader, nothing else
	hs.RegisterPictureMacro(profileCard{p})
	hs.RegisterPictureMacro(groupPeople{p})

	// GET /groups – groups to choose from (signed-in users; editors are not always admins)
	// action: content.team.groups
	hs.Handle("GET", "/groups", func(w http.ResponseWriter, r *http.Request) (any, error) {
		me := identity.From(r.Context())
		if me.ID() == 0 && !me.Admin() {
			return nil, apperr.ErrUnauthorized
		}
		// the groups the caller may list (all or the own ones)
		scope := auth.ListScope(me, "groups")
		if scope == auth.ListNone {
			return nil, apperr.Forbidden("listing groups needs the permission core.groups.list_all or core.groups.list_own")
		}
		var own []int64
		if me != nil {
			own = me.Groups
		}
		rows, err := hs.DB.Query(r.Context(), `SELECT g.id, g.name, g.description, (SELECT count(*) FROM user_groups ug JOIN users u ON u.id = ug.user_id
			WHERE ug.group_id = g.id AND NOT u.disabled) FROM groups g WHERE $1 OR g.id = ANY($2) ORDER BY g.name`, scope == auth.ListAll, own)
		if err != nil {
			return nil, err
		}
		type grp struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Members     int    `json:"members"`
		}
		return pgx.CollectRows(rows, func(r pgx.CollectableRow) (grp, error) {
			var g grp
			return g, r.Scan(&g.ID, &g.Name, &g.Description, &g.Members)
		})
	})
	// GET /fields – fields and the identity-provider attributes in use
	// action: content.team.fields
	hs.Handle("GET", "/fields", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if identity.From(r.Context()).ID() == 0 {
			return nil, apperr.ErrUnauthorized
		}
		var attrs []string
		rows, err := hs.DB.Query(r.Context(), `SELECT DISTINCT jsonb_object_keys(attributes) k FROM users WHERE NOT disabled ORDER BY k LIMIT 200`)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err == nil {
				attrs = append(attrs, k)
			}
		}
		rows.Close()
		return map[string]any{"fields": Fields, "attributes": attrs}, nil
	})
	// GET /elements/{id}/members – the members as shown on the page (JSON)
	// action: content.team.members
	hs.Handle("GET", "/elements/{id}/members", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		typ, body, _, _, err := hs.Content.ElementBody(r.Context(), id, r.URL.Query().Get("lang"))
		if err != nil {
			return nil, err
		}
		if typ != TypeID {
			return nil, apperr.NotFound("team")
		}
		c, err := decode(body)
		if err != nil {
			return nil, err
		}
		c = forViewer(r.Context(), c)
		secs, err := members(r.Context(), hs, c)
		if err != nil {
			return nil, err
		}
		return map[string]any{"config": c, "sections": secs}, nil
	})
	return nil
}
