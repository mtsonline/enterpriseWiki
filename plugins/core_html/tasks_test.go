package core_html

import (
	"encoding/json"
	"fmt"
	"testing"

	nethtml "golang.org/x/net/html"
)

func TestTasks(t *testing.T) {
	src := "<p><input type=\"checkbox\"> raw @eve</p>\n\n<ul><li><input checked type=\"checkbox\"> raw item @eve</li></ul>\n\n" +
		"- [ ] One **bold** @Anna, @bob@ldap :date{value=2026-11-30} :date{value=2027-01-01}\n" +
		"  - [x] Nested `@code` mail a@example.com\n" +
		"- plain @carl\n" +
		"> - [X] Quoted @dora\n\n" +
		":::if{group=x}\n- [ ] Secret @eve\n:::\n"
	body, _ := json.Marshal(Body{Format: "markdown", Source: src})
	hidden := func(n *nethtml.Node) bool {
		for p := n.Parent; p != nil; p = p.Parent {
			for _, a := range p.Attr {
				if a.Key == "data-macro" && a.Val == "if" {
					return true
				}
			}
		}
		return false
	}
	tasks, err := Tasks(body, hidden)
	if err != nil {
		t.Fatal(err)
	}
	got := ""
	for _, x := range tasks {
		got += fmt.Sprintf("%d %v %q %s %v %v\n", x.N, x.Done, x.Title, x.Day, x.People, x.Hidden)
	}
	want := `1 false "One bold @Anna, @bob@ldap" 2026-11-30 [anna bob@ldap] false
2 true "Nested @code mail a@example.com"  [] false
3 true "Quoted @dora"  [dora] false
4 false "Secret @eve"  [eve] true
`
	if got != want {
		t.Fatalf("tasks:\n%s\nwant:\n%s", got, want)
	}
	// the data: a task needs a person or a day; hidden ones are left out
	items := taskExtractor{}.ExtractData(TypeID, body, hidden)
	if len(items) != 2 || items[0].Index != 1 || items[0].NeedPerson || items[1].Index != 3 || !items[1].NeedPerson || items[1].Data["state"] != "done" {
		t.Fatalf("items: %+v", items)
	}
	if items := (taskExtractor{}).ExtractData("core.block", body, hidden); items != nil {
		t.Fatalf("other types: %+v", items)
	}
}
