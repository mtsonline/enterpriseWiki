// Package content_propdb provides the element type "Property database":
// the elements of sources (categories, tags, types) with their properties,
// interactive in the browser (filters, views, editing in place, new
// entries). The data is read with the permissions of the viewer through
// core.properties; the server renders an accessible table as a fallback.
package content_propdb

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
	"github.com/mtsonline/enterprisewiki/plugins/core_html"
	props "github.com/mtsonline/enterprisewiki/plugins/core_properties"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// TypeID of the element type.
const TypeID = "content.propdb"

// Plugin is the property database plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Setup registers the element type.
func (p *Plugin) Setup(hs *sdk.Host) error {
	hs.RegisterElementType(Type{})
	return nil
}

// Views a database can open with.
var Views = []string{"table", "cards", "kanban", "calendar", "timeline", "chart"}

// Config is the body of a database. Sources, filters, columns and sort are
// edited as the short texts of the property report (source, filter,
// columns, sort) and stored also parsed.
type Config struct {
	Intro      core_html.Body  `json:"intro"`
	SourceText string          `json:"source"`
	FilterText string          `json:"filter"`
	ColumnText string          `json:"columns_text"`
	SortText   string          `json:"sort_text"`
	Sources    []props.Source  `json:"sources"` // empty: the category of the element with subcategories
	Filters    []props.Filter  `json:"filters"` // always applied (readers add their own)
	Columns    []string        `json:"columns"`
	Sort       []props.SortKey `json:"sort"`
	View       string          `json:"view"`
	Group      string          `json:"group"` // board columns, chart categories
	Date       string          `json:"date"`  // calendar, timeline
	// NewType and NewCategory: new entries (element type, category; 0 = the category of the database)
	NewType     string `json:"new_type"`
	NewCategory int64  `json:"new_category"`
	PerPage     int    `json:"per_page"`
	// ExportMode of downloads: safe (default: values starting with = stay
	// text) or original (they become formulas)
	ExportMode string `json:"export_mode"`
}

var keyRe = func(s string) bool {
	if s == "" || len(s) > 63 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

func decode(raw []byte) (*Config, error) {
	c := &Config{}
	if err := json.Unmarshal(raw, c); err != nil {
		return nil, apperr.BadRequest("invalid settings: %v", err)
	}
	if !slices.Contains(Views, c.View) {
		c.View = "table"
	}
	if len(c.SourceText) > 2000 || len(c.FilterText) > 4000 || len(c.ColumnText) > 2000 || len(c.SortText) > 500 {
		return nil, apperr.BadRequest("settings too long")
	}
	// the texts are authoritative: the parsed forms are derived from them
	var err error
	if c.Sources, err = props.ParseSources(c.SourceText); err != nil {
		return nil, err
	}
	if c.Filters, err = props.ParseFilters(c.FilterText); err != nil {
		return nil, err
	}
	c.Columns, c.Sort = nil, nil
	for _, k := range strings.Split(c.ColumnText, ",") {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			c.Columns = append(c.Columns, k)
		}
	}
	for _, k := range strings.Split(c.SortText, ",") {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			c.Sort = append(c.Sort, props.SortKey{Key: strings.TrimPrefix(k, "-"), Desc: strings.HasPrefix(k, "-")})
		}
	}
	if len(c.Sources) > 50 || len(c.Filters) > 30 || len(c.Columns) > 50 || len(c.Sort) > 5 {
		return nil, apperr.BadRequest("too many sources, filters, columns or sort keys")
	}
	for _, s := range c.Sources {
		switch s.Kind {
		case "category", "tag", "type", "elements":
		default:
			return nil, apperr.BadRequest("unknown source %q", s.Kind)
		}
		if utf8.RuneCountInString(s.Tag) > 100 || len(s.IDs) > 1000 {
			return nil, apperr.BadRequest("invalid source")
		}
	}
	for _, f := range c.Filters {
		if !keyRe(f.Key) || len(f.Value) > 4000 {
			return nil, apperr.BadRequest("invalid filter")
		}
	}
	for _, k := range append(append([]string{}, c.Columns...), c.Group, c.Date) {
		if k != "" && !keyRe(k) {
			return nil, apperr.BadRequest("invalid property key %q", k)
		}
	}
	for _, s := range c.Sort {
		if !keyRe(s.Key) {
			return nil, apperr.BadRequest("invalid sort key")
		}
	}
	if c.PerPage <= 0 || c.PerPage > 500 {
		c.PerPage = 50
	}
	if c.ExportMode != "original" {
		c.ExportMode = "safe"
	}
	if c.NewCategory < 0 {
		c.NewCategory = 0
	}
	if len(c.NewType) > 100 {
		return nil, apperr.BadRequest("invalid type")
	}
	if c.Intro.Format == "" {
		c.Intro.Format = "markdown"
	}
	d, _ := json.Marshal(c.Intro)
	clean, err := (&core_html.Type{}).Normalize(d)
	if err != nil {
		return nil, apperr.BadRequest("intro: %v", err)
	}
	_ = json.Unmarshal(clean, &c.Intro)
	if c.Sources == nil {
		c.Sources = []props.Source{}
	}
	if c.Filters == nil {
		c.Filters = []props.Filter{}
	}
	if c.Columns == nil {
		c.Columns = []string{}
	}
	if c.Sort == nil {
		c.Sort = []props.SortKey{}
	}
	return c, nil
}

