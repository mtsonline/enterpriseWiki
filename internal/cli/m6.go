package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"text/tabwriter"

	"github.com/mtsonline/enterprisewiki/internal/app"
	"github.com/mtsonline/enterprisewiki/internal/mail"
)

// mailCmd manages the e-mail queue.
func (e *env) mailCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ewiki mail status|list|show|retry|delete|test|send|bounces")
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("mail", flag.ContinueOnError)
	status := fs.String("status", "", "filter: queued, held, sent, partial, failed")
	q := fs.String("q", "", "search in subject and recipients")
	limit := fs.Int("limit", 50, "number of messages")
	var pos []string
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			return err
		}
		rest = fs.Args()
		if len(rest) > 0 {
			pos = append(pos, rest[0])
			rest = rest[1:]
		}
	}
	id := func() (int64, error) {
		if len(pos) == 0 {
			return 0, fmt.Errorf("usage: ewiki mail %s ID", sub)
		}
		return strconv.ParseInt(pos[0], 10, 64)
	}
	return e.withApp(false, func(ctx context.Context, a *app.App) error {
		switch sub {
		case "status":
			st, err := a.Mail.GetStatus(ctx)
			if err == nil {
				printJSON(e.stdout, st)
			}
			return err
		case "list":
			list, err := a.Mail.Outbox(ctx, *status, *q, *limit, 0)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tSTATUS\tDELIVERED\tPENDING\tFAILED\tCREATED\tSUBJECT")
			for _, o := range list {
				fmt.Fprintf(tw, "%d\t%s\t%d\t%d\t%d\t%s\t%s\n", o.ID, o.Status, o.Delivered, o.Pending, o.Failed, o.CreatedAt.Format("2006-01-02 15:04"), o.Subject)
			}
			return tw.Flush()
		case "show":
			n, err := id()
			if err != nil {
				return err
			}
			m, err := a.Mail.Get(ctx, n)
			if err == nil {
				printJSON(e.stdout, m)
			}
			return err
		case "retry":
			n, err := id()
			if err != nil {
				return err
			}
			if err := a.Mail.Retry(ctx, n); err != nil {
				return err
			}
			fmt.Fprintf(e.stdout, "message %d queued again\n", n)
			return nil
		case "delete":
			n, err := id()
			if err != nil {
				return err
			}
			return a.Mail.Delete(ctx, n)
		case "test":
			if len(pos) == 0 {
				return errors.New("usage: ewiki mail test ADDRESS")
			}
			site := a.Config.Get().UI.SiteName
			if err := a.Mail.SendNow(ctx, mail.Message{To: []string{pos[0]}, Subject: site + ": test message", Text: "Outgoing e-mail works.", Source: "test"}); err != nil {
				return err
			}
			fmt.Fprintln(e.stdout, "test message sent")
			return nil
		case "send":
			n, err := a.Mail.Process(ctx)
			fmt.Fprintf(e.stdout, "%d recipients delivered\n", n)
			return err
		case "bounces":
			n, err := a.Mail.ProcessBounces(ctx)
			fmt.Fprintf(e.stdout, "%d failed recipients found in bounces\n", n)
			return err
		}
		return fmt.Errorf("unknown mail command %q", sub)
	})
}

// jobsCmd lists and runs background jobs.
func (e *env) jobsCmd(args []string) error {
	return e.withApp(false, func(ctx context.Context, a *app.App) error {
		if len(args) == 0 || args[0] == "list" {
			for _, n := range a.Workers.Names() {
				fmt.Fprintln(e.stdout, n)
			}
			return nil
		}
		if args[0] == "run" && len(args) > 1 {
			if err := a.Workers.RunOnce(ctx, args[1]); err != nil {
				return err
			}
			fmt.Fprintf(e.stdout, "%s finished\n", args[1])
			return nil
		}
		return errors.New("usage: ewiki jobs list | ewiki jobs run NAME")
	})
}

// cacheCmd: ewiki cache status|clear
func (e *env) cacheCmd(args []string) error {
	if len(args) != 1 || (args[0] != "status" && args[0] != "clear") {
		return errors.New("usage: ewiki cache status|clear")
	}
	return e.withApp(false, func(ctx context.Context, a *app.App) error {
		c := a.Config.Get().Cache
		if args[0] == "clear" {
			if !c.Enabled {
				fmt.Fprintln(e.stdout, "the guest cache is switched off (cache.enabled: false)")
				return nil
			}
			a.Cache.Invalidate(ctx, "cli")
			fmt.Fprintln(e.stdout, "guest cache cleared on all servers (within a few seconds)")
			return nil
		}
		var gen int64
		_ = a.DB.QueryRow(ctx, `SELECT generation FROM cache_state WHERE id = 1`).Scan(&gen)
		fmt.Fprintf(e.stdout, "enabled:     %v\nttl:         %ds (0 = until the next change)\nmax entries: %d\nmax size:    %d MB\ngeneration:  %d\n",
			c.Enabled, c.TTLSeconds, c.MaxEntries, c.MaxMB, gen)
		fmt.Fprintln(e.stdout, "hit statistics are kept per server process: GET /api/v1/admin/cache on the running server")
		return nil
	})
}
