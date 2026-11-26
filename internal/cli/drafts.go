package cli

import (
	"encoding/json"
	"errors"
	"strconv"
)

// draftCmd lists and publishes drafts of a user.
//
//	ewiki draft list --as USER
//	ewiki draft show ID --as USER
//	ewiki draft publish ID [--at RFC3339] [--category ID] [--silent] --as USER
//	ewiki draft delete ID --as USER
func (e *env) draftCmd(args []string) error {
	usage := errors.New("usage: ewiki draft list | show ID | publish ID [--at TIME] [--category ID] [--silent] | delete ID --as USER")
	flags, pos, err := flagArgs(args, "silent")
	if err != nil {
		return err
	}
	if len(pos) == 0 || flags["as"] == "" {
		return usage
	}
	as := asArgs(flags)
	switch {
	case pos[0] == "list" && len(pos) == 1:
		return e.api(append([]string{"GET", "/api/v1/drafts"}, as...))
	case pos[0] == "show" && len(pos) == 2:
		return e.api(append([]string{"GET", "/api/v1/drafts/" + pos[1]}, as...))
	case pos[0] == "delete" && len(pos) == 2:
		return e.api(append([]string{"DELETE", "/api/v1/drafts/" + pos[1]}, as...))
	case pos[0] == "publish" && len(pos) == 2:
		in := map[string]any{"notify": flags["silent"] != "true"}
		if v := flags["at"]; v != "" {
			in["at"] = v
		}
		if v := flags["category"]; v != "" {
			id, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return usage
			}
			in["category_id"] = id
		}
		body, _ := json.Marshal(in)
		return e.api(append([]string{"POST", "/api/v1/drafts/" + pos[1] + "/publish", string(body)}, as...))
	}
	return usage
}
