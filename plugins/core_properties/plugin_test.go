package core_properties

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFormula(t *testing.T) {
	vals := map[string]float64{"a": 3, "b": 4, "zero": 0}
	cases := map[string]float64{
		"a + b * 2": 11, "(a + b) * 2": 14, "-a + 10": 7, "round(10 / 3, 2)": 3.33, "max(a, b, 1)": 4, "min(a, b)": 3,
		"if(a > b, 1, 2)": 2, "a < b && b < 5": 1, "!(a == 3) || zero": 0, "sqrt(a*a + b*b)": 5, "missing + 1": 1, "a % 2": 1, "abs(-2.5)": 2.5,
	}
	for src, want := range cases {
		f, err := ParseFormula(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		got, err := f.Eval(vals)
		if err != nil || got != want {
			t.Fatalf("%s = %v (%v), want %v", src, got, err, want)
		}
	}
	for _, src := range []string{"", "a +", "(a", "a b", "unknown(1)", "if(1, 2)", "a $ b", strings.Repeat("(", 60) + "1" + strings.Repeat(")", 60), strings.Repeat("1+", 300) + "1"} {
		if _, err := ParseFormula(src); err == nil {
			t.Fatalf("accepted %q", src)
		}
	}
	f, _ := ParseFormula("a / zero")
	if _, err := f.Eval(vals); err == nil {
		t.Fatal("division by zero")
	}
}

func TestCheckValue(t *testing.T) {
	ok := map[string][2]string{
		"integer": {`"12"`, `12`}, "decimal": {`"12,5"`, `12.5`}, "currency": {`3.14159`, `3.14`}, "boolean": {`true`, `true`},
		"date": {`"2026-02-28"`, `"2026-02-28"`}, "email": {`"a@example.org"`, `"a@example.org"`}, "color": {`"#AABBCC"`, `"#aabbcc"`},
		"duration": {`"1h 30m"`, `5400`}, "version": {`"1.10.2"`, `"1.10.2"`}, "country": {`"at"`, `"AT"`}, "time": {`"09:30"`, `"09:30"`},
		"multiselect": {`["a","a","b"]`, `["a","b"]`}, "daterange": {`{"from":"2026-01-01","to":"2026-01-31"}`, `{"from":"2026-01-01","to":"2026-01-31"}`},
		"location": {`{"address":"Graz","lat":47.07,"lng":15.44}`, `{"address":"Graz","lat":47.07,"lng":15.44}`},
	}
	for typ, c := range ok {
		td, _ := TypeByID(typ)
		v, err := CheckValue("k", td, Options{}, json.RawMessage(c[0]))
		if err != nil || v == nil || string(v.Raw) != c[1] {
			t.Fatalf("%s %s: %v %v", typ, c[0], v, err)
		}
	}
	bad := map[string]string{
		"integer": `"1.5"`, "date": `"2026-02-30"`, "email": `"a@"`, "url": `"javascript:alert(1)"`, "color": `"red"`, "progress": `120`,
		"daterange": `{"from":"2026-02-01","to":"2026-01-01"}`, "media": `"/api/v1/files/../../etc/passwd"`, "user": `[0]`, "text": `"a\u0000b"`,
		"location": `{"address":"x","lat":95,"lng":0}`, "phone": `"call me"`, "formula": `1`,
	}
	for typ, raw := range bad {
		td, _ := TypeByID(typ)
		if v, err := CheckValue("k", td, Options{}, json.RawMessage(raw)); err == nil {
			t.Fatalf("%s accepted %s: %v", typ, raw, v)
		}
	}
	sel, _ := TypeByID("select")
	if v, err := CheckValue("k", sel, Options{Choices: []Choice{{Value: "Open"}}}, json.RawMessage(`"open"`)); err != nil || string(v.Raw) != `"Open"` {
		t.Fatalf("choice: %v %v", v, err)
	}
}

func TestParseFilters(t *testing.T) {
	fs, err := ParseFilters("status in open,review; due <= today+30d; owner = me; budget between 100..500; notes empty; title contains in house; done != true")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, f := range fs {
		got = append(got, f.Key+" "+f.Op+" "+string(f.Value))
	}
	want := `status in ["open","review"]|due le "today+30d"|owner me |budget between [100,500]|notes empty |title contains "in house"|done ne true`
	if strings.Join(got, "|") != want {
		t.Fatalf("%s", strings.Join(got, "|"))
	}
	if _, err := ParseFilters("nonsense"); err == nil {
		t.Fatal("accepted")
	}
	src, err := ParseSources("category:12+sub; tag:hr; type:core.html; elements:1,2")
	if err != nil || len(src) != 4 || !src[0].Sub || src[3].IDs[1] != 2 {
		t.Fatalf("%+v %v", src, err)
	}
}
