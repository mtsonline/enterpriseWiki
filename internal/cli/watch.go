package cli

import (
	"encoding/json"
	"errors"
	"strconv"
)

// watchCmd manages the watches of a user (wrapper around the API).
//
//	ewiki watch list --as USER
//	ewiki watch add category|element ID [--mode link|content] [--no-recursive] --as USER
//	ewiki watch remove WATCH_ID --as USER
func (e *env) watchCmd(args []string) error {
	usage := errors.New("usage: ewiki watch list|add TYPE ID [--mode link|content] [--no-recursive]|remove ID --as USER")
	as, mode, rec := "", "link", true
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
		body, _ := json.Marshal(map[string]any{"target_type": pos[1], "target_id": id, "mode": mode, "recursive": rec})
		return e.api(append([]string{"PUT", "/api/v1/me/watches", string(body)}, asArgs...))
	case "remove":
		if len(pos) != 2 {
			return usage
		}
		return e.api(append([]string{"DELETE", "/api/v1/me/watches/" + pos[1]}, asArgs...))
	}
	return usage
}
