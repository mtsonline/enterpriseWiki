package cli

import (
	"errors"
	"net/url"
	"strings"
)

// flagArgs splits "--name value" pairs (and bare "--flag" switches listed in
// switches) from positional arguments.
func flagArgs(args []string, switches ...string) (map[string]string, []string, error) {
	flags := map[string]string{}
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			pos = append(pos, a)
			continue
		}
		name := strings.TrimPrefix(a, "--")
		if k, v, ok := strings.Cut(name, "="); ok {
			flags[k] = v
			continue
		}
		isSwitch := false
		for _, s := range switches {
			isSwitch = isSwitch || s == name
		}
		if isSwitch {
			flags[name] = "true"
			continue
		}
		if i+1 >= len(args) {
			return nil, nil, errors.New("missing value for --" + name)
		}
		i++
		flags[name] = args[i]
	}
	return flags, pos, nil
}

// asArgs returns the --as arguments for e.api.
func asArgs(flags map[string]string) []string {
	if v := flags["as"]; v != "" {
		return []string{"--as", v}
	}
	return nil
}

func query(flags map[string]string, keys ...string) string {
	v := url.Values{}
	for _, k := range keys {
		if s := flags[k]; s != "" {
			v.Set(strings.ReplaceAll(k, "-", "_"), s)
		}
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

func def(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
