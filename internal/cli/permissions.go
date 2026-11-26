package cli

import (
	"encoding/json"
	"errors"
)

// permCmd lists the permission catalog and shows effective permissions.
//
//	ewiki perm list
//	ewiki perm overview user|group|role|authenticated|guest [ID]
//	ewiki perm defaults set KEY...          global permissions of every signed-in user
func (e *env) permCmd(args []string) error {
	usage := errors.New("usage: ewiki perm list | overview TYPE [ID] | defaults set KEY...")
	flags, pos, err := flagArgs(args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return usage
	}
	switch pos[0] {
	case "list":
		return e.api(append([]string{"GET", "/api/v1/permissions"}, asArgs(flags)...))
	case "overview":
		if len(pos) < 2 || len(pos) > 3 {
			return usage
		}
		flags["subject_type"] = pos[1]
		if len(pos) == 3 {
			flags["subject_id"] = pos[2]
		}
		return e.api(append([]string{"GET", "/api/v1/permissions/overview" + query(flags, "subject_type", "subject_id")}, asArgs(flags)...))
	case "defaults":
		if len(pos) < 2 || pos[1] != "set" {
			return usage
		}
		keys := pos[2:]
		if keys == nil {
			keys = []string{}
		}
		body, _ := json.Marshal(map[string]any{"permissions": keys})
		return e.api(append([]string{"PUT", "/api/v1/admin/permissions/authenticated", string(body)}, asArgs(flags)...))
	}
	return usage
}
