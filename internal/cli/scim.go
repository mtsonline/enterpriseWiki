package cli

import (
	"context"
	"errors"
	"flag"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/app"
	"github.com/mtsonline/enterprisewiki/internal/auth"
)

// scim manages SCIM 2.0 clients (directories that provision users and groups).
func (e *env) scim(args []string) error {
	usage := errors.New("usage: ewiki scim list | create NAME --provider P [--subject-from userName|externalId] [--on-delete disable|delete] | token ID | delete ID")
	if len(args) == 0 {
		return usage
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("scim", flag.ContinueOnError)
	provider := fs.String("provider", "", "sign-in the provisioned people use (e.g. oidc, oidc-entra)")
	subjectFrom := fs.String("subject-from", "userName", "SCIM attribute that is the subject at the provider: userName or externalId")
	onDelete := fs.String("on-delete", "disable", "what DELETE does: disable or delete")
	var arg string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		arg, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	return e.withApp(false, func(ctx context.Context, a *app.App) error {
		base := strings.TrimRight(a.Config.Get().Server.BaseURL, "/") + "/scim/v2"
		switch sub {
		case "list":
			list, err := a.Auth.ScimClients(ctx)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
			rowln(tw, "ID\tNAME\tPROVIDER\tSUBJECT FROM\tON DELETE\tLAST USED")
			for _, c := range list {
				last := "-"
				if c.LastUsedAt != nil {
					last = c.LastUsedAt.Format(time.RFC3339)
				}
				row(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", c.ID, c.Name, c.Provider, c.SubjectFrom, c.OnDelete, last)
			}
			return tw.Flush()
		case "create":
			if arg == "" {
				return usage
			}
			c, tok, err := a.Auth.CreateScimClient(ctx, auth.ScimClient{Name: arg, Provider: *provider, SubjectFrom: *subjectFrom, OnDelete: *onDelete})
			if err != nil {
				return err
			}
			e.printf("created SCIM client %s (id %d)\ntenant URL: %s\ntoken (shown once): %s\n", c.Name, c.ID, base, tok)
			return nil
		case "token", "delete":
			id, err := strconv.ParseInt(arg, 10, 64)
			if err != nil || id <= 0 {
				return usage
			}
			if sub == "delete" {
				return a.Auth.DeleteScimClient(ctx, id)
			}
			tok, err := a.Auth.RenewScimToken(ctx, id)
			if err != nil {
				return err
			}
			e.printf("new token (shown once): %s\n", tok)
			return nil
		}
		return usage
	})
}
