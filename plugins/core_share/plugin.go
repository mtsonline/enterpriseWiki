// Package core_share shares pages and categories with users, groups and
// roles. The recipients get a notification in the wiki and by e-mail
// (unless switched off in their profile). Sharing never changes
// permissions: whoever may not see the target is listed and skipped.
package core_share

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/notify"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// Plugin is the share plugin.
type Plugin struct{ host *sdk.Host }

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Input names the target and the recipients.
type Input struct {
	TargetType string  `json:"target_type"` // element | category
	TargetID   int64   `json:"target_id"`
	Users      []int64 `json:"users"`
	Groups     []int64 `json:"groups"`
	Roles      []int64 `json:"roles"`
	Message    string  `json:"message"`
}

// Result says who was (or would be) notified.
type Result struct {
	Recipients    int      `json:"recipients"`
	WithoutAccess []string `json:"without_access"` // may not see the target, not notified
	Sent          bool     `json:"sent"`
}

type target struct {
	category int64
	title    string
	link     string
}

// Setup implements sdk.SetupPlugin.
func (p *Plugin) Setup(host *sdk.Host) error {
	p.host = host
	notify.RegisterKind("share", "core.share.kind")
	// POST /check – who would be notified (without sending)
	host.Handle("POST", "/check", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in Input
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return p.share(r.Context(), in, false)
	})
	// POST /share – notify the recipients
	host.Handle("POST", "/share", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in Input
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		return p.share(r.Context(), in, true)
	})
	return nil
}

func (p *Plugin) target(ctx context.Context, in Input) (*target, error) {
	lang := p.host.Config.Get().I18n.DefaultLanguage
	switch in.TargetType {
	case "element":
		cat, err := p.host.Content.ElementCategory(ctx, in.TargetID, acl.View)
		if err != nil {
			return nil, err
		}
		v, err := p.host.Content.GetElement(ctx, in.TargetID, lang, false)
		if err != nil {
			return nil, err
		}
		return &target{cat, v.Title, fmt.Sprintf("/e/%d", in.TargetID)}, nil
	case "category":
		if err := p.host.Content.Require(ctx, in.TargetID, acl.View); err != nil {
			return nil, err
		}
		c, err := p.host.Content.GetCategory(ctx, in.TargetID)
		if err != nil {
			return nil, err
		}
		title := c.Titles[lang]
		for _, t := range c.Titles {
			if title == "" {
				title = t
			}
		}
		return &target{in.TargetID, title, fmt.Sprintf("/c/%d", in.TargetID)}, nil
	}
	return nil, apperr.BadRequest("target_type must be element or category")
}

func (p *Plugin) share(ctx context.Context, in Input, send bool) (*Result, error) {
	if err := perm.Require(ctx, "core.share.use"); err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(in.Message) > 2000 {
		return nil, apperr.BadRequest("message too long (max. 2000 characters)")
	}
	t, err := p.target(ctx, in)
	if err != nil {
		return nil, err
	}
	ids, err := p.host.Members(ctx, in.Users, in.Groups, in.Roles)
	if err != nil {
		return nil, err
	}
	if len(ids) > 2000 {
		return nil, apperr.BadRequest("too many recipients (max. 2000)")
	}
	me := identity.From(ctx)
	res := &Result{WithoutAccess: []string{}}
	var ok []int64
	for _, id := range ids {
		if id == me.ID() {
			continue
		}
		if p.host.UserCan(ctx, id, t.category, acl.View) {
			ok = append(ok, id)
			continue
		}
		var name string
		_ = p.host.DB.QueryRow(ctx, `SELECT COALESCE(NULLIF(display_name, ''), username) FROM users WHERE id=$1`, id).Scan(&name)
		res.WithoutAccess = append(res.WithoutAccess, name)
	}
	res.Recipients = len(ok)
	if !send || len(ok) == 0 {
		return res, nil
	}
	by := me.DisplayName
	if by == "" {
		by = me.Handle()
	}
	msg := strings.TrimSpace(in.Message)
	n, err := p.host.Notify.Send(ctx, ok, notify.Message{Kind: "share", EntityType: in.TargetType, EntityID: in.TargetID, Link: t.link,
		Text: func(lang string) (string, string) {
			params := map[string]string{"by": by, "title": t.title, "message": msg}
			body := p.host.T(ctx, lang, "core.share.mail_body", params)
			if msg != "" {
				body += "\n\n" + p.host.T(ctx, lang, "core.share.mail_message", params)
			}
			return p.host.T(ctx, lang, "core.share.mail_subject", params), body
		}})
	if err != nil {
		return nil, err
	}
	res.Recipients, res.Sent = n, true
	_ = audit.Log(ctx, p.host.DB, "share.send", in.TargetType, in.TargetID, nil, map[string]any{"users": in.Users, "groups": in.Groups, "roles": in.Roles, "recipients": n, "without_access": len(res.WithoutAccess)})
	return res, nil
}
