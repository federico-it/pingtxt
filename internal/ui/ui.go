// Package ui embeds the compiled Next.js static export (web/out).
// The Docker build copies web/out into ./dist before compiling the hub.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var files embed.FS

// FS returns the static web UI.
func FS() fs.FS {
	sub, err := fs.Sub(files, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
