// Package core_blocks provides blocks at template positions as elements:
// an element of the type core.block lives in a category (with its
// permissions, versions, drafts and translations), is never listed and is
// shown wherever it is placed ("core.blocks/<element id>").
package core_blocks

import (
	"context"
	"embed"
	_ "embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"slices"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/limits"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	core_html "github.com/mtsonline/enterprisewiki/plugins/core_html"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// TypeID is the element type of blocks.
const TypeID = "core.block"

// BlockPrefix: a block element is placed as BlockPrefix + its id.
const BlockPrefix = content.ElementBlockPrefix

// EditPerm may change blocks shown on the whole site (placed on the level
// template or site): content on every page is the administration's.
const EditPerm = "core.blocks.edit"

// Plugin is the blocks plugin.
type Plugin struct{ host *coreplugin.Host }

func init() { coreplugin.Register(&Plugin{}) }

// Manifest implements coreplugin.Plugin.
func (p *Plugin) Manifest() coreplugin.Manifest { return coreplugin.ParseManifest(manifest) }

// Locales implements coreplugin.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Type is the element type core.block: the content of a page (core.html),
// never listed, with a rule for blocks shown on the whole site.
type Type struct {
	core_html.Type
	p *Plugin
}

// TypeID implements content.ElementType.
func (t *Type) TypeID() string { return TypeID }

// Labels implements content.ElementType.
func (t *Type) Labels() map[string]string { return map[string]string{"en": "Block", "de": "Baustein"} }

// Unlisted implements content.Unlisted: blocks are shown where they are
// placed, not in listings, sources, feeds or the search.
func (t *Type) Unlisted() bool { return true }

// CheckCreate implements content.Guard (the permissions of the category
// suffice to create a block; placing it is checked by the placement).
func (t *Type) CheckCreate(context.Context, *content.Service, int64) error { return nil }

// CheckChange implements content.Guard: a block placed on the level
// template, site or element type is content on every page (of a type); only who may edit blocks
// (core.blocks.edit) changes, moves or deletes it.
func (t *Type) CheckChange(ctx context.Context, svc *content.Service, el *content.Element, _, _ json.RawMessage) error {
	site, err := svc.PlacedSiteWide(ctx, BlockPrefix+fmt.Sprint(el.ID))
	if err != nil {
		return err
	}
	if site && !perm.Has(ctx, EditPerm) {
		return apperr.Forbidden("this block is shown on the whole site: changing it needs the permission %q", EditPerm)
	}
	return nil
}

// Listed is a block element for the placement (its id and title).
type Listed struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	CategoryID int64  `json:"category_id"`
}

// Setup registers the element type and the API.
func (p *Plugin) Setup(host *coreplugin.Host) error {
	p.host = host
	host.RegisterElementType(&Type{p: p})
	// GET /blocks?lang=de lists the blocks the caller may see (id, title,
	// category): the blocks the placement offers. At most the expert
	// setting of the list limit.
	// action: core.blocks.list
	host.Handle("GET", "/blocks", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ctx := r.Context()
		max := limits.Value(host.Config.Get(), "core.blocks.list")
		els, err := host.Content.ElementsOfType(ctx, TypeID, r.URL.Query().Get("lang"), int(max))
		if err != nil {
			return nil, err
		}
		out := make([]Listed, 0, len(els))
		for _, e := range els {
			out = append(out, Listed{ID: e.ID, Title: e.Title, CategoryID: e.CategoryID})
		}
		return out, nil
	})
	// GET /blocks/{id}/placements: where a block is shown (the levels and
	// positions), for those who may edit it.
	// action: core.blocks.placements
	host.Handle("GET", "/blocks/{id}/placements", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		el, err := host.Content.GetElement(ctx, id, "", false)
		if err != nil {
			return nil, err
		}
		if el.Type != TypeID {
			return nil, apperr.NotFound("block")
		}
		if !slices.Contains(el.Permissions, "edit") {
			return nil, apperr.Forbidden("only who may edit the block sees where it is shown")
		}
		return host.Content.PlacementsOfBlock(ctx, BlockPrefix+fmt.Sprint(id))
	})
	return nil
}
