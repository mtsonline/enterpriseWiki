package cli

import (
	"errors"
)

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
