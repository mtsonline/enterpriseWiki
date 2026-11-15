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
		c := cliAPI(ctx, a)
		switch sub {
		case "list":
			var list []auth.ScimClient
			if err := c.do("GET", "/api/v1/admin/scim/clients", nil, &list); err != nil {
				return err
			}
			tw := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
			rowln(tw, "ID\tNAME\tPROVIDER\tSUBJECT FROM\tON DELETE\tLAST USED")
			for _, x := range list {
				last := "-"
				if x.LastUsedAt != nil {
					last = x.LastUsedAt.Format(time.RFC3339)
				}
				row(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", x.ID, x.Name, x.Provider, x.SubjectFrom, x.OnDelete, last)
			}
			return tw.Flush()
		case "create":
			if arg == "" {
				return usage
			}
			var res struct {
				Client auth.ScimClient `json:"client"`
				Token  string          `json:"token"`
				URL    string          `json:"url"`
			}
			in := auth.ScimClient{Name: arg, Provider: *provider, SubjectFrom: *subjectFrom, OnDelete: *onDelete}
			if err := c.do("POST", "/api/v1/admin/scim/clients", in, &res); err != nil {
				return err
			}
			e.printf("created SCIM client %s (id %d)\ntenant URL: %s\ntoken (shown once): %s\n", res.Client.Name, res.Client.ID, res.URL, res.Token)
			return nil
		case "token", "delete":
			id, err := strconv.ParseInt(arg, 10, 64)
			if err != nil || id <= 0 {
				return usage
			}
			path := "/api/v1/admin/scim/clients/" + strconv.FormatInt(id, 10)
			if sub == "delete" {
				return c.do("DELETE", path, nil, nil)
			}
			var res struct {
				Token string `json:"token"`
			}
			if err := c.do("POST", path+"/token", nil, &res); err != nil {
				return err
			}
			e.printf("new token (shown once): %s\n", res.Token)
			return nil
		}
		return usage
	})
}
