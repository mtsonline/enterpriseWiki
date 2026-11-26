package cli

import (
	"encoding/json"
	"errors"
	"strconv"
)

// watchCmd manages the watches of a user (wrapper around the API).
//
//	ewiki watch list --as USER
//	ewiki watch add category|element ID [--mode link|content] [--frequency instant|daily|weekly] [--no-recursive] --as USER
//	ewiki watch remove WATCH_ID --as USER
func (e *env) watchCmd(args []string) error {
	if len(args) > 0 && args[0] == "assign" {
		return e.watchAssignCmd(args[1:])
	}
	usage := errors.New("usage: ewiki watch list|add TYPE ID [--mode link|content] [--frequency instant|daily|weekly] [--no-recursive]|remove ID --as USER")
	as, mode, freq, rec := "", "link", "instant", true
	var pos []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--as":
			if i+1 >= len(args) {
				return usage
			}
			i++
			as = args[i]
		case "--mode":
			if i+1 >= len(args) {
				return usage
			}
			i++
			mode = args[i]
		case "--frequency":
			if i+1 >= len(args) {
				return usage
			}
			i++
			freq = args[i]
		case "--no-recursive":
			rec = false
		default:
			pos = append(pos, args[i])
		}
	}
	if len(pos) == 0 || as == "" {
		return usage
	}
	asArgs := []string{"--as", as}
	switch pos[0] {
	case "list":
		return e.api(append([]string{"GET", "/api/v1/me/watches"}, asArgs...))
	case "add":
		if len(pos) != 3 {
			return usage
		}
		id, err := strconv.ParseInt(pos[2], 10, 64)
		if err != nil {
			return usage
		}
		body, _ := json.Marshal(map[string]any{"target_type": pos[1], "target_id": id, "mode": mode, "frequency": freq, "recursive": rec})
		return e.api(append([]string{"PUT", "/api/v1/me/watches", string(body)}, asArgs...))
	case "remove":
		if len(pos) != 2 {
			return usage
		}
		return e.api(append([]string{"DELETE", "/api/v1/me/watches/" + pos[1]}, asArgs...))
	}
	return usage
}

// watchAssignCmd manages centrally assigned watches.
//
//	ewiki watch assign list
//	ewiki watch assign add user|group|role SUBJECT_ID category|element TARGET_ID
//	                   [--mode link|content] [--frequency instant|daily|weekly] [--no-recursive] [--mandatory] [--note TEXT]
//	ewiki watch assign remove ID
func (e *env) watchAssignCmd(args []string) error {
	usage := errors.New("usage: ewiki watch assign list | add SUBJECT_TYPE SUBJECT_ID TARGET_TYPE TARGET_ID [--mode M] [--frequency F] [--no-recursive] [--mandatory] [--note TEXT] | remove ID")
	flags, pos, err := flagArgs(args, "no-recursive", "mandatory")
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return usage
	}
	switch {
	case pos[0] == "list" && len(pos) == 1:
		return e.api(append([]string{"GET", "/api/v1/admin/watch-assignments"}, asArgs(flags)...))
	case pos[0] == "remove" && len(pos) == 2:
		return e.api(append([]string{"DELETE", "/api/v1/admin/watch-assignments/" + pos[1]}, asArgs(flags)...))
	case pos[0] == "add" && len(pos) == 5:
		sid, err1 := strconv.ParseInt(pos[2], 10, 64)
		tid, err2 := strconv.ParseInt(pos[4], 10, 64)
		if err1 != nil || err2 != nil {
			return usage
		}
		rec := flags["no-recursive"] != "true"
		in := map[string]any{"subject_type": pos[1], "subject_id": sid, "target_type": pos[3], "target_id": tid,
			"mode": def(flags["mode"], "link"), "frequency": def(flags["frequency"], "instant"), "recursive": rec,
			"mandatory": flags["mandatory"] == "true", "note": flags["note"]}
		body, _ := json.Marshal(in)
		return e.api(append([]string{"POST", "/api/v1/admin/watch-assignments", string(body)}, asArgs(flags)...))
	}
	return usage
}
