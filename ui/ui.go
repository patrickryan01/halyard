// Package ui embeds the web front end.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web
var assets embed.FS

// Handler serves the front end.
func Handler() http.Handler {
	sub, _ := fs.Sub(assets, "web")
	return http.FileServer(http.FS(sub))
}
