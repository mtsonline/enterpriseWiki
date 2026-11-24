package core_layout

import (
	"strings"
	"testing"
)

func TestMapBlock(t *testing.T) {
	osm := mapBlock{allow: func() []string { return []string{"https://www.openstreetmap.org", "https://maps.example.com"} }}
	out, err := render(t, osm, "de", map[string]string{"place": "Universitätsstraße 65, Klagenfurt <Haupteingang>", "lat": "46.6167", "lon": "14.2653", "zoom": "17"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-ew-widget="ewl.embed"`, `data-src="https://www.openstreetmap.org/export/embed.html?bbox=`, `&amp;marker=46.616700%2C14.265300"`,
		`data-host="www.openstreetmap.org"`, `data-title="Karte: Universitätsstraße 65, Klagenfurt &lt;Haupteingang&gt;"`,
		`<figcaption><span class="ewl-map-place">Universitätsstraße 65, Klagenfurt &lt;Haupteingang&gt;</span></figcaption>`,
		`href="https://www.openstreetmap.org/?mlat=46.616700&amp;mlon=14.265300#map=17/46.616700/14.265300"`, `class="ewl-embed ewl-map ewl-embed--16x9"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	// another allowed map by its address
	out, err = render(t, osm, "en", map[string]string{"place": "Office", "address": "https://maps.example.com/m/42"}, "")
	if err != nil || !strings.Contains(out, `data-src="https://maps.example.com/m/42"`) {
		t.Fatalf("address: %v %s", err, out)
	}
	for _, p := range []map[string]string{
		{"lat": "46.6", "lon": "14.2"}, // no place
		{"place": "x"},                 // nothing to show
		{"place": "x", "lat": "46.6"},  // half a coordinate
		{"place": "x", "lat": "91", "lon": "14"},
		{"place": "x", "lat": "46,6", "lon": "14"},
		{"place": "x", "lat": "46.6", "lon": "1e2"},
		{"place": "x", "lat": "46.6", "lon": "14", "zoom": "25"},
		{"place": "x", "lat": "46.6", "lon": "14", "address": "https://maps.example.com/m"},
		{"place": "x", "address": "https://evil.example/m"},
		{"place": "x", "address": "javascript:alert(1)"},
	} {
		if _, err := render(t, osm, "en", p, ""); err == nil {
			t.Errorf("%v accepted", p)
		}
	}
	// OpenStreetMap itself must be allowed too
	if _, err := render(t, mapBlock{}, "en", map[string]string{"place": "x", "lat": "1", "lon": "2"}, ""); err == nil || !strings.Contains(err.Error(), "openstreetmap") {
		t.Errorf("not allowed: %v", err)
	}
}
