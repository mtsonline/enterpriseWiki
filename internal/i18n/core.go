package i18n

import (
	"embed"
	"io/fs"
)

//go:embed locales/*.json
var coreFS embed.FS

// CoreLocales returns the UI strings of the core.
func CoreLocales() fs.FS {
	sub, _ := fs.Sub(coreFS, "locales")
	return sub
}
