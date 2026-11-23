package markdown

import (
	"strings"
	"testing"
)

func TestSetTask(t *testing.T) {
	src := "Intro [ ] not a task\n\n- [ ] one @anna\n  - [x] nested\n- plain\n- [ ] two\n\n```\n- [ ] code\n```\n\n> - [X] quoted\n"
	for _, c := range []struct {
		n    int
		done bool
		want string
	}{
		{1, true, "Intro [ ] not a task\n\n- [x] one @anna\n  - [x] nested\n- plain\n- [ ] two\n\n```\n- [ ] code\n```\n\n> - [X] quoted\n"},
		{2, false, "Intro [ ] not a task\n\n- [ ] one @anna\n  - [ ] nested\n- plain\n- [ ] two\n\n```\n- [ ] code\n```\n\n> - [X] quoted\n"},
		{3, true, "Intro [ ] not a task\n\n- [ ] one @anna\n  - [x] nested\n- plain\n- [x] two\n\n```\n- [ ] code\n```\n\n> - [X] quoted\n"},
		{4, false, "Intro [ ] not a task\n\n- [ ] one @anna\n  - [x] nested\n- plain\n- [ ] two\n\n```\n- [ ] code\n```\n\n> - [ ] quoted\n"},
	} {
		got, err := SetTask(src, c.n, c.done)
		if err != nil || got != c.want {
			t.Errorf("SetTask(%d, %v) = %q, %v", c.n, c.done, got, err)
		}
	}
	if _, err := SetTask(src, 5, true); err != ErrNoTask {
		t.Errorf("item 5: %v", err)
	}
	if _, err := SetTask(src, 0, true); err != ErrNoTask {
		t.Errorf("item 0: %v", err)
	}
}

func TestToHTMLTasks(t *testing.T) {
	got, err := ToHTMLTasks("- [ ] one\n  - [x] two\n- plain\n\n<ul><li><input type=\"checkbox\"> raw</li></ul>\n", "zq-")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<li class="zq-1"><input disabled="" type="checkbox"> one`, `<li class="zq-2"><input checked="" disabled="" type="checkbox"> two`, "<li>plain</li>", `<li><input type="checkbox"> raw</li>`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}
