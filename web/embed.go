// Package web embeds the built frontend (web/dist, produced by "npm run build").
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built frontend, or nil if it has not been built.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}

// HasIcon reports whether the built frontend has the icon (style outline or
// filled); built is false when the frontend or its icons are not built (then
// nothing can be said about the name).
func HasIcon(style, name string) (has, built bool) {
	sub := FS()
	if sub == nil {
		return false, false
	}
	vers, err := fs.ReadDir(sub, "assets/icons")
	if err != nil || len(vers) == 0 {
		return false, false
	}
	for _, v := range vers {
		if _, err := fs.Stat(sub, "assets/icons/"+v.Name()+"/"+style+"/"+name+".svg"); err == nil {
			return true, true
		}
	}
	return false, true
}
