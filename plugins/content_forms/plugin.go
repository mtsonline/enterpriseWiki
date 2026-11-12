// Package content_forms provides the element type "Form" (content.forms):
// web forms with all usual fields, conditions, checks and uploads, laid out
// as an accessible grid or placed freely. Everyone who may see the form may
// submit it; the submissions are stored (property database, e-mail,
// workflow and webhook as further targets) and are seen by the creator of
// the form and by persons with "See submissions".
//
// Security: values are checked on the server (types, lengths, patterns,
// options, conditions), uploads by content (type, Office macros, active PDF
// content, optional virus scanner), a signed token with a minimum age and a
// hidden field keep bots out, and submissions are limited per address and
// account. What happens with a submission is done with the rights of the
// creator of the form, checked again at that moment.
package content_forms

import (
	"context"
	"embed"
	"encoding/json"
	"html"
	"io/fs"
	"strings"
	"sync"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

//go:embed migrations/*.sql
var migrations embed.FS

// TypeID is the element type.
const TypeID = "content.forms"

const (
	permCreate      = "content.forms.create"
	permAdmin       = "content.forms.admin"
	permSubmissions = "content.forms.submissions"
	permExternal    = "content.forms.external"
)

// Plugin is the forms plugin (one instance per site).
type Plugin struct {
	host    *sdk.Host
	limiter *limiter
}

func init() { sdk.Register(&Plugin{}) }

// NewInstance implements sdk.Factory (one plugin per site).
func (p *Plugin) NewInstance() sdk.Plugin { return &Plugin{} }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// hosts of the sites (the element type is registered once per process)
var hosts sync.Map

func hostOf(ctx context.Context) *sdk.Host {
	if h, ok := hosts.Load(sdk.SiteOf(ctx)); ok {
		return h.(*sdk.Host)
	}
	return nil
}

// Setup implements sdk.SetupPlugin.
func (p *Plugin) Setup(host *sdk.Host) error {
	p.host = host
	p.limiter = newLimiter()
	hosts.Store(host.Site, host)
	host.RegisterElementType(Type{})
	host.RegisterMacro(resultsMacro{p})
	host.Files.AddReference("plugin_form_submissions", "files")
	host.RegisterWorker("deliver", 30*time.Second, p.deliver)
	host.RegisterWorker("retention", time.Hour, p.retention)
	p.routes()
	return nil
}

// ---------------------------------------------------------------- element type

// Type is the element type "Form".
type Type struct{}

// TypeID implements content.ElementType.
func (Type) TypeID() string { return TypeID }

// Labels implements content.ElementType.
func (Type) Labels() map[string]string { return map[string]string{"en": "Form", "de": "Formular"} }

// EmptyBody implements content.ElementType.
func (Type) EmptyBody() json.RawMessage {
	return json.RawMessage(`{"intro":"","layout":"grid","fields":[{"id":"name","type":"text","label":"Name","required":true,"autocomplete":"name","width":6},{"id":"email","type":"email","label":"E-mail","required":true,"autocomplete":"email","width":6},{"id":"message","type":"textarea","label":"Message","rows":5}],"targets":{"store":true}}`)
}

// Normalize implements content.ElementType.
func (Type) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	f, err := Decode(raw)
	if err != nil {
		return nil, err
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(f)
}

// Text implements content.ElementType (labels, help and intro for the search).
func (Type) Text(raw json.RawMessage) string {
	f, err := Decode(raw)
	if err != nil {
		return ""
	}
	parts := []string{f.Intro}
	for _, x := range f.Fields {
		parts = append(parts, x.Label, x.Help)
	}
	return strings.Join(parts, "\n")
}

// CheckCreate implements content.Guard.
func (Type) CheckCreate(ctx context.Context, _ *content.Service, categoryID int64) error {
	gh := hostOf(ctx)
	if gh == nil || gh.Can(ctx, categoryID, permCreate) || gh.Can(ctx, categoryID, permAdmin) {
		return nil
	}
	return apperr.Forbidden("creating forms needs the permission %q in this category", "Create forms")
}

// CheckChange implements content.Guard: the creator (with "Create forms")
// or a forms administrator; targets need the rights for them.
func (Type) CheckChange(ctx context.Context, _ *content.Service, el *content.Element, _, newBody json.RawMessage) error {
	gh := hostOf(ctx)
	if gh == nil {
		return nil
	}
	p := identity.From(ctx)
	own := p != nil && el.CreatedByID != nil && *el.CreatedByID == p.ID()
	if !(own && gh.Can(ctx, el.CategoryID, permCreate)) && !gh.Can(ctx, el.CategoryID, permAdmin) {
		return apperr.Forbidden("only the creator of the form or a forms administrator may change it")
	}
	if newBody == nil {
		return nil
	}
	f, err := Decode(newBody)
	if err != nil {
		return err
	}
	return checkTargets(ctx, gh, el.CategoryID, f)
}

// checkTargets: e-mail to addresses outside the wiki and webhooks need
// "Send submissions outside"; the property database and the workflow must
// be usable by the editor; media categories need the right to upload.
func checkTargets(ctx context.Context, gh *sdk.Host, cat int64, f *Form) error {
	external := gh.Can(ctx, cat, permExternal)
	for _, e := range f.Targets.Email {
		for _, r := range Recipients(e.To) {
			if !strings.HasPrefix(r, "user:") && !strings.HasPrefix(r, "group:") && !external {
				return apperr.Forbidden("e-mail to %s needs the permission %q", r, "Send submissions outside")
			}
		}
	}
	if f.Targets.Webhook != nil && !external {
		return apperr.Forbidden("a webhook needs the permission %q", "Send submissions outside")
	}
	if t := f.Targets.PropDB; t != nil {
		if err := propDBUsable(ctx, gh, t.Element); err != nil {
			return err
		}
	}
	if t := f.Targets.Workflow; t != nil {
		if err := workflowUsable(ctx, gh, t.Element); err != nil {
			return err
		}
	}
	if t := f.Targets.Files; t != nil && !gh.ACL.Can(ctx, identity.From(ctx), t.Category, acl.Edit) {
		return apperr.Forbidden("files: no right to upload into category %d", t.Category)
	}
	for _, x := range f.Fields {
		if x.MediaCategory > 0 && !gh.ACL.Can(ctx, identity.From(ctx), x.MediaCategory, acl.Edit) {
			return apperr.Forbidden("field %q: no right to upload into category %d", x.ID, x.MediaCategory)
		}
	}
	return nil
}

var esc = html.EscapeString
