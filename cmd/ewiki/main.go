// Command ewiki is the Enterprise Wiki server and command line tool.
//
//	ewiki serve              start the server
//	ewiki setup              interactive (or headless) installation
//	ewiki help               list all commands
package main

import (
	"os"

	"github.com/mtsonline/enterprisewiki/internal/cli"
	_ "github.com/mtsonline/enterprisewiki/plugins/all" // compiled-in plugins
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
