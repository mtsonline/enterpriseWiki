package content

import "testing"

func TestSourceModesText(t *testing.T) {
	list, err := ParseSources("category:3+sub; -tag:Intern; + type:core.html; -elements:4,5")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 || list[0].Mode != "" || list[1].Mode != ModeExclude || list[1].Tag != "intern" || list[2].Mode != ModeRequire || list[3].Mode != ModeExclude {
		t.Fatalf("modes: %+v", list)
	}
	if got := FormatSources(list); got != "category:3+sub; -tag:intern; +type:core.html; -elements:4,5" {
		t.Fatalf("format: %q", got)
	}
	if HasInclude(list[1:]) || !HasInclude(list) {
		t.Fatal("HasInclude")
	}
	if d := WithDefault(list[1:2], 9); len(d) != 2 || d[0].ID != 9 || !d[0].Sub {
		t.Fatalf("WithDefault: %+v", d)
	}
	for _, bad := range []string{"-nope:1", "+search:", "--tag:x"} {
		if _, err := ParseSources(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestPaging(t *testing.T) {
	p := Paging{Limit: 6, Paginate: true, PerPage: 4, Page: 2}
	if p.Offset() != 4 || p.Size() != 2 || p.Pages(100) != 2 || p.Pages(5) != 2 || p.Pages(3) != 1 {
		t.Fatalf("paging: %d %d %d", p.Offset(), p.Size(), p.Pages(100))
	}
	if q := (Paging{Limit: 6, PerPage: 4, Page: 1}); q.Offset() != 0 || q.Size() != 6 || q.Pages(10) != 1 {
		t.Fatal("no paging")
	}
}
