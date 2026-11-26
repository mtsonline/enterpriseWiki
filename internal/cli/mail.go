package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
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
		c := cliAPI(ctx, a)
		outbox := func(n int64, rest string) string { return fmt.Sprintf("/api/v1/admin/mail/outbox/%d%s", n, rest) }
		switch sub {
		case "status":
			var st json.RawMessage
			if err := c.do("GET", "/api/v1/admin/mail", nil, &st); err != nil {
				return err
			}
			printJSON(e.stdout, st)
			return nil
		case "list":
			var list []mail.OutboxEntry
			q := url.Values{"status": {*status}, "q": {*q}, "limit": {strconv.Itoa(*limit)}}
			if err := c.do("GET", "/api/v1/admin/mail/outbox?"+q.Encode(), nil, &list); err != nil {
				return err
			}
			tw := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
			rowln(tw, "ID\tSTATUS\tDELIVERED\tPENDING\tFAILED\tCREATED\tSUBJECT")
			for _, o := range list {
				row(tw, "%d\t%s\t%d\t%d\t%d\t%s\t%s\n", o.ID, o.Status, o.Delivered, o.Pending, o.Failed, o.CreatedAt.Format("2006-01-02 15:04"), o.Subject)
			}
			return tw.Flush()
		case "show":
			n, err := id()
			if err != nil {
				return err
			}
			var m json.RawMessage
			if err := c.do("GET", outbox(n, ""), nil, &m); err != nil {
				return err
			}
			printJSON(e.stdout, m)
			return nil
		case "retry":
			n, err := id()
			if err != nil {
				return err
			}
			if err := c.do("POST", outbox(n, "/retry"), nil, nil); err != nil {
				return err
			}
			e.printf("message %d queued again\n", n)
			return nil
		case "delete":
			n, err := id()
			if err != nil {
				return err
			}
			return c.do("DELETE", outbox(n, ""), nil, nil)
		case "test":
			if len(pos) == 0 {
				return errors.New("usage: ewiki mail test ADDRESS")
			}
			if err := c.do("POST", "/api/v1/admin/mail/test", map[string]string{"to": pos[0]}, nil); err != nil {
				return err
			}
			e.println("test message sent")
			return nil
		case "send", "bounces":
			// the background jobs "mail" and "mail.bounces", run now
			job := map[string]string{"send": "mail", "bounces": "mail.bounces"}[sub]
			if err := c.do("POST", "/api/v1/admin/jobs/"+job+"/run", nil, nil); err != nil {
				return err
			}
			e.printf("%s finished (see ewiki mail status)\n", job)
			return nil
		}
		return fmt.Errorf("unknown mail command %q", sub)
	})
}
