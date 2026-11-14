package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"net/url"
	"strings"
)

const pluginSourcesUsage = `usage: ewiki plugin sources list [--as USER]
       ewiki plugin sources add PREFIX [--note TEXT] [--as USER]
       ewiki plugin sources remove PREFIX [--as USER]
       ewiki plugin sources check SOURCE [--as USER]
       ewiki plugin sources external allow|forbid [--as USER]`

// pluginSources manages the trusted sources of core plugins through the API.
func (e *env) pluginSources(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	sub, args := args[0], args[1:]
	var arg string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		arg, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("plugin sources", flag.ContinueOnError)
	note := fs.String("note", "", "what the source is (e.g. the team)")
	as := fs.String("as", "", "act as this user (never more than the rights of the signed-in user)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	const base = "/api/v1/admin/plugins/sources"
	body := func(v any) []byte { b, _ := json.Marshal(v); return b }
	switch {
	case sub == "list" && arg == "":
		return e.apiBody("GET", base, nil, *as)
	case sub == "add" && arg != "":
		return e.apiBody("POST", base, body(map[string]string{"prefix": arg, "note": *note}), *as)
	case sub == "remove" && arg != "":
		return e.apiBody("DELETE", base+"?prefix="+url.QueryEscape(arg), nil, *as)
	case sub == "check" && arg != "":
		return e.apiBody("POST", "/api/v1/admin/plugins/origin", body(map[string]string{"source": arg}), *as)
	case sub == "external" && (arg == "allow" || arg == "forbid"):
		return e.apiBody("PUT", base+"/external", body(map[string]bool{"allow": arg == "allow"}), *as)
	}
	return errors.New(pluginSourcesUsage)
}
