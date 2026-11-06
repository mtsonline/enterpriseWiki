package cli

import (
	"encoding/json"
	"errors"
	"flag"
)

const demoUsage = `usage: ewiki demo status
       ewiki demo create --as ADMIN     (demo area with example content of all features)
       ewiki demo remove                (removes everything the demo created, without a trace)
       ewiki demo reset-site --as ADMIN --confirm "SITE NAME"
                                        (the whole site as at its first start: all content,
                                         accounts except ADMIN and settings are deleted)`

func (e *env) demoCmd(args []string) error {
	if len(args) == 0 {
		return errors.New(demoUsage)
	}
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	as := fs.String("as", "", "administrator (user[@provider]) who creates the demo / stays after the reset")
	confirm := fs.String("confirm", "", "name of the site (reset-site)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	switch args[0] {
	case "status":
		return e.apiBody("GET", "/api/v1/admin/demo", nil, *as)
	case "create":
		if *as == "" {
			return errors.New("ewiki demo create needs --as ADMIN (the demo is created with that administrator's rights)")
		}
		return e.apiBody("POST", "/api/v1/admin/demo", nil, *as)
	case "remove":
		return e.apiBody("DELETE", "/api/v1/admin/demo", nil, *as)
	case "reset-site":
		if *as == "" || *confirm == "" {
			return errors.New(`ewiki demo reset-site needs --as ADMIN and --confirm "SITE NAME"`)
		}
		body, _ := json.Marshal(map[string]string{"confirm": *confirm})
		return e.apiBody("POST", "/api/v1/admin/reset", body, *as)
	}
	return errors.New(demoUsage)
}
