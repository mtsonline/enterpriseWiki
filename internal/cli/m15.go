package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
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

var auditFilters = []string{"q", "area", "action", "actor", "ip", "from", "to", "entity-type", "entity-id", "user-id", "limit", "offset"}

// auditCmd searches and exports the audit log.
//
//	ewiki audit search [--q TEXT] [--area A] [--action A] [--actor NAME] [--ip IP] [--from T] [--to T]
//	                   [--entity-type T --entity-id ID] [--limit N]
//	ewiki audit export [--format csv|jsonl] [filters]
//	ewiki audit settings | audit settings set @file|-
//	ewiki audit forward-test TARGET_JSON|@file|-
func (e *env) auditCmd(args []string) error {
	usage := errors.New("usage: ewiki audit search|export [filters] | settings [set @file|-] | forward-test TARGET_JSON|@file|-")
	flags, pos, err := flagArgs(args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return usage
	}
	switch pos[0] {
	case "search":
		return e.api(append([]string{"GET", "/api/v1/admin/audit/search" + query(flags, auditFilters...)}, asArgs(flags)...))
	case "export":
		if flags["format"] == "" {
			flags["format"] = "jsonl"
		}
		return e.api(append([]string{"GET", "/api/v1/admin/audit/export" + query(flags, append(auditFilters, "format")...)}, asArgs(flags)...))
	case "settings":
		if len(pos) == 3 && pos[1] == "set" {
			return e.api(append([]string{"PUT", "/api/v1/admin/audit/settings", pos[2]}, asArgs(flags)...))
		}
		if len(pos) != 1 {
			return usage
		}
		return e.api(append([]string{"GET", "/api/v1/admin/audit/settings"}, asArgs(flags)...))
	case "forward-test":
		if len(pos) != 2 {
			return usage
		}
		// the target as JSON (as in audit settings; a stored secret is kept when the id matches)
		return e.api(append([]string{"POST", "/api/v1/admin/audit/forward/test", pos[1]}, asArgs(flags)...))
	}
	return usage
}

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

// inboxCmd shows the notifications of a user.
//
//	ewiki inbox [list] [--limit N] --as USER
//	ewiki inbox read [ID...] --as USER      (no ID: all)
func (e *env) inboxCmd(args []string) error {
	usage := errors.New("usage: ewiki inbox [list] [--limit N] | read [ID...] --as USER")
	flags, pos, err := flagArgs(args)
	if err != nil {
		return err
	}
	if flags["as"] == "" {
		return usage
	}
	if len(pos) == 0 || pos[0] == "list" {
		return e.api(append([]string{"GET", "/api/v1/me/inbox" + query(flags, "limit")}, asArgs(flags)...))
	}
	if pos[0] != "read" {
		return usage
	}
	ids := []int64{}
	for _, s := range pos[1:] {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return usage
		}
		ids = append(ids, id)
	}
	in := map[string]any{}
	if len(ids) > 0 {
		in["ids"] = ids
	}
	body, _ := json.Marshal(in)
	return e.api(append([]string{"POST", "/api/v1/me/inbox/read", string(body)}, asArgs(flags)...))
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

func def(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
