package cli

import (
	"encoding/json"
	"errors"
)

// prefCmd shows and changes the personal settings of a user.
//
//	ewiki pref list --as USER                  current settings and allowed values
//	ewiki pref set KEY VALUE --as USER         ("" removes the setting)
func (e *env) prefCmd(args []string) error {
	usage := errors.New("usage: ewiki pref list | set KEY VALUE --as USER")
	flags, pos, err := flagArgs(args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return usage
	}
	if flags["as"] == "" {
		return errors.New("personal settings need --as USER")
	}
	as := asArgs(flags)
	switch {
	case pos[0] == "list" && len(pos) == 1:
		return e.api(append([]string{"GET", "/api/v1/me/prefs"}, as...))
	case pos[0] == "set" && len(pos) == 3:
		body, _ := json.Marshal(map[string]any{"prefs": map[string]string{pos[1]: pos[2]}})
		return e.api(append([]string{"PUT", "/api/v1/me", string(body)}, as...))
	}
	return usage
}
