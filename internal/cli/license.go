package cli

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/mtsonline/enterprisewiki/web"
)

// licenseCmd shows and installs the license key (wrapper around the API).
//
//	ewiki license show
//	ewiki license install KEY|@file|-
//	ewiki license remove
//	ewiki license notices        third-party licenses contained in the binary
func (e *env) licenseCmd(args []string) error {
	usage := errors.New("usage: ewiki license show | install KEY|@file|- | remove | notices")
	if len(args) == 0 {
		return usage
	}
	switch args[0] {
	case "show":
		return e.api([]string{"GET", "/api/v1/admin/license"})
	case "install":
		if len(args) != 2 {
			return usage
		}
		key := args[1]
		switch {
		case key == "-":
			b, err := io.ReadAll(e.stdin)
			if err != nil {
				return err
			}
			key = string(b)
		case strings.HasPrefix(key, "@"):
			b, err := os.ReadFile(key[1:])
			if err != nil {
				return err
			}
			key = string(b)
		}
		body, _ := json.Marshal(map[string]string{"key": strings.TrimSpace(key)})
		return e.api([]string{"PUT", "/api/v1/admin/license", string(body)})
	case "remove":
		return e.api([]string{"DELETE", "/api/v1/admin/license"})
	case "notices":
		var b []byte
		err := errors.New("no frontend")
		if w := web.FS(); w != nil {
			b, err = fs.ReadFile(w, "licenses.txt")
		}
		if err != nil {
			return errors.New("this binary was built without the third-party license list (run scripts/build.sh)")
		}
		_, err = e.stdout.Write(b)
		return err
	}
	return usage
}
