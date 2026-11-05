package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"strings"
)

// siteCmd manages the sites of the instance (instance administrators).
func (e *env) siteCmd(args []string) error {
	usage := errors.New(`usage: ewiki site list
       ewiki site create NAME --host HOST[,HOST...] [--title T]
       ewiki site update NAME [--host HOST[,HOST...]] [--title T] [--disable|--enable]
       ewiki site delete NAME --yes
       ewiki site admins | site admins add USER_ID | site admins remove USER_ID`)
	if len(args) == 0 {
		return usage
	}
	sub := args[0]
	switch sub {
	case "list":
		return e.api([]string{"GET", "/api/v1/instance/sites"})
	case "admins":
		switch {
		case len(args) == 1:
			return e.api([]string{"GET", "/api/v1/instance/admins"})
		case len(args) == 3 && args[1] == "add":
			return e.api([]string{"POST", "/api/v1/instance/admins", `{"user_id":` + args[2] + `}`})
		case len(args) == 3 && args[1] == "remove":
			return e.api([]string{"DELETE", "/api/v1/instance/admins/" + args[2]})
		}
		return usage
	case "create", "update", "delete":
	default:
		return usage
	}
	if len(args) < 2 || strings.HasPrefix(args[1], "-") {
		return usage
	}
	name := args[1]
	fs := flag.NewFlagSet("site "+sub, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	hosts := fs.String("host", "", "host names, comma separated")
	title := fs.String("title", "", "title")
	disable := fs.Bool("disable", false, "disable the site (data is kept)")
	enable := fs.Bool("enable", false, "enable the site")
	yes := fs.Bool("yes", false, "confirm deleting the site with all its data")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	if sub == "delete" {
		if !*yes {
			return errors.New("deleting a site removes all its pages, files and settings: add --yes")
		}
		return e.api([]string{"DELETE", "/api/v1/instance/sites/" + name})
	}
	body := map[string]any{"name": name}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["host"] {
		list := []string{}
		for _, h := range strings.Split(*hosts, ",") {
			if h = strings.TrimSpace(h); h != "" {
				list = append(list, h)
			}
		}
		body["hostnames"] = list
	}
	if set["title"] {
		body["title"] = *title
	}
	if *disable || *enable {
		body["disabled"] = *disable
	}
	b, _ := json.Marshal(body)
	if sub == "create" {
		return e.api([]string{"POST", "/api/v1/instance/sites", string(b)})
	}
	return e.api([]string{"PATCH", "/api/v1/instance/sites/" + name, string(b)})
}
