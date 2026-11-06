package wopi

import (
	"context"
	"sort"
	"strings"
)

// Str reads a string setting.
func Str(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return strings.TrimSpace(v)
}

// Bool reads a bool setting (def when missing).
func Bool(m map[string]any, k string, def bool) bool {
	if v, ok := m[k].(bool); ok {
		return v
	}
	return def
}

// Status is the result of a connection test.
type Status struct {
	OK         bool     `json:"ok"`
	Error      string   `json:"error,omitempty"`
	Discovery  string   `json:"discovery"`
	Actions    int      `json:"actions"`
	View       []string `json:"view"`
	Edit       []string `json:"edit"`
	ProofKey   bool     `json:"proof_key"`
	Origins    []string `json:"origins"`
	Configured bool     `json:"configured"`
}

// Test fetches the discovery again and reports what the program offers.
func (o *Opener) Test(ctx context.Context) Status {
	st := Status{View: []string{}, Edit: []string{}, Origins: []string{}}
	c := o.Client()
	if c == nil {
		return st
	}
	st.Configured, st.Discovery = true, c.URL
	d, err := c.fetch(ctx)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	c.mu.Lock()
	c.d, c.lastTry, c.lastError = d, d.Fetched, nil
	c.cached.Store(d)
	c.mu.Unlock()
	st.OK, st.Actions, st.ProofKey = true, len(d.Actions), d.Proof.Current != nil
	view, edit := map[string]bool{}, map[string]bool{}
	for _, a := range d.Actions {
		if a.Ext == "" {
			continue
		}
		switch a.Name {
		case "view", "embedview":
			view[a.Ext] = true
		case "edit":
			edit[a.Ext] = true
		}
	}
	for e := range view {
		st.View = append(st.View, e)
	}
	for e := range edit {
		st.Edit = append(st.Edit, e)
	}
	sort.Strings(st.View)
	sort.Strings(st.Edit)
	st.Origins = o.Origins()
	return st
}