// Type implements content.ElementType.
type Type struct{}

// TypeID implements content.ElementType.
func (Type) TypeID() string { return TypeID }

// ListsElements implements content.Listing (left out of lists of several sources).
func (Type) ListsElements() bool { return true }

// Labels implements content.ElementType.
func (Type) Labels() map[string]string {
	return map[string]string{"en": "Property database", "de": "Eigenschaften-Datenbank"}
}

// EmptyBody implements content.ElementType.
func (Type) EmptyBody() json.RawMessage {
	return json.RawMessage(`{"intro":{"format":"markdown","source":""},"source":"","filter":"","columns_text":"","sort_text":"","sources":[],"filters":[],"columns":[],"sort":[],"view":"table","group":"","date":"","new_type":"core.html","new_category":0,"per_page":50,"export_mode":"safe"}`)
}

// Normalize implements content.ElementType.
func (Type) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	c, err := decode(raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

// Text implements content.ElementType.
func (Type) Text(raw json.RawMessage) string {
	var c Config
	_ = json.Unmarshal(raw, &c)
	return c.Intro.Source
}

// Render implements content.ElementType: the widget with an accessible
// table of the first page as fallback (permissions of the viewer apply).
func (Type) Render(ctx context.Context, rc content.RenderContext, raw json.RawMessage) (string, error) {
	c, err := decode(raw)
	if err != nil {
		return "", err
	}
	cfg, _ := json.Marshal(c)
	var b strings.Builder
	if strings.TrimSpace(c.Intro.Source) != "" {
		b.WriteString(`<div class="ewdb-intro">` + core_html.ToHTML(&c.Intro) + `</div>`)
	}
	fmt.Fprintf(&b, `<section class="ewdb" data-ew-widget="core.properties.database" data-config="%s" data-category="%d" data-element="%d">`,
		html.EscapeString(string(cfg)), rc.CategoryID, rc.ElementID)
	q := &props.Query{Sources: c.Sources, Filters: c.Filters, Columns: c.Columns, Sort: c.Sort, Limit: c.PerPage, Lang: rc.Lang, Exclude: rc.ElementID}
	if len(q.Sources) == 0 {
		q.Sources = []props.Source{{Kind: "category", ID: rc.CategoryID, Sub: true}}
	}
	fallback, err := props.RenderTable(ctx, q)
	if err != nil {
		fallback = `<p class="ew-notice ew-notice--warning" role="note">` + html.EscapeString(err.Error()) + `</p>`
	}
	b.WriteString(`<div class="ewprop-fallback">` + fallback + `</div></section>`)
	return b.String(), nil
}
