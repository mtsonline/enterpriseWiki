package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

const directoryUsage = `usage: ewiki directory sync PROVIDER [--dry-run] [--force] [--as USER]
       ewiki directory runs PROVIDER [--as USER]
       ewiki directory adopt PROVIDER USERNAME [--as USER]
PROVIDER is the sign-in provider, e.g. ldap or ldap-corp.`

// directoryCmd runs and lists the directory sync through the API of the
// sign-in plugin (same checks and audit as in the browser).
func (e *env) directoryCmd(args []string) error {
	if len(args) < 2 {
		return errors.New(directoryUsage)
	}
	var as, username string
	var dry, force bool
	first := 2
	if args[0] == "adopt" {
		if len(args) < 3 || strings.HasPrefix(args[2], "--") {
			return errors.New(directoryUsage)
		}
		username, first = args[2], 3
	}
	for i := first; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			dry = true
		case "--force":
			force = true
		case "--as":
			if i+1 >= len(args) {
				return errors.New(directoryUsage)
			}
			i++
			as = args[i]
		default:
			return errors.New(directoryUsage)
		}
	}
	provider := args[1]
	base, instance, err := directoryPlugin(provider)
	if err != nil {
		return err
	}
	switch args[0] {
	case "sync":
		body, _ := json.Marshal(map[string]any{"instance": instance, "dry_run": dry, "force": force})
		return e.apiBody("POST", base+"/sync", body, as)
	case "runs":
		return e.apiBody("GET", base+"/sync?instance="+url.QueryEscape(instance), nil, as)
	case "adopt":
		body, _ := json.Marshal(map[string]any{"instance": instance, "username": username})
		return e.apiBody("POST", base+"/sync/adopt", body, as)
	}
	return errors.New(directoryUsage)
}

// directoryPlugin finds the plugin and instance of a provider id: ldap is
// the first instance of auth.ldap, ldap-<id> the instance <id>.
func directoryPlugin(provider string) (base, instance string, err error) {
	if provider == "ldap" {
		return "/api/v1/x/auth.ldap", "", nil
	}
	if id, ok := strings.CutPrefix(provider, "ldap-"); ok && id != "" {
		return "/api/v1/x/auth.ldap", id, nil
	}
	return "", "", errors.New("no directory sync for provider " + provider + " (supported: ldap, ldap-<id>)")
}
