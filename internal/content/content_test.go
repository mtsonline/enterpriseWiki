package content

import "testing"

func TestNormalizeTags(t *testing.T) {
	got, err := NormalizeTags([]string{" Docs", "#docs", "HR", "", "c++"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "docs" || got[1] != "hr" || got[2] != "c++" {
		t.Fatalf("%v", got)
	}
	if _, err := NormalizeTags([]string{"<script>"}); err == nil {
		t.Fatal("invalid tag accepted")
	}
}

func TestSlugify(t *testing.T) {
	if s := Slugify("Über uns & Größe!"); s != "ueber-uns-groesse" {
		t.Fatalf("%q", s)
	}
}

func TestBuildTSQuery(t *testing.T) {
	if q := buildTSQuery(`foo bar's (baz) & | !`); q != "foo:* & bar:* & s:* & baz:*" {
		t.Fatalf("%q", q)
	}
	if q := buildTSQuery("  !!  "); q != "" {
		t.Fatalf("%q", q)
	}
}

func TestPathString(t *testing.T) {
	if pathString(parsePath("1.20.300")) != "1.20.300" {
		t.Fatal("roundtrip")
	}
}
