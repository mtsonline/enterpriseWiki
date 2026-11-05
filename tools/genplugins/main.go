// Command genplugins regenerates plugins/all/zz_generated.go. It does not
// import the plugins itself, so it also works when the generated file is
// broken (e.g. after a plugin directory was deleted manually).
package main

import (
	"fmt"
	"os"

	"github.com/mtsonline/enterprisewiki/internal/pluginmgr"
)

func main() {
	if err := pluginmgr.Generate("."); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("plugins/all/zz_generated.go updated")
}
