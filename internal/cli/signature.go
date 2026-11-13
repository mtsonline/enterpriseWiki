package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/mtsonline/enterprisewiki/internal/app"
	"github.com/mtsonline/enterprisewiki/internal/signature"
)

// signatureCmd: ewiki signature verify FILE [--offline] | ewiki signature keys
func (e *env) signatureCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ewiki signature verify FILE [--offline] | ewiki signature keys")
	}
	switch args[0] {
	case "verify":
		if len(args) < 2 {
			return errors.New("usage: ewiki signature verify FILE [--offline]")
		}
		b, err := os.ReadFile(args[1]) // #nosec G304 -- the file named by the operator
		if err != nil {
			return err
		}
		var x signature.Export
		if err := json.Unmarshal(b, &x); err != nil || x.Method == "" {
			return errors.New("not an exported signature")
		}
		check := func(keys []signature.PublicKey) error {
			cs := signature.Verify(&x, keys)
			for _, c := range cs {
				mark := "ok  "
				if !c.OK {
					mark = "FAIL"
				}
				e.printf("%s %s %s\n", mark, c.Name, c.Detail)
			}
			e.printf("signer: %s <%s>, method %s (%s), signed %s, seal key %s\n", x.Signer.Name, x.Signer.Email, x.Method, x.Level,
				x.SignedAt.Format("2006-01-02 15:04:05 MST"), x.Key.Fingerprint)
			if !signature.Valid(cs) {
				return errors.New("the signature is NOT valid")
			}
			e.printf("valid\n")
			return nil
		}
		// --offline: only the key in the file (compare its fingerprint with
		// the one the wiki publishes)
		if len(args) > 2 && args[2] == "--offline" {
			return check(nil)
		}
		return e.withApp(false, func(ctx context.Context, a *app.App) error {
			keys, err := a.Signatures.Keys(ctx)
			if err != nil {
				return err
			}
			return check(keys)
		})
	case "keys":
		return e.withApp(false, func(ctx context.Context, a *app.App) error {
			keys, err := a.Signatures.Keys(ctx)
			if err == nil {
				printJSON(e.stdout, keys)
			}
			return err
		})
	}
	return fmt.Errorf("unknown signature command %q", args[0])
}
