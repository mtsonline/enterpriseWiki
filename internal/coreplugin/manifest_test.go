package coreplugin

import "testing"

func TestManifestValidate(t *testing.T) {
	ok := Manifest{ID: "acme.kanban", Name: "Kanban", Version: "1.0.0", Kinds: []string{"element", "macro"}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	tpl := Manifest{ID: "acme.theme", Name: "T", Version: "1", Kinds: []string{"template"}, ColorScheme: "system"}
	if err := tpl.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Manifest{
		{ID: "acme.kanban", Name: "K", Version: "1"},                                                   // no kinds
		{ID: "acme.kanban", Name: "K", Version: "1", Kinds: []string{"generic"}},                       // unknown kind
		{ID: "kanban", Name: "K", Version: "1", Kinds: []string{"element"}},                            // no vendor
		{ID: "Acme.Kanban", Name: "K", Version: "1", Kinds: []string{"element"}},                       // upper case
		{ID: "acme.kanban", Version: "1", Kinds: []string{"element"}},                                  // no name
		{ID: "acme.k/../x", Name: "K", Version: "1", Kinds: []string{"element"}},                       // path
		{ID: "acme.theme", Name: "T", Version: "1", Kinds: []string{"template"}, ColorScheme: "night"}, // unknown scheme
		{ID: "acme.kanban", Name: "K", Version: "1", Kinds: []string{"element"}, ColorScheme: "dark"},  // not a template
	} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}
