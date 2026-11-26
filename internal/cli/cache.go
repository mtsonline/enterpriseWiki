package cli

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/mtsonline/enterprisewiki/internal/app"
)

// cacheCmd: ewiki cache status|clear
func (e *env) cacheCmd(args []string) error {
	if len(args) != 1 || (args[0] != "status" && args[0] != "clear") {
		return errors.New("usage: ewiki cache status|clear")
	}
	return e.withApp(false, func(ctx context.Context, a *app.App) error {
		c := cliAPI(ctx, a)
		if args[0] == "clear" {
			if err := c.do("DELETE", "/api/v1/admin/cache", nil, nil); err != nil {
				return err
			}
			e.println("guest cache cleared on all servers (within a few seconds)")
			return nil
		}
		var st json.RawMessage
		if err := c.do("GET", "/api/v1/admin/cache", nil, &st); err != nil {
			return err
		}
		printJSON(e.stdout, st)
		e.println("hit statistics are kept per server process: GET /api/v1/admin/cache on the running server")
		return nil
	})
}
