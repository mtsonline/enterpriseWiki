// Package core_properties gives elements of every type structured
// properties (text, numbers, dates, choices, people, links, formulas …):
//
//   - definitions are global or belong to a category and are inherited by
//     its subcategories (a nearer definition of the same key wins); each
//     has a type with options, may be required, have a default, apply to
//     some element types only and restrict who sees and who changes it;
//   - authors may add free properties (a key with a type), unless the
//     category or the whole site allows definitions only;
//   - tags are the built-in property "tags" (stored with the element);
//   - values are edited together with the element (a field of every
//     editor, see content.ElementField) and stored typed and indexed, so
//     the macro "Property report" and the element "Property database"
//     filter, sort, group and sum in the database.
//
// Seeing a value needs the View permission of the element and, for a
// definition with restricted visibility, being one of its subjects;
// changing it the Edit permission and the edit subjects. Definitions need
// core.properties.admin (global) or core.properties.define (category).
package core_properties

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"sync"

	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// Limits.
const (
	MaxProps    = 100  // values per element
	MaxDefs     = 300  // definitions per category (and global)
	MaxValueLen = 4000 // characters of a text value
	MaxLabelLen = 80
)

// Plugin is the properties plugin.
type Plugin struct {
	host *sdk.Host
	mu   sync.Mutex
	// parsed formulas by definition id and its options' text
	formulas map[string]*Formula
}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// Setup implements sdk.SetupPlugin.
func (p *Plugin) Setup(host *sdk.Host) error {
	p.host = host
	p.formulas = map[string]*Formula{}
	host.RegisterElementField(field{p})
	host.RegisterMacro(report{p})
	p.routesDefs()
	p.routesValues()
	p.routesQuery()
	return nil
}

// formula returns the parsed formula of a definition (cached).
func (p *Plugin) formula(src string) (*Formula, error) {
	p.mu.Lock()
	f, ok := p.formulas[src]
	p.mu.Unlock()
	if ok {
		return f, nil
	}
	f, err := ParseFormula(src)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	if len(p.formulas) > 1000 {
		p.formulas = map[string]*Formula{}
	}
	p.formulas[src] = f
	p.mu.Unlock()
	return f, nil
}

// freeDefault is the site setting "free" (free properties allowed; default yes).
func (p *Plugin) freeDefault() bool {
	v, ok := p.host.Settings()["free"].(bool)
	return !ok || v
}

func pick(m map[string]string, lang string) string {
	if v := m[lang]; v != "" {
		return v
	}
	if v := m["en"]; v != "" {
		return v
	}
	for _, v := range m {
		return v
	}
	return ""
}

func lm(en, de string) map[string]string { return map[string]string{"en": en, "de": de} }

func rawOr(v json.RawMessage, def string) json.RawMessage {
	if len(v) == 0 {
		return json.RawMessage(def)
	}
	return v
}

var _ = context.Background
