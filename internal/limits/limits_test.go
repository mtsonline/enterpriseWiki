package limits

import (
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

func TestRegisterAndValue(t *testing.T) {
	d := Def{Key: "acme.demo.size_kb", Group: "requests", Unit: "kb", Default: 64, Min: 4, Max: 1024, Label: "Size"}
	if err := Register("acme.demo", d); err != nil {
		t.Fatal(err)
	}
	if err := Register("acme.demo", d); err != nil {
		t.Fatalf("the same declaration again (further sites): %v", err)
	}
	for name, bad := range map[string]Def{
		"foreign key":   {Key: "core.x.y", Group: "g", Unit: "kb", Default: 1, Min: 1, Max: 2, Label: "x"},
		"unknown unit":  {Key: "acme.demo.a", Group: "g", Unit: "parsec", Default: 1, Min: 1, Max: 2, Label: "x"},
		"default range": {Key: "acme.demo.b", Group: "g", Unit: "kb", Default: 9, Min: 1, Max: 2, Label: "x"},
		"no label":      {Key: "acme.demo.c", Group: "g", Unit: "kb", Default: 1, Min: 1, Max: 2},
		"changed":       {Key: "acme.demo.size_kb", Group: "requests", Unit: "kb", Default: 65, Min: 4, Max: 1024, Label: "Size"},
	} {
		if Register("acme.demo", bad) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	c := &config.Config{}
	if v := Value(c, d.Key); v != 64 {
		t.Errorf("default: %d", v)
	}
	c.Limits = map[string]int64{d.Key: 256}
	if v, b := Value(c, d.Key), Bytes(c, d.Key); v != 256 || b != 256<<10 {
		t.Errorf("configured: %d, %d bytes", v, b)
	}
	c.Limits[d.Key] = 999999 // outside of the range (edited by hand): the default applies
	if v := Value(c, d.Key); v != 64 {
		t.Errorf("out of range: %d", v)
	}
	if Check(d.Key, 2) == nil || Check(d.Key, 512) != nil || Check("acme.unknown", 1) == nil {
		t.Error("Check")
	}
	RouteBody("POST /api/v1/x/acme.demo/things", d.Key)
	if BodyKey("POST /api/v1/x/acme.demo/things") != d.Key || BodyKey("GET /nothing") != "" {
		t.Error("RouteBody")
	}
	if Value(nil, "core.request.guest_kb") != 64 {
		t.Error("core limits are registered")
	}
}
