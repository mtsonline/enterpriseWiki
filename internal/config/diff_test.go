package config

import (
	"slices"
	"testing"
)

func TestChangedKeys(t *testing.T) {
	a := Defaults()
	b, err := clone(a)
	if err != nil {
		t.Fatal(err)
	}
	if got := ChangedKeys(a, b); len(got) != 0 {
		t.Fatalf("no change: %v", got)
	}
	b.UI.SiteName = "Other"
	b.Retention.TrashDays = a.Retention.TrashDays + 1
	got := ChangedKeys(a, b)
	if !slices.Equal(got, []string{"retention.trash_days", "ui.site_name"}) {
		t.Fatalf("changed: %v", got)
	}
}
