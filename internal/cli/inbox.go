package cli

import (
	"encoding/json"
	"errors"
	"strconv"
)

// inboxCmd shows the notifications of a user.
//
//	ewiki inbox [list] [--limit N] [--before ID] [--unread] --as USER
//	ewiki inbox read [ID...] --as USER      (no ID: all)
//
// --before ID pages: it lists the ones older than the notification ID.
func (e *env) inboxCmd(args []string) error {
	usage := errors.New("usage: ewiki inbox [list] [--limit N] [--before ID] [--unread] | read [ID...] --as USER")
	flags, pos, err := flagArgs(args, "unread")
	if err != nil {
		return err
	}
	if flags["as"] == "" {
		return usage
	}
	if len(pos) == 0 || pos[0] == "list" {
		if flags["unread"] == "true" {
			flags["unread"] = "1"
		} else {
			delete(flags, "unread")
		}
		return e.api(append([]string{"GET", "/api/v1/me/inbox" + query(flags, "limit", "before", "unread")}, asArgs(flags)...))
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
