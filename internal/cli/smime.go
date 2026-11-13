package cli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const smimeBase = "/api/v1/x/signature.smime"

const smimeUsage = `usage: ewiki smime list [--all | --mine --as USER]
       ewiki smime add FILE [--label TEXT] [--password-file PATH | --password-stdin] [--seal] [--no-sign] [--mine --as USER]
       ewiki smime set ID [--label TEXT] [--sign | --no-sign] [--seal | --no-seal] [--as USER]
       ewiki smime remove ID [--as USER]
       ewiki smime settings [mail_signing=true|false] [on_expired=unsigned|hold] [personal=true|false] [seal_evidence=true|false]`

// smimeCmd manages S/MIME certificates through the API of the plugin
// signature.smime (same checks, limits and audit as in the browser).
func (e *env) smimeCmd(args []string) error {
	if len(args) == 0 {
		return errors.New(smimeUsage)
	}
	var (
		pos                      []string
		as, label, passFile      string
		all, mine, passStdin     bool
		labelSet                 bool
		sign, noSign, seal, noSl bool
	)
	for i := 1; i < len(args); i++ {
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", errors.New(smimeUsage)
			}
			i++
			return args[i], nil
		}
		var err error
		switch args[i] {
		case "--as":
			as, err = next()
		case "--label":
			label, err = next()
			labelSet = true
		case "--password-file":
			passFile, err = next()
		case "--password-stdin":
			passStdin = true
		case "--all":
			all = true
		case "--mine":
			mine = true
		case "--sign":
			sign = true
		case "--no-sign":
			noSign = true
		case "--seal":
			seal = true
		case "--no-seal":
			noSl = true
		default:
			if strings.HasPrefix(args[i], "--") {
				return fmt.Errorf("unknown option %s\n%s", args[i], smimeUsage)
			}
			pos = append(pos, args[i])
		}
		if err != nil {
			return err
		}
	}
	idArg := func() (string, error) {
		if len(pos) != 1 {
			return "", errors.New(smimeUsage)
		}
		if _, err := strconv.ParseInt(pos[0], 10, 64); err != nil {
			return "", fmt.Errorf("invalid certificate id %q", pos[0])
		}
		return pos[0], nil
	}
	switch args[0] {
	case "list":
		scope := "org"
		switch {
		case all:
			scope = "all"
		case mine:
			scope = "mine"
		}
		return e.apiBody("GET", smimeBase+"/certs?scope="+scope, nil, as)
	case "add":
		if len(pos) != 1 {
			return errors.New(smimeUsage)
		}
		data, err := os.ReadFile(pos[0]) // #nosec G304 -- the file named by the operator
		if err != nil {
			return err
		}
		var password string
		switch {
		case passFile != "":
			b, err := os.ReadFile(passFile) // #nosec G304 -- the file named by the operator
			if err != nil {
				return err
			}
			password = strings.TrimRight(string(b), "\r\n")
		case passStdin:
			b, err := io.ReadAll(io.LimitReader(e.stdin, 4096))
			if err != nil {
				return err
			}
			password = strings.TrimRight(string(b), "\r\n")
		}
		scope := "org"
		if mine {
			scope = "mine"
		}
		body, err := json.Marshal(map[string]any{"scope": scope, "label": label, "file": base64.StdEncoding.EncodeToString(data),
			"password": password, "seal": seal, "sign_mail": !noSign})
		if err != nil {
			return err
		}
		password = ""
		return e.apiBody("POST", smimeBase+"/certs", body, as)
	case "set":
		id, err := idArg()
		if err != nil {
			return err
		}
		ch := map[string]any{}
		if labelSet {
			ch["label"] = label
		}
		if sign || noSign {
			ch["sign_mail"] = sign
		}
		if seal || noSl {
			ch["seal"] = seal
		}
		if len(ch) == 0 {
			return errors.New(smimeUsage)
		}
		body, err := json.Marshal(ch)
		if err != nil {
			return err
		}
		return e.apiBody("PATCH", smimeBase+"/certs/"+id, body, as)
	case "remove":
		id, err := idArg()
		if err != nil {
			return err
		}
		return e.apiBody("DELETE", smimeBase+"/certs/"+id, nil, as)
	case "settings":
		if len(pos) == 0 {
			return e.apiBody("GET", smimeBase+"/settings", nil, as)
		}
		return e.apiJSON("GET", smimeBase+"/settings", as, func(cur []byte) error {
			st := map[string]any{}
			if err := json.Unmarshal(cur, &st); err != nil {
				return err
			}
			for _, kv := range pos {
				k, v, ok := strings.Cut(kv, "=")
				switch {
				case !ok:
					return errors.New(smimeUsage)
				case k == "on_expired":
					st[k] = v
				case k == "mail_signing" || k == "personal" || k == "seal_evidence":
					b, err := strconv.ParseBool(v)
					if err != nil {
						return fmt.Errorf("%s: true or false", k)
					}
					st[k] = b
				default:
					return fmt.Errorf("unknown setting %q", k)
				}
			}
			body, err := json.Marshal(st)
			if err != nil {
				return err
			}
			return e.apiBody("PUT", smimeBase+"/settings", body, as)
		})
	}
	return fmt.Errorf("unknown smime command %q\n%s", args[0], smimeUsage)
}
