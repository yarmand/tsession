package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

// staticFS embeds the browser bundle: index.html, app.css, app.js, and the
// vendored xterm.js/addon-fit assets under vendor/. There is no Node build
// step — these files are served as-is.
//
//go:embed static
var staticFS embed.FS

// staticHandler returns an http.Handler serving the embedded frontend at
// "/" (index.html) and its supporting assets (app.js, app.css, vendor/...)
// at their matching paths.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		// static is embedded at build time; a failure here means the
		// embed directive itself is broken, which build/test would catch
		// immediately — this should never happen at runtime.
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
