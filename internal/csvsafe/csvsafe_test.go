package csvsafe

import "testing"

func TestCell(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "plain": "plain", "=HYPERLINK(\"x\")": "'=HYPERLINK(\"x\")", "+1": "'+1", "-2": "'-2",
		"@SUM(A1)": "'@SUM(A1)", "\tx": "'\tx", "\rx": "'\rx", "a=b": "a=b",
	} {
		if got := Cell(in); got != want {
			t.Errorf("Cell(%q) = %q, want %q", in, got, want)
		}
	}
	if r := Row([]string{"=1", "ok"}); r[0] != "'=1" || r[1] != "ok" {
		t.Errorf("Row: %q", r)
	}
}
