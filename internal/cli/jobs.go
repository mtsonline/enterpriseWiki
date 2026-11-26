package cli

import (
	"context"
	"errors"
	"net/url"

	"github.com/mtsonline/enterprisewiki/internal/app"
)

// jobsCmd lists and runs background jobs.
func (e *env) jobsCmd(args []string) error {
	return e.withApp(false, func(ctx context.Context, a *app.App) error {
		c := cliAPI(ctx, a)
		if len(args) == 0 || args[0] == "list" {
			var names []string
			if err := c.do("GET", "/api/v1/admin/jobs", nil, &names); err != nil {
				return err
			}
			for _, n := range names {
				e.println(n)
			}
			return nil
		}
		if args[0] == "run" && len(args) > 1 {
			if err := c.do("POST", "/api/v1/admin/jobs/"+url.PathEscape(args[1])+"/run", nil, nil); err != nil {
				return err
			}
			e.printf("%s finished\n", args[1])
			return nil
		}
		return errors.New("usage: ewiki jobs list | ewiki jobs run NAME")
	})
}
